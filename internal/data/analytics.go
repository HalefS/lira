package data

import (
	"context"
	"database/sql"
	"math"
	"sort"
	"time"
)

type AnalyticsMetric struct {
	Current  float64 `json:"current"`
	Previous float64 `json:"previous"`
}

type TechAnalytics struct {
	UserID         int64   `json:"user_id"`
	Name           string  `json:"name"`
	IssuesLogged   int     `json:"issues_logged"`
	Resolved       int     `json:"resolved"`
	AvgTimeMinutes float64 `json:"avg_time_minutes"`
}

type TypeAnalytics struct {
	Type     string `json:"type"`
	Current  int    `json:"current"`
	Previous int    `json:"previous"`
	// FalsePositives is how many of this period's issues of this type were
	// false alarms.
	FalsePositives int `json:"false_positives"`
}

type Analytics struct {
	Range                 string           `json:"range"`
	CurrentStart          time.Time        `json:"current_start"`
	CurrentEnd            time.Time        `json:"current_end"`
	PreviousStart         time.Time        `json:"previous_start"`
	PreviousEnd           time.Time        `json:"previous_end"`
	TotalIssues           AnalyticsMetric  `json:"total_issues"`
	Resolved              AnalyticsMetric  `json:"resolved"`
	Pending               AnalyticsMetric  `json:"pending"`
	ResolutionRate        AnalyticsMetric  `json:"resolution_rate"`
	AvgTimeMinutes        AnalyticsMetric  `json:"avg_time_minutes"`
	FastestTimeMinutes    AnalyticsMetric  `json:"fastest_time_minutes"`
	RecurringAlertsOpened AnalyticsMetric  `json:"recurring_alerts_opened"`
	FalsePositives        AnalyticsMetric  `json:"false_positives"`
	FalsePositiveRate     AnalyticsMetric  `json:"false_positive_rate"`
	ByType                []*TypeAnalytics `json:"by_type"`
	ByTechnician          []*TechAnalytics `json:"by_technician"`
	BestTechnicianID      *int64           `json:"best_technician_id"`
}

type AnalyticsModel struct {
	DB *sql.DB
}

type periodSummary struct {
	total, resolved, pending int
	falsePositives           int
	avgTime, fastestTime     float64
}

func (m AnalyticsModel) summarize(ctx context.Context, start, end time.Time) (periodSummary, error) {
	query := `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'Ok'),
			COUNT(*) FILTER (WHERE status = 'Pending'),
			COALESCE(AVG(time_minutes) FILTER (WHERE status = 'Ok'), 0),
			COALESCE(MIN(time_minutes) FILTER (WHERE status = 'Ok'), 0),
			COUNT(*) FILTER (WHERE false_positive)
		FROM issues
		WHERE created_at >= $1 AND created_at < $2`

	var s periodSummary
	err := m.DB.QueryRowContext(ctx, query, start, end).Scan(
		&s.total, &s.resolved, &s.pending, &s.avgTime, &s.fastestTime, &s.falsePositives,
	)
	return s, err
}

func (m AnalyticsModel) countRecurringAlertsOpened(ctx context.Context, start, end time.Time) (int, error) {
	var count int
	err := m.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM recurring_alerts WHERE created_at >= $1 AND created_at < $2`,
		start, end).Scan(&count)
	return count, err
}

func (m AnalyticsModel) typeBreakdown(ctx context.Context, start, end time.Time) (map[string]int, error) {
	rows, err := m.DB.QueryContext(ctx, `
		SELECT type, COUNT(*) FROM issues
		WHERE created_at >= $1 AND created_at < $2
		GROUP BY type`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]int)
	for rows.Next() {
		var t string
		var c int
		if err := rows.Scan(&t, &c); err != nil {
			return nil, err
		}
		out[t] = c
	}
	return out, rows.Err()
}

// falsePositivesByType counts the false alarms in the period, per issue type.
func (m AnalyticsModel) falsePositivesByType(ctx context.Context, start, end time.Time) (map[string]int, error) {
	rows, err := m.DB.QueryContext(ctx, `
		SELECT type, COUNT(*) FROM issues
		WHERE created_at >= $1 AND created_at < $2 AND false_positive
		GROUP BY type`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]int)
	for rows.Next() {
		var t string
		var c int
		if err := rows.Scan(&t, &c); err != nil {
			return nil, err
		}
		out[t] = c
	}
	return out, rows.Err()
}

func (m AnalyticsModel) technicianBreakdown(ctx context.Context, start, end time.Time) ([]*TechAnalytics, error) {
	rows, err := m.DB.QueryContext(ctx, `
		SELECT u.id, u.name,
		       COUNT(i.id),
		       COUNT(i.id) FILTER (WHERE i.status = 'Ok'),
		       COALESCE(AVG(i.time_minutes) FILTER (WHERE i.status = 'Ok'), 0)
		FROM issues i
		INNER JOIN users u ON i.logged_by = u.id
		WHERE i.created_at >= $1 AND i.created_at < $2
		GROUP BY u.id, u.name
		ORDER BY COUNT(i.id) DESC`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*TechAnalytics
	for rows.Next() {
		var t TechAnalytics
		if err := rows.Scan(&t.UserID, &t.Name, &t.IssuesLogged, &t.Resolved, &t.AvgTimeMinutes); err != nil {
			return nil, err
		}
		t.AvgTimeMinutes = round1(t.AvgTimeMinutes)
		out = append(out, &t)
	}
	return out, rows.Err()
}

func round1(f float64) float64 {
	return math.Round(f*10) / 10
}

// Get returns a current-vs-previous analytics comparison for the given
// range ("week" or "month"), using rolling windows ending now — e.g.
// "week" compares the last 7 days against the 7 days before that, rather
// than calendar weeks, to avoid partial-period distortion right after a
// week or month boundary.
func (m AnalyticsModel) Get(rangeStr string) (*Analytics, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	now := time.Now()
	var span time.Duration
	if rangeStr == "month" {
		span = 30 * 24 * time.Hour
	} else {
		rangeStr = "week"
		span = 7 * 24 * time.Hour
	}

	curStart, curEnd := now.Add(-span), now
	prevStart, prevEnd := now.Add(-2*span), now.Add(-span)

	cur, err := m.summarize(ctx, curStart, curEnd)
	if err != nil {
		return nil, err
	}
	prev, err := m.summarize(ctx, prevStart, prevEnd)
	if err != nil {
		return nil, err
	}

	curAlerts, err := m.countRecurringAlertsOpened(ctx, curStart, curEnd)
	if err != nil {
		return nil, err
	}
	prevAlerts, err := m.countRecurringAlertsOpened(ctx, prevStart, prevEnd)
	if err != nil {
		return nil, err
	}

	curTypes, err := m.typeBreakdown(ctx, curStart, curEnd)
	if err != nil {
		return nil, err
	}
	prevTypes, err := m.typeBreakdown(ctx, prevStart, prevEnd)
	if err != nil {
		return nil, err
	}

	curFalsePositives, err := m.falsePositivesByType(ctx, curStart, curEnd)
	if err != nil {
		return nil, err
	}

	typeNames := make(map[string]bool)
	for t := range curTypes {
		typeNames[t] = true
	}
	for t := range prevTypes {
		typeNames[t] = true
	}
	byType := make([]*TypeAnalytics, 0, len(typeNames))
	for t := range typeNames {
		byType = append(byType, &TypeAnalytics{Type: t, Current: curTypes[t], Previous: prevTypes[t], FalsePositives: curFalsePositives[t]})
	}
	sort.Slice(byType, func(i, j int) bool {
		if byType[i].Current != byType[j].Current {
			return byType[i].Current > byType[j].Current
		}
		return byType[i].Type < byType[j].Type
	})

	byTech, err := m.technicianBreakdown(ctx, curStart, curEnd)
	if err != nil {
		return nil, err
	}

	var bestID *int64
	bestAvg := math.MaxFloat64
	for _, t := range byTech {
		if t.Resolved > 0 && t.AvgTimeMinutes < bestAvg {
			bestAvg = t.AvgTimeMinutes
			id := t.UserID
			bestID = &id
		}
	}

	resRateCur, resRatePrev := 0.0, 0.0
	if cur.total > 0 {
		resRateCur = round1(float64(cur.resolved) / float64(cur.total) * 100)
	}
	if prev.total > 0 {
		resRatePrev = round1(float64(prev.resolved) / float64(prev.total) * 100)
	}

	fpRateCur, fpRatePrev := 0.0, 0.0
	if cur.total > 0 {
		fpRateCur = round1(float64(cur.falsePositives) / float64(cur.total) * 100)
	}
	if prev.total > 0 {
		fpRatePrev = round1(float64(prev.falsePositives) / float64(prev.total) * 100)
	}

	return &Analytics{
		Range:                 rangeStr,
		CurrentStart:          curStart,
		CurrentEnd:            curEnd,
		PreviousStart:         prevStart,
		PreviousEnd:           prevEnd,
		TotalIssues:           AnalyticsMetric{float64(cur.total), float64(prev.total)},
		Resolved:              AnalyticsMetric{float64(cur.resolved), float64(prev.resolved)},
		Pending:               AnalyticsMetric{float64(cur.pending), float64(prev.pending)},
		ResolutionRate:        AnalyticsMetric{resRateCur, resRatePrev},
		AvgTimeMinutes:        AnalyticsMetric{round1(cur.avgTime), round1(prev.avgTime)},
		FastestTimeMinutes:    AnalyticsMetric{cur.fastestTime, prev.fastestTime},
		RecurringAlertsOpened: AnalyticsMetric{float64(curAlerts), float64(prevAlerts)},
		FalsePositives:        AnalyticsMetric{float64(cur.falsePositives), float64(prev.falsePositives)},
		FalsePositiveRate:     AnalyticsMetric{fpRateCur, fpRatePrev},
		ByType:                byType,
		ByTechnician:          byTech,
		BestTechnicianID:      bestID,
	}, nil
}
