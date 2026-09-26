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
	// SupportCompanyTelnet / SupportCompanyTelefonica are the two external
	// companies that support the building. The set is deliberately fixed in
	// the database CHECK constraint too, so adding a third company later means
	// a migration as well as a new constant here.
	SupportCompanyTelnet     = "telnet"
	SupportCompanyTelefonica = "telefonica"

	// A handover is tracked on its own timeline, separately from the issue's
	// Ok/Pending status, because an issue normally sits Pending for a long time
	// while we wait on the third party.
	SupportStatusPending = "pending"
	SupportStatusSolved  = "solved"
)

const (
	maxSupportRequestsPerIssue = 20
	maxSupportNotesLength      = 500
	maxSupportTicketIDLength   = 100
	maxSupportNameLength       = 100
)

// SupportRequest is one handover of an issue to an external company, with the
// company's own tracking reference attached.
type SupportRequest struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	IssueID   int64     `json:"issue_id"`
	Company   string    `json:"company"`
	Status    string    `json:"status"`

	// Telnet: who we reported the issue to, and who reported back that the fix
	// was good. Free text, because these are names of people on Telnet's side
	// (or whoever answered the phone) rather than anyone in our own user list.
	TelnetTechnician  *string `json:"telnet_technician"`
	TelnetConfirmedBy *string `json:"telnet_confirmed_by"`

	// Telefonica: the ticket id in their own system. They assign their own
	// technician, so there is no name to record for them.
	TelefonicaTicketID *string `json:"telefonica_ticket_id"`

	Notes     string `json:"notes"`
	CreatedBy int64  `json:"created_by"`
	Version   int    `json:"-"`
}

// SupportRequestUse is one handover as sent by the client when an issue is
// created or edited. A zero ID means "a handover that doesn't exist yet", so
// the server inserts it rather than trying to update an existing row.
type SupportRequestUse struct {
	ID                 int64   `json:"id,omitempty"`
	Company            string  `json:"company"`
	Status             string  `json:"status"`
	TelnetTechnician   *string `json:"telnet_technician"`
	TelnetConfirmedBy  *string `json:"telnet_confirmed_by"`
	TelefonicaTicketID *string `json:"telefonica_ticket_id"`
	Notes              string  `json:"notes"`
}

// trimmed returns the field's value with surrounding whitespace removed, and
// false when that leaves nothing — the "not provided" case for a text field.
func trimmed(s *string) (string, bool) {
	if s == nil {
		return "", false
	}
	v := strings.TrimSpace(*s)
	return v, v != ""
}

// ValidateSupportRequests checks the shape of the handovers sent with an issue
// create/update request: the company, the status, and that each company has
// the fields it actually needs. Length limits and the required-field rules are
// also enforced by the database (see migration 000016); the checks here exist
// to return a message naming the field.
func ValidateSupportRequests(v *validator.Validator, uses []SupportRequestUse) {
	v.Check(len(uses) <= maxSupportRequestsPerIssue, "support_requests",
		"must not contain more than 20 handovers")

	seenTickets := make(map[string]bool, len(uses))
	for _, u := range uses {
		field := "company"
		switch u.Company {
		case SupportCompanyTelnet:
			technician, ok := trimmed(u.TelnetTechnician)
			switch {
			case !ok:
				v.AddError(field, "a Telnet handover must name the technician it was reported to")
			case len(technician) > maxSupportNameLength:
				v.AddError(field, "the Telnet technician's name must not be more than 100 characters")
			}
			// The confirmation is only knowable once Telnet has called back, so
			// it is required exactly when the handover is marked solved.
			if confirmedBy, ok := trimmed(u.TelnetConfirmedBy); ok {
				if len(confirmedBy) > maxSupportNameLength {
					v.AddError(field, "the name of who reported the fix must not be more than 100 characters")
				}
			} else if u.Status == SupportStatusSolved {
				v.AddError(field, "a solved Telnet handover must name who reported it was fixed")
			}
		case SupportCompanyTelefonica:
			ticket, _ := trimmed(u.TelefonicaTicketID)
			switch {
			case ticket == "":
				v.AddError(field, "a Telefónica handover must include their ticket id")
			case len(ticket) > maxSupportTicketIDLength:
				v.AddError(field, "the Telefónica ticket id must not be more than 100 characters")
			case seenTickets[strings.ToLower(ticket)]:
				v.AddError(field, "the same Telefónica ticket id is listed more than once")
			default:
				seenTickets[strings.ToLower(ticket)] = true
			}
		default:
			v.AddError(field, "must be 'telnet' or 'telefonica'")
		}

		v.Check(u.Status == SupportStatusPending || u.Status == SupportStatusSolved,
			"status", "must be 'pending' or 'solved'")
		v.Check(len(u.Notes) <= maxSupportNotesLength, "notes",
			"must not be more than 500 characters")
	}
}

type SupportRequestModel struct {
	DB *sql.DB
}

// isDuplicateTelefonicaTicket reports whether err is the violation of the
// partial unique index that keeps one Telefónica ticket from being logged twice
// on the same issue. Matched against the index name in the driver's message,
// the same way the catalog models detect their duplicate-name errors.
func isDuplicateTelefonicaTicket(err error) bool {
	return err != nil &&
		strings.Contains(err.Error(), `violates unique constraint "issue_support_requests_telefonica_ticket_key"`)
}

// supportRequestColumns is the column list shared by every read of a handover,
// so the scan order below can never drift from the select list.
const supportRequestColumns = `
	s.id, s.created_at, s.updated_at, s.issue_id, s.company, s.status,
	s.telnet_technician, s.telnet_confirmed_by, s.telefonica_ticket_id,
	s.notes, s.created_by, s.version`

func scanSupportRequest(scan func(dest ...any) error) (*SupportRequest, error) {
	var s SupportRequest
	err := scan(
		&s.ID, &s.CreatedAt, &s.UpdatedAt, &s.IssueID, &s.Company, &s.Status,
		&s.TelnetTechnician, &s.TelnetConfirmedBy, &s.TelefonicaTicketID,
		&s.Notes, &s.CreatedBy, &s.Version,
	)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// SetForIssue makes the issue's handovers match the given selection: entries
// carrying an id are updated in place, entries without one are inserted, and
// handovers that are no longer listed are removed.
//
// The whole thing runs in one transaction so the issue is never left with half
// the handovers the form showed. Rows are matched on id *and* issue_id, so a
// client cannot claim (or overwrite) a handover belonging to another issue.
func (m SupportRequestModel) SetForIssue(issueID int64, uses []SupportRequestUse, loggedBy int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Must be non-nil: a nil slice becomes SQL NULL, and "<> ALL(NULL)" matches
	// no rows, so every handover would be deleted.
	keep := make([]int64, 0, len(uses))
	for _, u := range uses {
		if u.ID > 0 {
			keep = append(keep, u.ID)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM issue_support_requests
		WHERE issue_id = $1 AND id <> ALL($2::bigint[])`,
		issueID, pq.Array(keep),
	); err != nil {
		return err
	}

	for _, u := range uses {
		if u.ID > 0 {
			res, err := tx.ExecContext(ctx, `
				UPDATE issue_support_requests
				SET company=$3, status=$4, telnet_technician=$5,
				    telnet_confirmed_by=$6, telefonica_ticket_id=$7,
				    notes=$8, updated_at=NOW(), version=version+1
				WHERE id=$1 AND issue_id=$2`,
				u.ID, issueID, u.Company, u.Status, u.TelnetTechnician,
				u.TelnetConfirmedBy, u.TelefonicaTicketID, u.Notes,
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
			INSERT INTO issue_support_requests
				(issue_id, company, status, telnet_technician,
				 telnet_confirmed_by, telefonica_ticket_id, notes, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			issueID, u.Company, u.Status, u.TelnetTechnician,
			u.TelnetConfirmedBy, u.TelefonicaTicketID, u.Notes, loggedBy,
		); err != nil {
			// The Telefónica ticket is already logged on this issue — the
			// duplicate the form's own validation cannot see, because it does
			// not know what the other (unchanged) handovers look like.
			if isDuplicateTelefonicaTicket(err) {
				return ErrDuplicateSupportTicket
			}
			return err
		}
	}

	return tx.Commit()
}

// loadIssueSupportRequests fills in SupportRequests on each of the given issues
// with a single query. Every issue ends up with a non-nil slice, so the JSON is
// always an array.
func loadIssueSupportRequests(ctx context.Context, db *sql.DB, issues []*Issue) error {
	if len(issues) == 0 {
		return nil
	}

	byID := make(map[int64]*Issue, len(issues))
	ids := make([]int64, 0, len(issues))
	for _, is := range issues {
		is.SupportRequests = []*SupportRequest{}
		byID[is.ID] = is
		ids = append(ids, is.ID)
	}

	query := `SELECT ` + supportRequestColumns + `
		FROM issue_support_requests s
		WHERE s.issue_id = ANY($1::bigint[])
		ORDER BY s.created_at, s.id`

	rows, err := db.QueryContext(ctx, query, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		s, err := scanSupportRequest(rows.Scan)
		if err != nil {
			return err
		}
		if is, ok := byID[s.IssueID]; ok {
			is.SupportRequests = append(is.SupportRequests, s)
		}
	}
	return rows.Err()
}
