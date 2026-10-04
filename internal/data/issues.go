package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/HalefS/lira/internal/validator"
)

type Issue struct {
	ID          int64     `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	Mode        string    `json:"mode"`
	Location    string    `json:"location"`
	Type        string    `json:"type"`
	Problem     string    `json:"problem"`
	Resolution  string    `json:"resolution"`
	TimeMinutes int       `json:"time_minutes"`
	StartTime   *string   `json:"start_time"`
	EndTime     *string   `json:"end_time"`
	// Which Melia Connecta Agent reported this issue, and which one we
	// confirmed the fix with. Deliberately excluded from any table/list
	// rendering in the UI — only the add/edit issue form surfaces these.
	ReportedByAgent  *string `json:"reported_by_agent"`
	ConfirmedByAgent *string `json:"confirmed_by_agent"`
	Status           string  `json:"status"`
	LoggedBy         int64   `json:"logged_by"`
	LoggedByName     string  `json:"logged_by_name"`
	LoggedByIdx      int     `json:"logged_by_idx"`
	Version          int     `json:"-"`
	// FalsePositive marks a false alarm: the issue was reported but nothing
	// was actually wrong. Counted separately in the analytics.
	FalsePositive bool `json:"false_positive"`
	// Consumables used while resolving this issue (batteries, remotes,
	// phones, ...). Always an array in the JSON, empty when none were used.
	Consumables []*IssueConsumable `json:"consumables"`
	// Handovers of this issue to Telnet or Telefonica, each with the
	// reference that company gave us. Always an array in the JSON, empty when
	// the issue was handled entirely in-house.
	SupportRequests []*SupportRequest `json:"support_requests"`
	// TVs that were moved between rooms while fixing this issue, because the
	// room's own set was unusable and no spare was in stock. Always an array in
	// the JSON, empty when nothing was swapped.
	TVSwaps []*TVSwap `json:"tv_swaps"`
}

type IssueFilters struct {
	Mode   string
	Status string
	Type   string
	Search string
	Date   string
}

type Stats struct {
	TotalIssues  int            `json:"total_issues"`
	Resolved     int            `json:"resolved"`
	Pending      int            `json:"pending"`
	AvgMinutes   float64        `json:"avg_minutes"`
	ByType       map[string]int `json:"by_type"`
	ByTechnician []TechStat     `json:"by_technician"`
}

type TechStat struct {
	UserID    int64  `json:"user_id"`
	Name      string `json:"name"`
	AvatarIdx int    `json:"avatar_idx"`
	Count     int    `json:"count"`
}

func ValidateIssue(v *validator.Validator, issue *Issue) {
	v.Check(issue.Mode == "apt" || issue.Mode == "dept", "mode", "must be 'apt' or 'dept'")
	v.Check(issue.Location != "", "location", "must be provided")
	v.Check(len(issue.Location) <= 100, "location", "must not be more than 100 characters")
	v.Check(issue.Type != "", "type", "must be provided")
	v.Check(issue.Problem != "", "problem", "must be provided")
	v.Check(len(issue.Problem) <= 500, "problem", "must not be more than 500 characters")
	v.Check(len(issue.Resolution) <= 500, "resolution", "must not be more than 500 characters")
	v.Check(issue.TimeMinutes >= 0, "time_minutes", "must be zero or greater")
	v.Check(issue.Status == "Ok" || issue.Status == "Pending", "status", "must be 'Ok' or 'Pending'")
}

var clockTimePattern = regexp.MustCompile(`^([01]\d|2[0-3]):([0-5]\d)$`)

// ValidateClockTime checks a "HH:MM" 24-hour string, matching exactly what
// an <input type="time"> element produces.
func ValidateClockTime(v *validator.Validator, field, value string) {
	v.Check(clockTimePattern.MatchString(value), field, "must be a time in HH:MM 24-hour format")
}

func parseClockTime(s string) (hour, minute int, ok bool) {
	m := clockTimePattern.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, false
	}
	hour, _ = strconv.Atoi(m[1])
	minute, _ = strconv.Atoi(m[2])
	return hour, minute, true
}

// ComputeDurationMinutes returns the whole minutes between two "HH:MM"
// clock times, treating an end time earlier than the start time as
// crossing midnight (e.g. an overnight shift). It returns ok=false when
// either input is malformed or the resulting duration is zero — this is
// the same rule the frontend applies, kept in sync here since this is the
// authoritative, server-side calculation that actually gets stored.
func ComputeDurationMinutes(start, end string) (minutes int, ok bool) {
	sh, sm, ok1 := parseClockTime(start)
	eh, em, ok2 := parseClockTime(end)
	if !ok1 || !ok2 {
		return 0, false
	}
	diff := (eh*60 + em) - (sh*60 + sm)
	if diff < 0 {
		diff += 24 * 60
	}
	if diff <= 0 {
		return 0, false
	}
	return diff, true
}

type IssueModel struct {
	DB *sql.DB
}

// loadIssueChildren fills in the child collections attached to every issue —
// the consumables used on it, the handovers to Telnet / Telefonica, and the TVs
// moved between rooms — with one query each, so a whole list of issues costs
// three extra round-trips rather than three per issue.
func loadIssueChildren(ctx context.Context, db *sql.DB, issues []*Issue) error {
	if err := loadIssueConsumables(ctx, db, issues); err != nil {
		return err
	}
	if err := loadIssueSupportRequests(ctx, db, issues); err != nil {
		return err
	}
	return loadIssueTVSwaps(ctx, db, issues)
}

func (m IssueModel) Insert(issue *Issue) error {
	query := `
		INSERT INTO issues (mode, location, type, problem, resolution, time_minutes, status, logged_by, start_time, end_time, reported_by_agent, confirmed_by_agent, false_positive)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id, created_at, version`

	args := []any{
		issue.Mode, issue.Location, issue.Type, issue.Problem,
		issue.Resolution, issue.TimeMinutes, issue.Status, issue.LoggedBy,
		issue.StartTime, issue.EndTime, issue.ReportedByAgent, issue.ConfirmedByAgent,
		issue.FalsePositive,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	return m.DB.QueryRowContext(ctx, query, args...).Scan(&issue.ID, &issue.CreatedAt, &issue.Version)
}

func (m IssueModel) Get(id int64) (*Issue, error) {
	query := `
		SELECT i.id, i.created_at, i.mode, i.location, i.type, i.problem,
		       i.resolution, i.time_minutes, i.start_time, i.end_time, i.reported_by_agent, i.confirmed_by_agent, i.status, i.logged_by,
		       u.name, u.avatar_idx, i.version, i.false_positive
		FROM issues i
		INNER JOIN users u ON i.logged_by = u.id
		WHERE i.id = $1`

	var issue Issue
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query, id).Scan(
		&issue.ID, &issue.CreatedAt, &issue.Mode, &issue.Location, &issue.Type,
		&issue.Problem, &issue.Resolution, &issue.TimeMinutes, &issue.StartTime, &issue.EndTime, &issue.ReportedByAgent, &issue.ConfirmedByAgent, &issue.Status,
		&issue.LoggedBy, &issue.LoggedByName, &issue.LoggedByIdx, &issue.Version, &issue.FalsePositive,
	)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil, ErrRecordNotFound
		default:
			return nil, err
		}
	}

	if err := loadIssueChildren(ctx, m.DB, []*Issue{&issue}); err != nil {
		return nil, err
	}
	return &issue, nil
}

// GetAll returns up to limit issues matching the filters, plus the total number
// that match them ignoring the limit.
//
// The total is not a convenience: the client needs it to know whether "load more"
// has anything left to load, and computing it here from the same WHERE as the
// rows is the only way that number stays true.
// carryPendingOntoToday reports whether a date filter is asking for the current
// day, which is the one date that behaves differently from the others.
//
// An issue stays on today's list until it is solved. Today is the list somebody
// actually works through, so an issue logged on Monday and still pending on
// Wednesday belongs on Wednesday's list -- otherwise it quietly stops being
// visible anywhere while it is still somebody's job, which is the whole problem
// this fixes.
//
// Only the current day does this. Asked for October 1st, you get October 1st:
// a day being looked back on has to stay the day it was, or the history page and
// every report built on it stop meaning anything.
//
// The comparison is against the server's calendar via Today, the same source the
// LCU gate and the maintenance due dates use, so "today" cannot mean two
// different days inside one process. A browser sitting in another timezone can
// ask for a day that is not the server's today, and it then gets that day alone
// -- the old behaviour -- rather than a list it did not ask for.
func carryPendingOntoToday(date string, today time.Time) bool {
	return date != "" && date == today.Format(time.DateOnly)
}

func (m IssueModel) GetAll(f IssueFilters, limit int) ([]*Issue, int, error) {
	conditions := []string{"1=1"}
	args := []any{}
	argIdx := 1

	if f.Mode != "" {
		conditions = append(conditions, fmt.Sprintf("i.mode = $%d", argIdx))
		args = append(args, f.Mode)
		argIdx++
	}
	if f.Status != "" {
		conditions = append(conditions, fmt.Sprintf("i.status = $%d", argIdx))
		args = append(args, f.Status)
		argIdx++
	}
	if f.Type != "" {
		conditions = append(conditions, fmt.Sprintf("i.type = $%d", argIdx))
		args = append(args, f.Type)
		argIdx++
	}
	if f.Date != "" {
		// created_at is assigned by the database on insert and never written
		// again afterwards, so no row can sit in the future and the pending arm
		// needs no upper bound of its own.
		//
		// The two arms are one parenthesised condition rather than two separate
		// ones, so that a status filter still narrows the result rather than being
		// silently widened by the carry-forward.
		if carryPendingOntoToday(f.Date, Today()) {
			conditions = append(conditions, fmt.Sprintf(
				"(i.created_at::date = $%d OR i.status = 'Pending')", argIdx))
		} else {
			conditions = append(conditions, fmt.Sprintf("i.created_at::date = $%d", argIdx))
		}
		args = append(args, f.Date)
		argIdx++
	}
	if f.Search != "" {
		conditions = append(conditions, fmt.Sprintf(
			"(i.location ILIKE $%d OR i.problem ILIKE $%d OR i.type ILIKE $%d OR u.name ILIKE $%d)",
			argIdx, argIdx+1, argIdx+2, argIdx+3,
		))
		like := "%" + f.Search + "%"
		args = append(args, like, like, like, like)
		argIdx += 4
	}

	// One FROM/WHERE string, used verbatim by both the count and the rows, so
	// the "total" can never disagree with the list it is counting.
	fromWhere := `FROM issues i
		INNER JOIN users u ON i.logged_by = u.id
		WHERE ` + strings.Join(conditions, " AND ")

	ctx, cancel := listContext()
	defer cancel()

	total, err := CountMatching(ctx, m.DB, fromWhere, args)
	if err != nil {
		return nil, 0, err
	}

	// LIMIT is bound rather than interpolated, so a hostile or mistaken limit
	// cannot become part of the query.
	query := fmt.Sprintf(`
		SELECT i.id, i.created_at, i.mode, i.location, i.type, i.problem,
		       i.resolution, i.time_minutes, i.start_time, i.end_time, i.reported_by_agent, i.confirmed_by_agent, i.status, i.logged_by,
		       u.name, u.avatar_idx, i.version, i.false_positive
		%s
		ORDER BY i.created_at DESC, i.id DESC
		LIMIT $%d`, fromWhere, argIdx)

	rows, err := m.DB.QueryContext(ctx, query, append(append([]any{}, args...), limit)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	issues := []*Issue{}
	for rows.Next() {
		var issue Issue
		err := rows.Scan(
			&issue.ID, &issue.CreatedAt, &issue.Mode, &issue.Location, &issue.Type,
			&issue.Problem, &issue.Resolution, &issue.TimeMinutes, &issue.StartTime, &issue.EndTime, &issue.ReportedByAgent, &issue.ConfirmedByAgent, &issue.Status,
			&issue.LoggedBy, &issue.LoggedByName, &issue.LoggedByIdx, &issue.Version, &issue.FalsePositive,
		)
		if err != nil {
			return nil, 0, err
		}
		issues = append(issues, &issue)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}

	if err = loadIssueChildren(ctx, m.DB, issues); err != nil {
		return nil, 0, err
	}
	return issues, total, nil
}

func (m IssueModel) Update(issue *Issue) error {
	query := `
		UPDATE issues
		SET mode=$1, location=$2, type=$3, problem=$4, resolution=$5,
		    time_minutes=$6, status=$7, start_time=$8, end_time=$9,
		    reported_by_agent=$10, confirmed_by_agent=$11, false_positive=$12, version=version+1
		WHERE id=$13 AND version=$14
		RETURNING version`

	args := []any{
		issue.Mode, issue.Location, issue.Type, issue.Problem,
		issue.Resolution, issue.TimeMinutes, issue.Status,
		issue.StartTime, issue.EndTime,
		issue.ReportedByAgent, issue.ConfirmedByAgent, issue.FalsePositive,
		issue.ID, issue.Version,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query, args...).Scan(&issue.Version)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return ErrEditConflict
		default:
			return err
		}
	}
	return nil
}

// Delete removes an issue. The consumables it recorded are deleted with it by
// the foreign key, so their stock is handed back first — in the same
// transaction, so the count can never be credited for an issue that still
// exists, or left short for one that is already gone.
func (m IssueModel) Delete(id int64, deletedBy int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	used, err := quantitiesByItem(ctx, tx, id)
	if err != nil {
		return err
	}
	for itemID, qty := range used {
		if err := applyStock(ctx, tx, itemID, qty, StockReasonRemoved, id, deletedBy); err != nil {
			return err
		}
	}

	result, err := tx.ExecContext(ctx, `DELETE FROM issues WHERE id = $1`, id)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrRecordNotFound
	}
	return tx.Commit()
}

func (m IssueModel) GetStats(date string) (*Stats, error) {
	// If no date given, use today
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stats := &Stats{
		ByType:       make(map[string]int),
		ByTechnician: []TechStat{},
	}

	// Summary query
	summaryQuery := `
		SELECT
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE status = 'Ok') AS resolved,
			COUNT(*) FILTER (WHERE status = 'Pending') AS pending,
			COALESCE(AVG(time_minutes), 0) AS avg_minutes
		FROM issues
		WHERE created_at::date = $1`

	err := m.DB.QueryRowContext(ctx, summaryQuery, date).Scan(
		&stats.TotalIssues, &stats.Resolved, &stats.Pending, &stats.AvgMinutes,
	)
	if err != nil {
		return nil, err
	}

	// By type
	typeQuery := `
		SELECT type, COUNT(*) FROM issues
		WHERE created_at::date = $1
		GROUP BY type ORDER BY COUNT(*) DESC`

	typeRows, err := m.DB.QueryContext(ctx, typeQuery, date)
	if err != nil {
		return nil, err
	}
	defer typeRows.Close()
	for typeRows.Next() {
		var t string
		var c int
		if err := typeRows.Scan(&t, &c); err != nil {
			return nil, err
		}
		stats.ByType[t] = c
	}

	// By technician. Managers are included too when they have logged something
	// on this date, because in practice managers do log their own issues and
	// their workload is just as real as anyone else's. Technicians are listed
	// either way, including on a day they logged nothing, so the chart still
	// shows who is on shift. This is why the role test is in HAVING rather than
	// WHERE: it needs the per-user count to decide.
	techQuery := `
		SELECT u.id, u.name, u.avatar_idx, COUNT(i.id) AS cnt
		FROM users u
		LEFT JOIN issues i ON i.logged_by = u.id AND i.created_at::date = $1
		GROUP BY u.id, u.name, u.avatar_idx, u.role
		HAVING u.role = 'technician' OR COUNT(i.id) > 0
		ORDER BY cnt DESC`

	techRows, err := m.DB.QueryContext(ctx, techQuery, date)
	if err != nil {
		return nil, err
	}
	defer techRows.Close()
	for techRows.Next() {
		var ts TechStat
		if err := techRows.Scan(&ts.UserID, &ts.Name, &ts.AvatarIdx, &ts.Count); err != nil {
			return nil, err
		}
		stats.ByTechnician = append(stats.ByTechnician, ts)
	}

	return stats, nil
}

// UserStats holds personal stats for a single user.
type UserStats struct {
	TotalIssues int     `json:"total_issues"`
	Resolved    int     `json:"resolved"`
	Pending     int     `json:"pending"`
	AvgMinutes  float64 `json:"avg_minutes"`
	ThisWeek    int     `json:"this_week"`
	ThisMonth   int     `json:"this_month"`
}

func (m IssueModel) GetUserStats(userID int64) (*UserStats, error) {
	query := `
		SELECT
			COUNT(*)                                             AS total,
			COUNT(*) FILTER (WHERE status = 'Ok')               AS resolved,
			COUNT(*) FILTER (WHERE status = 'Pending')          AS pending,
			COALESCE(AVG(time_minutes), 0)                      AS avg_minutes,
			COUNT(*) FILTER (WHERE created_at >= NOW() - INTERVAL '7 days')  AS this_week,
			COUNT(*) FILTER (WHERE created_at >= NOW() - INTERVAL '30 days') AS this_month
		FROM issues
		WHERE logged_by = $1`

	var s UserStats
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query, userID).Scan(
		&s.TotalIssues, &s.Resolved, &s.Pending,
		&s.AvgMinutes, &s.ThisWeek, &s.ThisMonth,
	)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (m IssueModel) GetByUser(userID int64, limit int) ([]*Issue, error) {
	query := `
		SELECT i.id, i.created_at, i.mode, i.location, i.type, i.problem,
		       i.resolution, i.time_minutes, i.start_time, i.end_time, i.reported_by_agent, i.confirmed_by_agent, i.status, i.logged_by,
		       u.name, u.avatar_idx, i.version, i.false_positive
		FROM issues i
		INNER JOIN users u ON i.logged_by = u.id
		WHERE i.logged_by = $1
		ORDER BY i.created_at DESC
		LIMIT $2`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, query, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var issues []*Issue
	for rows.Next() {
		var issue Issue
		err := rows.Scan(
			&issue.ID, &issue.CreatedAt, &issue.Mode, &issue.Location, &issue.Type,
			&issue.Problem, &issue.Resolution, &issue.TimeMinutes, &issue.StartTime, &issue.EndTime, &issue.ReportedByAgent, &issue.ConfirmedByAgent, &issue.Status,
			&issue.LoggedBy, &issue.LoggedByName, &issue.LoggedByIdx, &issue.Version, &issue.FalsePositive,
		)
		if err != nil {
			return nil, err
		}
		issues = append(issues, &issue)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	if err = loadIssueChildren(ctx, m.DB, issues); err != nil {
		return nil, err
	}
	return issues, nil
}

// ── Recurring-issue detection ───────────────────────────────────────────────

type DuplicateIssue struct {
	ID           int64     `json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	Status       string    `json:"status"`
	LoggedByName string    `json:"logged_by_name"`
}

// GetRecentDuplicates returns other issues of the same type, logged against
// the same location (case-insensitively), within windowHours of now.
// excludeID lets an issue being edited exclude itself from its own results
// (pass 0 when logging a brand new issue).
func (m IssueModel) GetRecentDuplicates(mode, location, issueType string, windowHours int, excludeID int64) ([]*DuplicateIssue, error) {
	query := `
		SELECT i.id, i.created_at, i.status, u.name
		FROM issues i
		INNER JOIN users u ON i.logged_by = u.id
		WHERE i.mode = $1
		  AND LOWER(i.location) = LOWER($2)
		  AND i.type = $3
		  AND i.created_at >= NOW() - ($4 * INTERVAL '1 hour')
		  AND i.id != $5
		ORDER BY i.created_at DESC`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, query, mode, location, issueType, windowHours, excludeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*DuplicateIssue
	for rows.Next() {
		var d DuplicateIssue
		if err := rows.Scan(&d.ID, &d.CreatedAt, &d.Status, &d.LoggedByName); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}

// RecurringGroupIssue is one occurrence within a recurring-issue alert —
// used by RecurringAlertModel (see recurring_alerts.go) to show who logged
// each occurrence, when, and how it was resolved.
type RecurringGroupIssue struct {
	ID           int64     `json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	Problem      string    `json:"problem"`
	Resolution   string    `json:"resolution"`
	Status       string    `json:"status"`
	LoggedByName string    `json:"logged_by_name"`
}

// ── Daily Report ──────────────────────────────────────────────────────────────

type DailyReport struct {
	Date         string         `json:"date"`
	GeneratedAt  time.Time      `json:"generated_at"`
	Summary      ReportSummary  `json:"summary"`
	AptIssues    []*Issue       `json:"apt_issues"`
	DeptIssues   []*Issue       `json:"dept_issues"`
	ByType       map[string]int `json:"by_type"`
	ByMode       map[string]int `json:"by_mode"`
	ByStatus     map[string]int `json:"by_status"`
	ByTechnician []TechStat     `json:"by_technician"`
	// TypeColors maps an issue type's name to the hex color a manager picked
	// for it, so anything drawing a type (the report page, the printed PDF)
	// can colour it the same way instead of falling back to a hardcoded
	// palette that drifts out of sync with the admin panel.
	TypeColors map[string]string `json:"type_colors"`
}

type ReportSummary struct {
	TotalIssues    int     `json:"total_issues"`
	AptIssues      int     `json:"apt_issues"`
	DeptIssues     int     `json:"dept_issues"`
	Resolved       int     `json:"resolved"`
	Pending        int     `json:"pending"`
	ResolutionRate float64 `json:"resolution_rate"`
	AvgMinutes     float64 `json:"avg_minutes"`
	TotalMinutes   int     `json:"total_minutes"`
	FastestMinutes int     `json:"fastest_minutes"`
	SlowestMinutes int     `json:"slowest_minutes"`
	FalsePositives int     `json:"false_positives"`
}

// GetDailyReport builds one day's report. limit caps each of the two issue
// lists independently; the summary, the breakdown maps and the per-technician
// table are all aggregates over the whole day and ignore it, so the headline
// numbers and the charts keep describing the day as more rows are loaded.
func (m IssueModel) GetDailyReport(date string, limit int) (*DailyReport, error) {
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	report := &DailyReport{
		Date:        date,
		GeneratedAt: time.Now(),
		ByType:      make(map[string]int),
		ByMode:      make(map[string]int),
		ByStatus:    make(map[string]int),
		TypeColors:  make(map[string]string),
	}

	// ── Issue type colors ──
	// Fetched up front and unconditionally: a type that logged no issues today
	// can still be drawn in the type breakdown of a previous day, and a type
	// deleted from the catalog keeps whatever color it had.
	colorRows, err := m.DB.QueryContext(ctx, `SELECT name, color FROM issue_types`)
	if err != nil {
		return nil, err
	}
	for colorRows.Next() {
		var name, color string
		if err := colorRows.Scan(&name, &color); err != nil {
			colorRows.Close()
			return nil, err
		}
		report.TypeColors[name] = color
	}
	if err := colorRows.Err(); err != nil {
		colorRows.Close()
		return nil, err
	}
	colorRows.Close()

	// ── Summary ──
	summaryQ := `
		SELECT
			COUNT(*)                                               AS total,
			COUNT(*) FILTER (WHERE mode = 'apt')                  AS apt_count,
			COUNT(*) FILTER (WHERE mode = 'dept')                 AS dept_count,
			COUNT(*) FILTER (WHERE status = 'Ok')                 AS resolved,
			COUNT(*) FILTER (WHERE status = 'Pending')            AS pending,
			COALESCE(AVG(time_minutes), 0)                        AS avg_min,
			COALESCE(SUM(time_minutes), 0)                        AS total_min,
			COALESCE(MIN(time_minutes) FILTER (WHERE time_minutes > 0), 0) AS fastest,
			COALESCE(MAX(time_minutes), 0)                        AS slowest,
			COUNT(*) FILTER (WHERE false_positive)                AS false_positives
		FROM issues
		WHERE created_at::date = $1`

	var s ReportSummary
	err = m.DB.QueryRowContext(ctx, summaryQ, date).Scan(
		&s.TotalIssues, &s.AptIssues, &s.DeptIssues,
		&s.Resolved, &s.Pending,
		&s.AvgMinutes, &s.TotalMinutes, &s.FastestMinutes, &s.SlowestMinutes, &s.FalsePositives,
	)
	if err != nil {
		return nil, err
	}
	if s.TotalIssues > 0 {
		s.ResolutionRate = float64(s.Resolved) / float64(s.TotalIssues) * 100
	}
	report.Summary = s

	// ── Breakdown maps, over every issue on the day ──
	//
	// Kept out of the row query below on purpose. They used to be counted while
	// scanning the rows the report lists, which was fine when that was every
	// issue of the day -- and silently wrong the moment the lists stopped being
	// every issue of the day, because the charts would have described only the
	// rows that happened to be on screen. Aggregated separately, they describe the
	// day whatever is loaded.
	breakdownQ := `
		SELECT mode, type, status, COUNT(*)
		FROM issues
		WHERE created_at::date = $1
		GROUP BY mode, type, status`

	breakdownRows, err := m.DB.QueryContext(ctx, breakdownQ, date)
	if err != nil {
		return nil, err
	}
	for breakdownRows.Next() {
		var mode, issueType, status string
		var n int
		if err := breakdownRows.Scan(&mode, &issueType, &status, &n); err != nil {
			breakdownRows.Close()
			return nil, err
		}
		report.ByType[issueType] += n
		report.ByMode[mode] += n
		report.ByStatus[status] += n
	}
	if err := breakdownRows.Err(); err != nil {
		breakdownRows.Close()
		return nil, err
	}
	breakdownRows.Close()

	// ── The issue lists, one query per mode ──
	//
	// Split rather than one combined query with a single LIMIT: the two tables
	// are read independently, so one shared limit would fill the apartment table
	// to the brim and leave the department table showing nothing at all while
	// the server was holding dozens of rows it was told not to send.
	issueQ := `
		SELECT i.id, i.created_at, i.mode, i.location, i.type, i.problem,
		       i.resolution, i.time_minutes, i.start_time, i.end_time, i.reported_by_agent, i.confirmed_by_agent, i.status, i.logged_by,
		       u.name, u.avatar_idx, i.version, i.false_positive
		FROM issues i
		INNER JOIN users u ON i.logged_by = u.id
		WHERE i.created_at::date = $1 AND i.mode = $2
		ORDER BY i.created_at DESC, i.id DESC`

	// limit of zero or less means every row. That is what the printed report
	// asks for: a PDF is an archive of the day, so quietly capping it would
	// produce a document that looks complete and is not.
	if limit > 0 {
		issueQ += "\n\t\tLIMIT $3"
	}

	reportIssues := []*Issue{}
	// Empty rather than nil so the JSON is always an array: the report page reads
	// .length on both, and a null there takes the whole page down.
	report.AptIssues = []*Issue{}
	report.DeptIssues = []*Issue{}
	for _, mode := range []string{"apt", "dept"} {
		var rows *sql.Rows
		var err error
		if limit > 0 {
			rows, err = m.DB.QueryContext(ctx, issueQ, date, mode, limit)
		} else {
			rows, err = m.DB.QueryContext(ctx, issueQ, date, mode)
		}
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var i Issue
			if err := rows.Scan(
				&i.ID, &i.CreatedAt, &i.Mode, &i.Location, &i.Type,
				&i.Problem, &i.Resolution, &i.TimeMinutes, &i.StartTime, &i.EndTime, &i.ReportedByAgent, &i.ConfirmedByAgent, &i.Status,
				&i.LoggedBy, &i.LoggedByName, &i.LoggedByIdx, &i.Version, &i.FalsePositive,
			); err != nil {
				rows.Close()
				return nil, err
			}
			reportIssues = append(reportIssues, &i)
			if mode == "apt" {
				report.AptIssues = append(report.AptIssues, &i)
			} else {
				report.DeptIssues = append(report.DeptIssues, &i)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}

	if err = loadIssueChildren(ctx, m.DB, reportIssues); err != nil {
		return nil, err
	}

	// ── By technician ──
	// "Technician" here means anyone who does technical work, not the role:
	// a manager who logs an issue has done technical work and belongs in this
	// breakdown, which is also how the issue tables above them already list
	// them. Same inclusion rule as the Dashboard workload chart, so the two
	// screens agree.
	techQ := `
		SELECT u.id, u.name, u.avatar_idx,
		       COUNT(i.id)                                        AS total,
		       COUNT(i.id) FILTER (WHERE i.status = 'Ok')        AS resolved,
		       COALESCE(AVG(i.time_minutes), 0)                   AS avg_min
		FROM users u
		LEFT JOIN issues i ON i.logged_by = u.id AND i.created_at::date = $1
		GROUP BY u.id, u.name, u.avatar_idx, u.role
		HAVING u.role = 'technician' OR COUNT(i.id) > 0
		ORDER BY total DESC`

	techRows, err := m.DB.QueryContext(ctx, techQ, date)
	if err != nil {
		return nil, err
	}
	defer techRows.Close()

	for techRows.Next() {
		var ts TechStat
		var resolved int
		var avgMin float64
		if err := techRows.Scan(&ts.UserID, &ts.Name, &ts.AvatarIdx, &ts.Count, &resolved, &avgMin); err != nil {
			return nil, err
		}
		report.ByTechnician = append(report.ByTechnician, ts)
	}

	return report, techRows.Err()
}
