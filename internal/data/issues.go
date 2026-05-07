package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/HalefS/lira/internal/validator"
)

type Issue struct {
	ID           int64     `json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	Mode         string    `json:"mode"`
	Location     string    `json:"location"`
	Type         string    `json:"type"`
	Problem      string    `json:"problem"`
	Resolution   string    `json:"resolution"`
	TimeMinutes  int       `json:"time_minutes"`
	Status       string    `json:"status"`
	LoggedBy     int64     `json:"logged_by"`
	LoggedByName string    `json:"logged_by_name"`
	LoggedByIdx  int       `json:"logged_by_idx"`
	Version      int       `json:"-"`
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

type IssueModel struct {
	DB *sql.DB
}

func (m IssueModel) Insert(issue *Issue) error {
	query := `
		INSERT INTO issues (mode, location, type, problem, resolution, time_minutes, status, logged_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, version`

	args := []any{
		issue.Mode, issue.Location, issue.Type, issue.Problem,
		issue.Resolution, issue.TimeMinutes, issue.Status, issue.LoggedBy,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	return m.DB.QueryRowContext(ctx, query, args...).Scan(&issue.ID, &issue.CreatedAt, &issue.Version)
}

func (m IssueModel) Get(id int64) (*Issue, error) {
	query := `
		SELECT i.id, i.created_at, i.mode, i.location, i.type, i.problem,
		       i.resolution, i.time_minutes, i.status, i.logged_by,
		       u.name, u.avatar_idx, i.version
		FROM issues i
		INNER JOIN users u ON i.logged_by = u.id
		WHERE i.id = $1`

	var issue Issue
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query, id).Scan(
		&issue.ID, &issue.CreatedAt, &issue.Mode, &issue.Location, &issue.Type,
		&issue.Problem, &issue.Resolution, &issue.TimeMinutes, &issue.Status,
		&issue.LoggedBy, &issue.LoggedByName, &issue.LoggedByIdx, &issue.Version,
	)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil, ErrRecordNotFound
		default:
			return nil, err
		}
	}
	return &issue, nil
}

func (m IssueModel) GetAll(f IssueFilters) ([]*Issue, error) {
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
		conditions = append(conditions, fmt.Sprintf("i.created_at::date = $%d", argIdx))
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

	query := fmt.Sprintf(`
		SELECT i.id, i.created_at, i.mode, i.location, i.type, i.problem,
		       i.resolution, i.time_minutes, i.status, i.logged_by,
		       u.name, u.avatar_idx, i.version
		FROM issues i
		INNER JOIN users u ON i.logged_by = u.id
		WHERE %s
		ORDER BY i.created_at DESC`, strings.Join(conditions, " AND "))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var issues []*Issue
	for rows.Next() {
		var issue Issue
		err := rows.Scan(
			&issue.ID, &issue.CreatedAt, &issue.Mode, &issue.Location, &issue.Type,
			&issue.Problem, &issue.Resolution, &issue.TimeMinutes, &issue.Status,
			&issue.LoggedBy, &issue.LoggedByName, &issue.LoggedByIdx, &issue.Version,
		)
		if err != nil {
			return nil, err
		}
		issues = append(issues, &issue)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return issues, nil
}

func (m IssueModel) Update(issue *Issue) error {
	query := `
		UPDATE issues
		SET mode=$1, location=$2, type=$3, problem=$4, resolution=$5,
		    time_minutes=$6, status=$7, version=version+1
		WHERE id=$8 AND version=$9
		RETURNING version`

	args := []any{
		issue.Mode, issue.Location, issue.Type, issue.Problem,
		issue.Resolution, issue.TimeMinutes, issue.Status,
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

func (m IssueModel) Delete(id int64) error {
	query := `DELETE FROM issues WHERE id = $1`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := m.DB.ExecContext(ctx, query, id)
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
	return nil
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

	// By technician
	techQuery := `
		SELECT u.id, u.name, u.avatar_idx, COUNT(i.id) AS cnt
		FROM users u
		LEFT JOIN issues i ON i.logged_by = u.id AND i.created_at::date = $1
		WHERE u.role = 'technician'
		GROUP BY u.id, u.name, u.avatar_idx
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
		       i.resolution, i.time_minutes, i.status, i.logged_by,
		       u.name, u.avatar_idx, i.version
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
			&issue.Problem, &issue.Resolution, &issue.TimeMinutes, &issue.Status,
			&issue.LoggedBy, &issue.LoggedByName, &issue.LoggedByIdx, &issue.Version,
		)
		if err != nil {
			return nil, err
		}
		issues = append(issues, &issue)
	}
	return issues, rows.Err()
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
}

func (m IssueModel) GetDailyReport(date string) (*DailyReport, error) {
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
	}

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
			COALESCE(MAX(time_minutes), 0)                        AS slowest
		FROM issues
		WHERE created_at::date = $1`

	var s ReportSummary
	err := m.DB.QueryRowContext(ctx, summaryQ, date).Scan(
		&s.TotalIssues, &s.AptIssues, &s.DeptIssues,
		&s.Resolved, &s.Pending,
		&s.AvgMinutes, &s.TotalMinutes, &s.FastestMinutes, &s.SlowestMinutes,
	)
	if err != nil {
		return nil, err
	}
	if s.TotalIssues > 0 {
		s.ResolutionRate = float64(s.Resolved) / float64(s.TotalIssues) * 100
	}
	report.Summary = s

	// ── All issues (apt + dept) ──
	issueQ := `
		SELECT i.id, i.created_at, i.mode, i.location, i.type, i.problem,
		       i.resolution, i.time_minutes, i.status, i.logged_by,
		       u.name, u.avatar_idx, i.version
		FROM issues i
		INNER JOIN users u ON i.logged_by = u.id
		WHERE i.created_at::date = $1
		ORDER BY i.mode, i.created_at`

	rows, err := m.DB.QueryContext(ctx, issueQ, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var i Issue
		if err := rows.Scan(
			&i.ID, &i.CreatedAt, &i.Mode, &i.Location, &i.Type,
			&i.Problem, &i.Resolution, &i.TimeMinutes, &i.Status,
			&i.LoggedBy, &i.LoggedByName, &i.LoggedByIdx, &i.Version,
		); err != nil {
			return nil, err
		}
		if i.Mode == "apt" {
			report.AptIssues = append(report.AptIssues, &i)
		} else {
			report.DeptIssues = append(report.DeptIssues, &i)
		}
		report.ByType[i.Type]++
		report.ByMode[i.Mode]++
		report.ByStatus[i.Status]++
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	// ── By technician ──
	techQ := `
		SELECT u.id, u.name, u.avatar_idx,
		       COUNT(i.id)                                        AS total,
		       COUNT(i.id) FILTER (WHERE i.status = 'Ok')        AS resolved,
		       COALESCE(AVG(i.time_minutes), 0)                   AS avg_min
		FROM users u
		LEFT JOIN issues i ON i.logged_by = u.id AND i.created_at::date = $1
		WHERE u.role = 'technician'
		GROUP BY u.id, u.name, u.avatar_idx
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
