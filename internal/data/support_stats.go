package data

import (
	"context"
	"database/sql"
	"time"
)

// SupportStats is the third-party turnaround picture for a date range: one row
// per company, plus the same figures across both.
//
// These numbers answer "how slow is Telnet?", which is the opposite question to
// the issue statistics, so they are computed from this table alone and are
// never mixed into the team's average resolution time or the analytics page.
type SupportStats struct {
	From      string                 `json:"from"`
	To        string                 `json:"to"`
	ByCompany []*SupportCompanyStats `json:"by_company"`
	Overall   *SupportCompanyStats   `json:"overall"`
}

// SupportCompanyStats is the summary for one company (Company holds the value
// for that row, or is empty on the combined Overall). Only handovers that carry
// a duration — that is, ones that were actually resolved — contribute to the
// time figures; a handover still waiting on the company has no answer yet, so
// it counts towards Total and Pending but cannot drag the average.
type SupportCompanyStats struct {
	Company  string `json:"company"`
	Total    int    `json:"total"`
	Pending  int    `json:"pending"`
	Resolved int    `json:"resolved"`
	// Timed is how many resolved handovers recorded a duration. It is lower
	// than Resolved only for rows created before durations existed.
	Timed int `json:"timed"`
	// The four time figures below are in minutes, and are zero when Timed is.
	// Median is here because one very slow handover skews an average badly.
	AvgMinutes     float64 `json:"avg_minutes"`
	MedianMinutes  float64 `json:"median_minutes"`
	FastestMinutes int     `json:"fastest_minutes"`
	SlowestMinutes int     `json:"slowest_minutes"`
}

// summarize computes one row of the summary. An empty company means both
// companies together, which is why the filter is a parameter rather than part
// of a fixed statement: the same query answers the per-company and the overall
// row, so the two can never disagree about what they count.
func (m SupportRequestModel) summarize(ctx context.Context, from, to, company string) (*SupportCompanyStats, error) {
	query := `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'pending'),
			COUNT(*) FILTER (WHERE status = 'solved'),
			COUNT(duration_minutes),
			COALESCE(AVG(duration_minutes), 0),
			COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY duration_minutes), 0),
			COALESCE(MIN(duration_minutes), 0),
			COALESCE(MAX(duration_minutes), 0)
		FROM issue_support_requests
		WHERE ($1 = '' OR created_at::date >= $1::date)
		  AND ($2 = '' OR created_at::date <= $2::date)
		  AND ($3 = '' OR company = $3)`

	var s SupportCompanyStats
	s.Company = company
	err := m.DB.QueryRowContext(ctx, query, from, to, company).Scan(
		&s.Total, &s.Pending, &s.Resolved, &s.Timed,
		&s.AvgMinutes, &s.MedianMinutes, &s.FastestMinutes, &s.SlowestMinutes,
	)
	if err != nil {
		return nil, err
	}
	s.AvgMinutes = round1(s.AvgMinutes)
	s.MedianMinutes = round1(s.MedianMinutes)
	return &s, nil
}

// GetStats returns the per-company summary plus the combined one for the given
// date range. An empty from or to is unbounded, so omitting both reports
// everything ever logged.
func (m SupportRequestModel) GetStats(from, to string) (*SupportStats, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stats := &SupportStats{From: from, To: to, ByCompany: []*SupportCompanyStats{}}

	// Both companies are always returned, even with nothing logged, so the page
	// can show "0 handovers" instead of silently omitting one of them.
	for _, company := range []string{SupportCompanyTelnet, SupportCompanyTelefonica} {
		s, err := m.summarize(ctx, from, to, company)
		if err != nil {
			return nil, err
		}
		stats.ByCompany = append(stats.ByCompany, s)
	}

	overall, err := m.summarize(ctx, from, to, "")
	if err != nil {
		return nil, err
	}
	stats.Overall = overall

	return stats, nil
}

// SupportListRow is one handover joined to the issue it belongs to, which is
// what the support page lists.
type SupportListRow struct {
	ID              int64      `json:"id"`
	CreatedAt       time.Time  `json:"created_at"`
	Company         string     `json:"company"`
	Status          string     `json:"status"`
	StartedAt       *time.Time `json:"started_at"`
	ResolvedAt      *time.Time `json:"resolved_at"`
	DurationMinutes *int       `json:"duration_minutes"`
	// Reference is the human-readable identifier of the handover: the
	// Telefonica ticket id, or the Telnet technician's name.
	Reference     string `json:"reference"`
	Notes         string `json:"notes"`
	IssueID       int64  `json:"issue_id"`
	IssueProblem  string `json:"issue_problem"`
	IssueMode     string `json:"issue_mode"`
	IssueLocation string `json:"issue_location"`
}

// GetList returns up to limit handovers in the date range, newest first, joined
// to their issue so the page can say which problem each one is about, plus the
// total number in the range.
func (m SupportRequestModel) GetList(from, to string, limit int) ([]*SupportListRow, int, error) {
	ctx, cancel := listContext()
	defer cancel()
	return m.list(ctx, from, to, limit)
}

// GetAll returns every handover in the range with no page limit. The printable
// report uses it: a PDF is an archive of the period, so a capped list would look
// complete while quietly leaving rows out.
func (m SupportRequestModel) GetAll(from, to string) ([]*SupportListRow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, _, err := m.list(ctx, from, to, 0)
	return rows, err
}

// list is the shared implementation behind GetList and GetAll. A limit of zero
// or less means every row, matching the convention in IssueModel.GetReport.
func (m SupportRequestModel) list(ctx context.Context, from, to string, limit int) ([]*SupportListRow, int, error) {
	fromWhere := `
		FROM issue_support_requests s
		INNER JOIN issues i ON i.id = s.issue_id
		WHERE ($1 = '' OR s.created_at::date >= $1::date)
		  AND ($2 = '' OR s.created_at::date <= $2::date)`

	// The page needs the count to drive "load more"; the report needs every row
	// and has no use for it, so do not pay for a second query there.
	total := 0
	var err error
	if limit > 0 {
		total, err = CountMatching(ctx, m.DB, fromWhere, []any{from, to})
		if err != nil {
			return nil, 0, err
		}
	}

	query := `
		SELECT s.id, s.created_at, s.company, s.status,
		       s.started_at, s.resolved_at, s.duration_minutes, s.notes,
		       COALESCE(s.telefonica_ticket_id, s.telnet_technician, ''),
		       i.id, i.problem, i.mode, i.location
		` + fromWhere + `
		ORDER BY s.started_at DESC NULLS LAST, s.id DESC`

	var rows *sql.Rows
	if limit > 0 {
		query += "\n\t\tLIMIT $3"
		rows, err = m.DB.QueryContext(ctx, query, from, to, limit)
	} else {
		rows, err = m.DB.QueryContext(ctx, query, from, to)
	}
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []*SupportListRow{}
	for rows.Next() {
		var r SupportListRow
		if err := rows.Scan(
			&r.ID, &r.CreatedAt, &r.Company, &r.Status,
			&r.StartedAt, &r.ResolvedAt, &r.DurationMinutes, &r.Notes,
			&r.Reference,
			&r.IssueID, &r.IssueProblem, &r.IssueMode, &r.IssueLocation,
		); err != nil {
			return nil, 0, err
		}
		out = append(out, &r)
	}
	return out, total, rows.Err()
}

// SupportReport is everything the printable third-party support report shows:
// the same per-company summary the page displays, the full list of handovers
// behind it, and the window they were taken over. It is the mirror of the issue
// reports, which deliberately leave these handovers out.
type SupportReport struct {
	From        string            `json:"from"`
	To          string            `json:"to"`
	GeneratedAt time.Time         `json:"generated_at"`
	Stats       *SupportStats     `json:"stats"`
	Rows        []*SupportListRow `json:"rows"`
}

// GetReport builds the printable report for a date range. An empty from or to is
// unbounded, matching the support page's "All time" option.
func (m SupportRequestModel) GetReport(from, to string) (*SupportReport, error) {
	stats, err := m.GetStats(from, to)
	if err != nil {
		return nil, err
	}
	rows, err := m.GetAll(from, to)
	if err != nil {
		return nil, err
	}
	return &SupportReport{
		From:        from,
		To:          to,
		GeneratedAt: time.Now(),
		Stats:       stats,
		Rows:        rows,
	}, nil
}
