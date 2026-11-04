package data

import (
	"context"
	"time"
)

// The daily digest is a snapshot computed on request, not a message that was
// delivered. There is no cron, no outbox and no "digest sent at 08:00" fact
// anywhere in the system. The endpoint exists so a page can open with "here is
// today" without the client firing a dozen report queries.

type DigestWindow struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type DigestIssues struct {
	Logged        int `json:"logged"`
	Resolved      int `json:"resolved"`
	FalsePositive int `json:"false_positive"`
	// Open is a state, not a window: how much work is still outstanding now.
	Open int `json:"open"`
}

type DigestMaintenance struct {
	ChecksLogged int `json:"checks_logged"`
	FaultsFound  int `json:"faults_found"`
	DueToday     int `json:"due_today"`
	Overdue      int `json:"overdue"`
}

type DigestTVSwaps struct {
	Opened int `json:"opened"`
	Solved int `json:"solved"`
}

type DigestSupport struct {
	Logged int `json:"logged"`
	Solved int `json:"solved"`
}

type DigestStock struct {
	Added int `json:"added"`
	Set   int `json:"set"`
	Used  int `json:"used"`
	Low   int `json:"low"`
}

type Digest struct {
	Date   string       `json:"date"`
	Window DigestWindow `json:"window"`
	// Scope is always "team". The issues table has no resolved_by column, so a
	// personal "resolved" count would be a fiction; the one personal number is
	// UnreadCount, and it is named for what it is.
	Scope       string            `json:"scope"`
	Issues      DigestIssues      `json:"issues"`
	Maintenance DigestMaintenance `json:"maintenance"`
	TVSwaps     DigestTVSwaps     `json:"tv_swaps"`
	Support     DigestSupport     `json:"support"`
	Stock       DigestStock       `json:"stock"`
	UnreadCount int               `json:"unread_count"`
	// HasActivity counts events only, never outstanding state. Including open
	// work would make it permanently true and the flag meaningless.
	HasActivity bool `json:"has_activity"`
}

// DigestWindowFor returns [local midnight, now). Local means the server's
// time.Local: there is no per-user timezone column, so inventing one here would
// disagree with every other date in the app. time.Date handles a DST day whose
// midnight-to-now span is 23 or 25 hours.
func DigestWindowFor(now time.Time) (from, to time.Time) {
	y, m, d := now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, now.Location()), now
}

func (m NotificationModel) Digest(userID int64, isManager bool, slaMinutes *int, now time.Time) (*Digest, error) {
	from, to := DigestWindowFor(now)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `
SELECT
    (SELECT count(*) FROM issues WHERE created_at >= $1 AND created_at < $2),
    (SELECT count(*) FROM issues WHERE resolved_at >= $1 AND resolved_at < $2),
    (SELECT count(*) FROM issues WHERE false_positive AND created_at >= $1 AND created_at < $2),
    (SELECT count(*) FROM issues WHERE status = 'Pending'),
    (SELECT count(*) FROM maintenance_checks WHERE created_at >= $1 AND created_at < $2),
    (SELECT count(*) FROM maintenance_checks WHERE outcome = 'fault' AND created_at >= $1 AND created_at < $2),
    (SELECT count(*) FROM maintenance_schedules WHERE active AND next_due_on = $3::date),
    (SELECT count(*) FROM maintenance_schedules WHERE active AND next_due_on < $3::date),
    (SELECT count(*) FROM issue_tv_swaps WHERE created_at >= $1 AND created_at < $2),
    (SELECT count(*) FROM tv_swap_alerts WHERE solved_at >= $1 AND solved_at < $2),
    (SELECT count(*) FROM issue_support_requests WHERE created_at >= $1 AND created_at < $2),
    (SELECT count(*) FROM issue_support_requests WHERE resolved_at >= $1 AND resolved_at < $2),
    (SELECT count(*) FROM consumable_stock_movements WHERE reason = 'added' AND created_at >= $1 AND created_at < $2),
    (SELECT count(*) FROM consumable_stock_movements WHERE reason = 'set'   AND created_at >= $1 AND created_at < $2),
    (SELECT count(*) FROM consumable_stock_movements WHERE reason = 'used'  AND created_at >= $1 AND created_at < $2),
    (SELECT count(*) FROM consumable_items WHERE reorder_level > 0 AND stock <= reorder_level)`

	var d Digest
	err := m.DB.QueryRowContext(ctx, query, from, to, from.Format(time.DateOnly)).Scan(
		&d.Issues.Logged, &d.Issues.Resolved, &d.Issues.FalsePositive, &d.Issues.Open,
		&d.Maintenance.ChecksLogged, &d.Maintenance.FaultsFound,
		&d.Maintenance.DueToday, &d.Maintenance.Overdue,
		&d.TVSwaps.Opened, &d.TVSwaps.Solved,
		&d.Support.Logged, &d.Support.Solved,
		&d.Stock.Added, &d.Stock.Set, &d.Stock.Used, &d.Stock.Low,
	)
	if err != nil {
		return nil, err
	}

	d.Date = now.Format(time.DateOnly)
	d.Window = DigestWindow{From: from, To: to}
	d.Scope = "team"

	// Only events count as activity. The outstanding numbers sit beside this
	// flag precisely so a quiet day can still report open work honestly.
	d.HasActivity = DigestHasActivity(&d)

	d.UnreadCount, err = m.UnreadCount(userID, isManager, slaMinutes, now, from)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// DigestHasActivity is exported for the unit test that stops outstanding work
// from quietly turning the flag into decoration.
func DigestHasActivity(d *Digest) bool {
	return d.Issues.Logged+d.Issues.Resolved+d.Issues.FalsePositive+
		d.Maintenance.ChecksLogged+d.Maintenance.FaultsFound+
		d.TVSwaps.Opened+d.TVSwaps.Solved+
		d.Support.Logged+d.Support.Solved+
		d.Stock.Added+d.Stock.Set+d.Stock.Used > 0
}
