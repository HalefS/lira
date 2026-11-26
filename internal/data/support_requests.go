package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/HalefS/lira/internal/validator"
	"github.com/lib/pq"
)

const (
	// SupportCompanyTelnet / SupportCompanyTelefonica are the two companies
	// that existed before the catalog did. They are seeded by migration 000043
	// and are no longer a fixed set: the authority is now support_companies,
	// which a manager edits in Settings. The constants remain because the
	// database's coherence CHECK and a few fallbacks still name these slugs.
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

// supportTimeLayout is the format an <input type="datetime-local"> produces.
// It carries no zone, so it is parsed in the server's local time — the same
// clock that created_at's NOW() default uses, which keeps a typed time and a
// defaulted time meaning the same thing.
const supportTimeLayout = "2006-01-02T15:04"

// ParseSupportTime parses a "YYYY-MM-DDTHH:MM" value from the client into a
// timestamp in the server's local time. It reports ok=false for an empty or
// malformed value, which callers treat as "not provided" rather than an error,
// the same way the other optional fields in this file behave.
func ParseSupportTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation(supportTimeLayout, s, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// supportTimeLayoutPattern is the same value as a regular expression, used to
// tell "malformed" apart from "absent" so the message can say which.
var supportTimeLayoutPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$`)

var (
	supportDateFieldPattern   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	supportNumberFieldPattern = regexp.MustCompile(`^-?\d+(?:\.\d+)?$`)
)

// SupportRequest is one handover of an issue to an external company. The values
// of the company's configured fields travel in Fields, keyed by each field's
// stable key. Legacy columns remain on the row so handovers logged before the
// configurable setup can still be read and rolled back to migration 000043.
type SupportRequest struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	IssueID   int64     `json:"issue_id"`
	Company   string    `json:"company"`
	Status    string    `json:"status"`

	// The company as it reads today. Joined from support_companies on every
	// read rather than copied onto the handover, so renaming a company or
	// changing its colour updates the history without rewriting a row.
	CompanyName  string `json:"company_name"`
	CompanyColor string `json:"company_color"`

	Fields map[string]string `json:"fields"`

	// A compact display value derived from the configured fields, preferring a
	// tracking reference. Lists use it instead of guessing which company column
	// holds the useful detail.
	ReferenceSummary string `json:"reference_summary"`

	TelnetTechnician   *string `json:"telnet_technician"`
	TelnetConfirmedBy  *string `json:"telnet_confirmed_by"`
	TelefonicaTicketID *string `json:"telefonica_ticket_id"`
	Reference          *string `json:"reference"`

	// How long the company took: the wall-clock moment we reported the problem,
	// the moment they gave us a solution, and the derived duration. These are
	// the company's turnaround, not our technicians' working time, and are
	// deliberately kept out of every issue statistic.
	StartedAt       *time.Time `json:"started_at"`
	ResolvedAt      *time.Time `json:"resolved_at"`
	DurationMinutes *int       `json:"duration_minutes"`

	Notes     string `json:"notes"`
	CreatedBy int64  `json:"created_by"`
	Version   int    `json:"-"`
}

// SupportRequestFieldUse is one configured field's value as sent by the client.
type SupportRequestFieldUse struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// SupportRequestUse is one handover as sent by the client when an issue is
// created or edited. A zero ID means "a handover that doesn't exist yet", so
// the server inserts it rather than trying to update an existing row.
type SupportRequestUse struct {
	ID      int64                    `json:"id,omitempty"`
	Company string                   `json:"company"`
	Status  string                   `json:"status"`
	Fields  []SupportRequestFieldUse `json:"fields"`
	Notes   string                   `json:"notes"`

	// Legacy values are still accepted so an open form from before the field
	// setup was introduced can be saved. New forms send only Fields.
	TelnetTechnician   *string `json:"telnet_technician"`
	TelnetConfirmedBy  *string `json:"telnet_confirmed_by"`
	TelefonicaTicketID *string `json:"telefonica_ticket_id"`
	Reference          *string `json:"reference"`

	// "YYYY-MM-DDTHH:MM", the value an <input type="datetime-local"> produces.
	// Empty means "not provided". Parsed and validated by the handler before
	// they reach the model, which stores real timestamps.
	StartedAt  string `json:"started_at"`
	ResolvedAt string `json:"resolved_at"`
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

// requestFieldValues turns the client's list into a keyed map. Blank keys are
// ignored: a field without a key cannot belong to any company definition.
func requestFieldValues(uses []SupportRequestFieldUse) map[string]string {
	values := make(map[string]string, len(uses))
	for _, f := range uses {
		key := strings.TrimSpace(f.Key)
		if key == "" {
			continue
		}
		values[key] = strings.TrimSpace(f.Value)
	}
	return values
}

// ValidateSupportRequests checks the shape of the handovers sent with an issue
// create/update request: the company, its configured fields, the status and the
// two timestamps. The same field definitions drive the handover form, so a
// manager's required toggle is enforced here rather than only in the browser.
//
// companies is the catalog as it stands. A handover's company must be one of
// those slugs: the table is the authority now, not a pair of constants, and the
// foreign key would reject anything else with a 500.
func ValidateSupportRequests(v *validator.Validator, uses []SupportRequestUse, companies []*SupportCompany) {
	v.Check(len(uses) <= maxSupportRequestsPerIssue, "support_requests",
		"must not contain more than 20 handovers")

	bySlug := make(map[string]*SupportCompany, len(companies))
	for _, c := range companies {
		bySlug[c.Slug] = c
	}

	seenReferences := make(map[string]bool, len(uses))
	for _, u := range uses {
		field := "company"
		company, known := bySlug[u.Company]
		if !known {
			v.AddError(field, "must be one of the configured third-party companies")
		} else {
			values := requestFieldValues(u.Fields)
			for _, def := range company.Fields {
				if !def.Active {
					continue
				}
				value := values[def.Key]
				if value == "" {
					if def.Required || (def.Type == SupportFieldConfirmation && u.Status == SupportStatusSolved) {
						v.AddError(field, "a "+company.Name+" handover needs "+def.Label)
					}
					continue
				}

				switch def.Type {
				case SupportFieldText, SupportFieldName, SupportFieldConfirmation, SupportFieldReference:
					if len(value) > maxSupportNameLength {
						v.AddError(field, def.Label+" must not be more than 100 characters")
					}
				case SupportFieldDate:
					v.Check(supportDateFieldPattern.MatchString(value), field,
						def.Label+" must be a date in YYYY-MM-DD format")
				case SupportFieldNumber:
					v.Check(supportNumberFieldPattern.MatchString(value), field,
						def.Label+" must be a number")
				}

				if def.Type == SupportFieldReference {
					key := strings.ToLower(value)
					if seenReferences[key] {
						v.AddError(field, "the same "+company.Name+" reference is listed more than once")
					} else {
						seenReferences[key] = true
					}
				}
			}
		}

		v.Check(u.Status == SupportStatusPending || u.Status == SupportStatusSolved,
			"status", "must be 'pending' or 'solved'")
		v.Check(len(u.Notes) <= maxSupportNotesLength, "notes",
			"must not be more than 500 characters")

		// When the problem was reported to the company. Required for every
		// handover: a turnaround cannot be measured without a starting point.
		if u.StartedAt == "" {
			v.AddError(field, "must record when the problem was reported to the company")
		} else {
			v.Check(supportTimeLayoutPattern.MatchString(u.StartedAt), field,
				"the reported time must be a date and time")
		}
		if u.ResolvedAt != "" {
			v.Check(supportTimeLayoutPattern.MatchString(u.ResolvedAt), field,
				"the solution time must be a date and time")
		}
		// Marked solved without a solution time would leave a row that silently
		// drops out of the per-company averages, so it is refused instead.
		if u.Status == SupportStatusSolved && u.ResolvedAt == "" {
			v.AddError(field, "a solved handover must record when the company gave us a solution")
		}
	}
}

// SupportRequestRecord is a handover that has passed validation and had its
// timestamps parsed, ready to be written. The client never sends a duration:
// it is derived here from the two moments, the same way a technician's working
// time is derived on the issue itself, so the stored number is always the
// authoritative one the statistics read.
type SupportRequestRecord struct {
	ID               int64
	Company          string
	Status           string
	Fields           map[string]string
	ReferenceSummary string
	PrimaryReference *string
	Notes            string
	StartedAt        *time.Time
	ResolvedAt       *time.Time
	DurationMinutes  *int
}

// BuildSupportRequestRecord parses a client's handover into a record. The
// company supplies the field order so the list's one-line reference prefers a
// tracking number over a person's name. It reports ok=false when a supplied
// timestamp is malformed; an absent one is simply nil.
func BuildSupportRequestRecord(u SupportRequestUse, company *SupportCompany) (SupportRequestRecord, bool) {
	fields := requestFieldValues(u.Fields)
	rec := SupportRequestRecord{
		ID:      u.ID,
		Company: u.Company,
		Status:  u.Status,
		Fields:  fields,
		Notes:   u.Notes,
	}

	if company != nil {
		var summary, primary string
		for _, def := range company.Fields {
			if !def.Active {
				continue
			}
			value := fields[def.Key]
			if value == "" {
				continue
			}
			if summary == "" {
				summary = value
			}
			if def.Type == SupportFieldReference && primary == "" {
				primary = value
			}
		}
		if summary == "" {
			for _, value := range fields {
				if value != "" {
					summary = value
					break
				}
			}
		}
		rec.ReferenceSummary = summary
		if primary != "" {
			rec.PrimaryReference = &primary
		}
	}

	if u.StartedAt != "" {
		t, ok := ParseSupportTime(u.StartedAt)
		if !ok {
			return rec, false
		}
		rec.StartedAt = &t
	}
	if u.ResolvedAt != "" {
		t, ok := ParseSupportTime(u.ResolvedAt)
		if !ok {
			return rec, false
		}
		rec.ResolvedAt = &t
	}
	if rec.StartedAt != nil && rec.ResolvedAt != nil {
		minutes := int(rec.ResolvedAt.Sub(*rec.StartedAt).Minutes())
		rec.DurationMinutes = &minutes
	}
	return rec, true
}

type SupportRequestModel struct {
	DB *sql.DB
}

// isDuplicateSupportReference reports whether err is the violation of a unique
// index that keeps a company's reference from being logged twice on the same
// issue. The new index reads the configured reference field; the two older
// names are kept so a handover edited from pre-migration data is still caught.
func isDuplicateSupportReference(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, `violates unique constraint "issue_support_requests_primary_reference_key"`) ||
		strings.Contains(msg, `violates unique constraint "issue_support_requests_telefonica_ticket_key"`) ||
		strings.Contains(msg, `violates unique constraint "issue_support_requests_reference_key"`)
}

// supportRequestColumns is the column list shared by every read of a handover,
// so the scan order below can never drift from the select list. The company's
// name and colour are joined in rather than stored on the row: a rename or a
// recolour then applies to the whole history at once.
const supportRequestColumns = `
	s.id, s.created_at, s.updated_at, s.issue_id, s.company, s.status,
	s.telnet_technician, s.telnet_confirmed_by, s.telefonica_ticket_id, s.reference,
	s.started_at, s.resolved_at, s.duration_minutes,
	s.notes, s.created_by, s.version, s.fields, s.reference_summary,
	c.name, c.color`

func scanSupportRequest(scan func(dest ...any) error) (*SupportRequest, error) {
	var s SupportRequest
	var fields []byte
	err := scan(
		&s.ID, &s.CreatedAt, &s.UpdatedAt, &s.IssueID, &s.Company, &s.Status,
		&s.TelnetTechnician, &s.TelnetConfirmedBy, &s.TelefonicaTicketID, &s.Reference,
		&s.StartedAt, &s.ResolvedAt, &s.DurationMinutes,
		&s.Notes, &s.CreatedBy, &s.Version, &fields, &s.ReferenceSummary,
		&s.CompanyName, &s.CompanyColor,
	)
	if err != nil {
		return nil, err
	}
	s.Fields = map[string]string{}
	if len(fields) > 0 {
		if err := json.Unmarshal(fields, &s.Fields); err != nil {
			return nil, err
		}
	}
	if s.Fields == nil {
		s.Fields = map[string]string{}
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
func (m SupportRequestModel) SetForIssue(issueID int64, uses []SupportRequestRecord, loggedBy int64) error {
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
		fieldJSON, err := json.Marshal(u.Fields)
		if err != nil {
			return err
		}
		if u.Fields == nil {
			fieldJSON = []byte("{}")
		}

		if u.ID > 0 {
			res, err := tx.ExecContext(ctx, `
				UPDATE issue_support_requests
				SET company=$3, status=$4, fields=$5, reference_summary=$6,
				    primary_reference=$7, started_at=$8, resolved_at=$9,
				    duration_minutes=$10, notes=$11, updated_at=NOW(),
				    version=version+1
				WHERE id=$1 AND issue_id=$2`,
				u.ID, issueID, u.Company, u.Status, string(fieldJSON),
				u.ReferenceSummary, u.PrimaryReference,
				u.StartedAt, u.ResolvedAt, u.DurationMinutes, u.Notes,
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
				(issue_id, company, status, fields, reference_summary,
				 primary_reference, started_at, resolved_at, duration_minutes,
				 notes, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			issueID, u.Company, u.Status, string(fieldJSON), u.ReferenceSummary,
			u.PrimaryReference, u.StartedAt, u.ResolvedAt, u.DurationMinutes,
			u.Notes, loggedBy,
		); err != nil {
			// The reference is already logged on this issue — the duplicate the
			// form's own validation cannot see, because it does not know what
			// the other (unchanged) handovers look like.
			if isDuplicateSupportReference(err) {
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
		INNER JOIN support_companies c ON c.slug = s.company
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
