package data

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/HalefS/lira/internal/validator"
	"github.com/lib/pq"
)

const (
	maxTVSwapsPerIssue = 10
	maxTVSwapRoomName  = 100
	maxTVSwapNotes     = 500
)

// TVSwap is one TV that was moved from one room to another while fixing an
// issue. A swap is its own row rather than a pair of columns on `issues`
// because the same issue can move a TV more than once — out of the faulty room
// and into storage, then back out once the repair is done — and each of those
// moves is a separate event that is worth keeping.
type TVSwap struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	IssueID   int64     `json:"issue_id"`

	// Where the working TV came from, and where it went.
	FromRoom string `json:"from_room"`
	ToRoom   string `json:"to_room"`

	// Free text for what a room pair cannot say: which set was moved, that the
	// faulty one is awaiting repair, and so on.
	Notes string `json:"notes"`

	CreatedBy *int64 `json:"created_by,omitempty"`
	Version   int    `json:"-"`
}

// TVSwapUse is one swap as sent by the client when an issue is created or
// edited. A zero ID means "a swap that doesn't exist yet", so the server inserts
// it rather than trying to update an existing row.
type TVSwapUse struct {
	ID       int64  `json:"id,omitempty"`
	FromRoom string `json:"from_room"`
	ToRoom   string `json:"to_room"`
	Notes    string `json:"notes"`
}

// ValidateTVSwaps checks the swaps sent with an issue create/update request:
// both ends of every swap are present and short enough, no two swaps on the
// same issue claim to be the same move, and no swap moves a TV onto itself.
//
// The required-field and no-self-swap rules are also enforced by the database
// (see migration 000019); the checks here exist to return a message naming the
// field, which a CHECK violation cannot do.
func ValidateTVSwaps(v *validator.Validator, uses []TVSwapUse) {
	v.Check(len(uses) <= maxTVSwapsPerIssue, "tv_swaps",
		"must not contain more than 10 swaps")

	seen := make(map[string]bool, len(uses))
	for _, u := range uses {
		field := "tv_swaps"
		from := strings.TrimSpace(u.FromRoom)
		to := strings.TrimSpace(u.ToRoom)

		switch {
		case from == "":
			v.AddError(field, "a swap must say which room the TV came from")
		case to == "":
			v.AddError(field, "a swap must say which room the TV went to")
		case strings.EqualFold(from, to):
			v.AddError(field, "the source and destination rooms must be different")
		}
		if len(from) > maxTVSwapRoomName {
			v.AddError(field, "a room name must not be more than 100 characters")
		}
		if len(to) > maxTVSwapRoomName {
			v.AddError(field, "a room name must not be more than 100 characters")
		}
		v.Check(len(u.Notes) <= maxTVSwapNotes, "tv_swaps",
			"must not be more than 500 characters")

		// The same move listed twice on one issue is a double click, not two
		// events. The comparison is on the *trimmed* room names, because the
		// lengths checked above are read before trimming and the model stores
		// the trimmed value — keying off the raw strings here would let
		// "1301 -> 1204" and " 1301  -> 1204 " past as different moves.
		if from != "" && to != "" {
			key := strings.ToLower(from) + "\x00" + strings.ToLower(to)
			if seen[key] {
				v.AddError(field, "the same swap is listed more than once")
			}
			seen[key] = true
		}
	}
}

type TVSwapModel struct {
	DB *sql.DB
}

// tvSwapColumns is the column list shared by every read of a swap, so the scan
// order below can never drift from the select list.
const tvSwapColumns = `
	s.id, s.created_at, s.issue_id, s.from_room, s.to_room, s.notes,
	s.created_by, s.version`

func scanTVSwap(scan func(dest ...any) error) (*TVSwap, error) {
	var s TVSwap
	var createdBy sql.NullInt64
	if err := scan(&s.ID, &s.CreatedAt, &s.IssueID, &s.FromRoom, &s.ToRoom,
		&s.Notes, &createdBy, &s.Version); err != nil {
		return nil, err
	}
	if createdBy.Valid {
		s.CreatedBy = &createdBy.Int64
	}
	return &s, nil
}

// SetForIssue makes the issue's swaps match the given list: entries carrying an
// id are updated in place, entries without one are inserted, and swaps that are
// no longer listed are removed.
//
// The whole thing runs in one transaction so the issue is never left with half
// the swaps the form showed. Rows are matched on id *and* issue_id, so a client
// cannot claim (or overwrite) a swap belonging to another issue.
func (m TVSwapModel) SetForIssue(issueID int64, uses []TVSwapUse, loggedBy int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Must be non-nil: a nil slice becomes SQL NULL, and "<> ALL(NULL)" matches
	// no rows, so every swap would be deleted.
	keep := make([]int64, 0, len(uses))
	for _, u := range uses {
		if u.ID > 0 {
			keep = append(keep, u.ID)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM issue_tv_swaps
		WHERE issue_id = $1 AND id <> ALL($2::bigint[])`,
		issueID, pq.Array(keep),
	); err != nil {
		return err
	}

	for _, u := range uses {
		from := strings.TrimSpace(u.FromRoom)
		to := strings.TrimSpace(u.ToRoom)
		notes := strings.TrimSpace(u.Notes)

		if u.ID > 0 {
			res, err := tx.ExecContext(ctx, `
				UPDATE issue_tv_swaps
				SET from_room=$3, to_room=$4, notes=$5, version=version+1
				WHERE id=$1 AND issue_id=$2`,
				u.ID, issueID, from, to, notes,
			)
			if err != nil {
				return err
			}
			affected, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if affected == 0 {
				// The id belongs to a different issue, or the row was deleted
				// while the form was open. Either way it is not ours to touch.
				return ErrRecordNotFound
			}
			continue
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO issue_tv_swaps
				(issue_id, from_room, to_room, notes, created_by)
			VALUES ($1, $2, $3, $4, $5)`,
			issueID, from, to, notes, loggedBy,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// TVSwapListItem is a swap as returned by GetList, with the issue context the
// swap table alone doesn't carry: which room was reported broken, and what type
// of fault it was. Without those, a bare "1324 -> 1301" is hard to read — it
// doesn't say which of the two rooms was the problem.
type TVSwapListItem struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	IssueID   int64     `json:"issue_id"`
	FromRoom  string    `json:"from_room"`
	ToRoom    string    `json:"to_room"`
	Notes     string    `json:"notes"`
	IssueMode string    `json:"issue_mode"`
	// The room or department the fault was reported in. This is usually the
	// same as ToRoom, but not always: a set can be moved to a room whose own
	// set was fine, and it is the reported room that says what the guest
	// complained about.
	IssueLocation string `json:"issue_location"`
	IssueType     string `json:"issue_type"`
	IssueProblem  string `json:"issue_problem"`
	LoggedByName  string `json:"logged_by_name"`
}

// GetList returns swaps newest first. Passing issueID 0 returns swaps across
// every issue; a non-zero id scopes the list to that issue. from and to are
// "YYYY-MM-DD" bounds on the swap's own created_at, either of which may be
// empty for unbounded.
//
// The issue's own fields are joined in because a swap on its own does not say
// which of the two rooms was the one that was faulty.
func (m TVSwapModel) GetList(issueID int64, from, to string, limit int) ([]*TVSwapListItem, error) {
	// The range is on the swap's own created_at, anchored in the server's
	// timezone like every other date filter in the app. An empty bound is
	// unbounded, so the page opens on "everything" and narrows from there.
	query := `
		SELECT s.id, s.created_at, s.issue_id, s.from_room, s.to_room, s.notes,
		       i.mode, i.location, i.type, i.problem, COALESCE(u.name, '')
		FROM issue_tv_swaps s
		INNER JOIN issues i ON i.id = s.issue_id
		LEFT JOIN users u ON u.id = i.logged_by
		WHERE ($1 = 0 OR s.issue_id = $1)
		  AND ($2 = '' OR s.created_at::date >= $2::date)
		  AND ($3 = '' OR s.created_at::date <= $3::date)
		ORDER BY s.created_at DESC, s.id DESC
		LIMIT $4`

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, query, issueID, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*TVSwapListItem{}
	for rows.Next() {
		var s TVSwapListItem
		if err := rows.Scan(&s.ID, &s.CreatedAt, &s.IssueID, &s.FromRoom, &s.ToRoom,
			&s.Notes, &s.IssueMode, &s.IssueLocation, &s.IssueType, &s.IssueProblem,
			&s.LoggedByName); err != nil {
			return nil, err
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

// loadIssueTVSwaps fills in TVSwaps on each of the given issues with a single
// query. Every issue ends up with a non-nil slice, so the JSON is always an
// array.
func loadIssueTVSwaps(ctx context.Context, db *sql.DB, issues []*Issue) error {
	if len(issues) == 0 {
		return nil
	}

	byID := make(map[int64]*Issue, len(issues))
	ids := make([]int64, 0, len(issues))
	for _, is := range issues {
		is.TVSwaps = []*TVSwap{}
		byID[is.ID] = is
		ids = append(ids, is.ID)
	}

	query := `SELECT ` + tvSwapColumns + `
		FROM issue_tv_swaps s
		WHERE s.issue_id = ANY($1::bigint[])
		ORDER BY s.created_at, s.id`

	rows, err := db.QueryContext(ctx, query, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		s, err := scanTVSwap(rows.Scan)
		if err != nil {
			return err
		}
		if is, ok := byID[s.IssueID]; ok {
			is.TVSwaps = append(is.TVSwaps, s)
		}
	}
	return rows.Err()
}
