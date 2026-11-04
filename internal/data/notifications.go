package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/HalefS/lira/internal/validator"
)

// Notification centre: a derived feed, not a stored notification log.
//
// Every item is computed from the domain tables at read time. The only stored
// state is per-user read marks (notification_reads), which is what lets a
// badge say "unread" without a background job writing notification rows.
//
// The feed is deliberately about obligations, not history. "What happened
// today" is the digest's job; an inbox that mixed the two would bury the one
// overdue device under twenty issue-logged rows by mid-morning.

const (
	// DefaultNotificationLimit is how many items a page opens with. It matches
	// the frontend's PAGE_SIZE so "load more" grows one page at a time.
	DefaultNotificationLimit = 20
	// MaxNotificationLimit is a hard ceiling on one request. The feed is a
	// queue, not an archive; a reader who reaches this has more work than a
	// list can help with.
	MaxNotificationLimit = 100
	// FaultFeedDays bounds the one event-style kind -- a maintenance check that
	// found a fault and left no issue behind. Without a window it would
	// accumulate for ever.
	FaultFeedDays = 7
)

type NotificationKind string

const (
	KindMaintenanceDue   NotificationKind = "maintenance_due"
	KindMaintenanceFault NotificationKind = "maintenance_fault"
	KindRecurringAlert   NotificationKind = "recurring_alert"
	KindTVSwapAlert      NotificationKind = "tv_swap_alert"
	KindLowStock         NotificationKind = "low_stock"
	KindSLABreach        NotificationKind = "sla_breach"
)

func (k NotificationKind) Valid() bool {
	switch k {
	case KindMaintenanceDue, KindMaintenanceFault, KindRecurringAlert,
		KindTVSwapAlert, KindLowStock, KindSLABreach:
		return true
	}
	return false
}

type NotificationSeverity string

const (
	SeverityCritical NotificationSeverity = "critical"
	SeverityWarning  NotificationSeverity = "warning"
	SeverityInfo     NotificationSeverity = "info"
)

// NotificationRef is the identity of one feed item, and the body of a
// mark-as-read request. It is not a row id, because there is no notification
// row to point at.
type NotificationRef struct {
	Kind     NotificationKind `json:"kind"`
	SourceID int64            `json:"source_id"`
	// Variant participates in identity for exactly one kind: low_stock uses the
	// reorder level, because the same item falling low against a different
	// threshold is a different alert.
	Variant string `json:"variant,omitempty"`
}

func ValidateNotificationRef(v *validator.Validator, ref NotificationRef) {
	v.Check(ref.Kind.Valid(), "kind", "must be a known notification kind")
	v.Check(ref.SourceID > 0, "source_id", "must be a positive integer")

	if ref.Kind == KindLowStock {
		v.Check(ref.Variant != "", "variant", "must be provided")
		if ref.Variant != "" {
			n, err := strconv.Atoi(ref.Variant)
			v.Check(err == nil && n >= 0 && strconv.Itoa(n) == ref.Variant,
				"variant", "must be the reorder level as digits")
		}
	} else {
		v.Check(ref.Variant == "", "variant", "must be empty for this kind")
	}
}

// Notification is one feed item. Kind and Data are the contract; there is
// deliberately no title or message field, because the client owns the wording
// in both languages and a server-composed English sentence would leak one
// language into the other.
type Notification struct {
	Kind       NotificationKind     `json:"kind"`
	SourceID   int64                `json:"source_id"`
	Variant    string               `json:"variant,omitempty"`
	Severity   NotificationSeverity `json:"severity"`
	OccurredAt time.Time            `json:"occurred_at"`
	Unread     bool                 `json:"unread"`
	Data       map[string]any       `json:"data"`
}

type FeedParams struct {
	UserID    int64
	IsManager bool
	Now       time.Time
	// Today is local midnight, which is also the date the due-soon window and
	// the digest are measured from.
	Today time.Time
	// SLAMinutes is nil when no SLA is configured, in which case no issue can
	// breach one.
	SLAMinutes *int
	Limit      int
	// Kind filters the feed and never the badge. Empty means every kind.
	Kind       NotificationKind
	UnreadOnly bool
}

type NotificationModel struct {
	DB *sql.DB
}

// notificationFeedCTE is shared by Feed and UnreadCount so the badge can never
// count a set the list does not contain. $1 user, $2 manager, $3 now,
// $4 today, $5 due-soon days, $6 sla minutes (nullable), $7 fault-feed days.
//
// occurred_at is always the moment the condition became true, never "now
// because we looked". Capping a due-soon date at now would move it forward on
// every request, so an item marked read at 10:00 would be unread again at
// 10:00:30 and the badge would never clear.
const notificationFeedCTE = `
WITH feed AS (
    SELECT 'maintenance_due'::text AS kind, s.id AS source_id, ''::text AS variant,
           CASE WHEN s.next_due_on < $4::date THEN 'critical' ELSE 'warning' END AS severity,
           GREATEST(s.created_at, (s.next_due_on - $5::int)::timestamptz) AS occurred_at,
           jsonb_build_object(
               'schedule_id', s.id, 'device_name', s.device_name,
               'equipment_type', s.equipment_type, 'department_id', s.department_id,
               'due_on', s.next_due_on, 'days_until_due', s.next_due_on - $4::date,
               'assignee_id', s.assignee_id) AS data
    FROM maintenance_schedules s
    WHERE s.active AND s.next_due_on <= $4::date + $5::int
      AND ($2::boolean OR s.assignee_id IS NULL OR s.assignee_id = $1::bigint)

  UNION ALL
    SELECT 'maintenance_fault', c.id, '', 'info', c.created_at,
           jsonb_build_object(
               'check_id', c.id, 'schedule_id', c.schedule_id,
               'device_name', s.device_name, 'done_on', c.done_on,
               'issue_id', c.issue_id, 'note', c.note)
    FROM maintenance_checks c
    JOIN maintenance_schedules s ON s.id = c.schedule_id
    WHERE c.outcome = 'fault' AND c.issue_id IS NULL
      AND c.created_at >= $3::timestamptz - make_interval(days => $7::int)
      AND ($2::boolean OR s.assignee_id IS NULL OR s.assignee_id = $1::bigint)

  UNION ALL
    SELECT 'recurring_alert', ra.id, '', 'critical', ra.updated_at,
           jsonb_build_object(
               'alert_id', ra.id, 'mode', ra.mode, 'location', ra.location,
               'issue_type', ra.type, 'solution', ra.solution)
    FROM recurring_alerts ra
    WHERE ra.status = 'pending' AND $2::boolean

  UNION ALL
    SELECT 'tv_swap_alert', a.id, '', 'critical', a.updated_at,
           jsonb_build_object(
               'alert_id', a.id, 'swap_id', a.swap_id, 'issue_id', w.issue_id,
               'from_room', w.from_room, 'to_room', w.to_room, 'notes', w.notes)
    FROM tv_swap_alerts a
    JOIN issue_tv_swaps w ON w.id = a.swap_id
    WHERE a.status = 'pending' AND $2::boolean

  UNION ALL
    SELECT 'low_stock', i.id, i.reorder_level::text, 'warning',
           COALESCE((SELECT max(m.created_at)
                     FROM consumable_stock_movements m
                     WHERE m.item_id = i.id), i.created_at),
           jsonb_build_object(
               'item_id', i.id, 'item_name', i.name, 'stock', i.stock,
               'reorder_level', i.reorder_level, 'icon', i.icon)
    FROM consumable_items i
    WHERE i.reorder_level > 0 AND i.stock <= i.reorder_level AND $2::boolean

  UNION ALL
    SELECT 'sla_breach', i.id, '', 'critical',
           COALESCE(i.started_at, i.created_at) + make_interval(mins => $6::int),
           jsonb_build_object(
               'issue_id', i.id, 'sla_minutes', $6::int,
               'minutes_over', GREATEST(0, floor(extract(epoch FROM
                   ($3::timestamptz - (COALESCE(i.started_at, i.created_at)
                    + make_interval(mins => $6::int)))) / 60))::int,
               'logged_by', i.logged_by, 'mode', i.mode,
               'location', i.location, 'issue_type', i.type)
    FROM issues i
    WHERE i.status = 'Pending' AND $6::int IS NOT NULL
      AND COALESCE(i.started_at, i.created_at) + make_interval(mins => $6::int) <= $3::timestamptz
      AND ($2::boolean OR i.logged_by = $1::bigint)
),
marked AS (
    SELECT f.*,
           (COALESCE(GREATEST(r.read_at, g.read_at), '-infinity'::timestamptz) < f.occurred_at) AS unread
    FROM feed f
    LEFT JOIN notification_reads r
      ON r.user_id = $1 AND r.kind = f.kind
     AND r.source_id = f.source_id AND r.variant = f.variant
    LEFT JOIN notification_reads g
      ON g.user_id = $1 AND g.kind = '*' AND g.source_id = 0 AND g.variant = ''
)`

// notificationOrder is the feed's contract. Severity groups the queue, unread
// floats work the reader has not seen, work items sort oldest first (the thing
// that has waited longest is the thing at the top) and the single info kind
// sorts newest first. The identity tie-breaker keeps grow-the-limit pagination
// from reordering two items recorded in the same second.
const notificationOrder = `
ORDER BY
    CASE severity WHEN 'critical' THEN 0 WHEN 'warning' THEN 1 ELSE 2 END,
    unread DESC,
    CASE WHEN severity = 'info' THEN NULL ELSE occurred_at END ASC NULLS LAST,
    CASE WHEN severity = 'info' THEN occurred_at END DESC NULLS LAST,
    kind, source_id, variant`

// Feed returns at most p.Limit items in contract order and reports whether the
// server holds more beyond them.
func (m NotificationModel) Feed(p FeedParams) ([]*Notification, bool, error) {
	limit := ClampNotificationLimit(p.Limit)
	sla := slaArg(p.SLAMinutes)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := notificationFeedCTE + `
SELECT kind, source_id, variant, severity, occurred_at, unread, data
FROM marked
WHERE ($8::text = '' OR kind = $8::text)
  AND (NOT $9::boolean OR unread)` + notificationOrder + `
LIMIT $10::int`

	rows, err := m.DB.QueryContext(ctx, query,
		p.UserID, p.IsManager, p.Now, p.Today.Format(time.DateOnly),
		DueSoonDays, sla, FaultFeedDays,
		string(p.Kind), p.UnreadOnly, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	items := []*Notification{}
	for rows.Next() {
		var (
			n       Notification
			rawData []byte
		)
		if err := rows.Scan(&n.Kind, &n.SourceID, &n.Variant, &n.Severity,
			&n.OccurredAt, &n.Unread, &rawData); err != nil {
			return nil, false, err
		}
		if err := json.Unmarshal(rawData, &n.Data); err != nil {
			return nil, false, fmt.Errorf("decode notification data: %w", err)
		}
		items = append(items, &n)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore, nil
}

// UnreadCount is over the caller's whole visible window: no kind filter, no
// unread filter and no limit. It is the sidebar badge, not the page length.
func (m NotificationModel) UnreadCount(userID int64, isManager bool, slaMinutes *int, now, today time.Time) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var count int
	err := m.DB.QueryRowContext(ctx, notificationFeedCTE+`
SELECT count(*) FROM marked WHERE unread`,
		userID, isManager, now, today.Format(time.DateOnly),
		DueSoonDays, slaArg(slaMinutes), FaultFeedDays).Scan(&count)
	return count, err
}

// MarkRead records that this user has seen one derived item. A vanished item is
// not an error: between the feed request and this call the condition can clear,
// and the write is harmless because every kind's occurred_at moves forward when
// the condition recurs.
func (m NotificationModel) MarkRead(userID int64, ref NotificationRef, now time.Time) (time.Time, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var readAt time.Time
	err := m.DB.QueryRowContext(ctx, `
		INSERT INTO notification_reads (user_id, kind, source_id, variant, read_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, kind, source_id, variant)
		DO UPDATE SET read_at = GREATEST(notification_reads.read_at, EXCLUDED.read_at)
		RETURNING read_at`,
		userID, string(ref.Kind), ref.SourceID, ref.Variant, now).Scan(&readAt)
	if err != nil {
		return time.Time{}, err
	}
	return readAt, nil
}

// MarkAllRead writes the sentinel row: everything that existed at now is read.
// It is one row rather than one per item, and anything that occurs later stays
// unread because its occurred_at is after this timestamp.
func (m NotificationModel) MarkAllRead(userID int64, now time.Time) (time.Time, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var readAt time.Time
	err := m.DB.QueryRowContext(ctx, `
		INSERT INTO notification_reads (user_id, kind, source_id, variant, read_at)
		VALUES ($1, '*', 0, '', $2)
		ON CONFLICT (user_id, kind, source_id, variant)
		DO UPDATE SET read_at = GREATEST(notification_reads.read_at, EXCLUDED.read_at)
		RETURNING read_at`,
		userID, now).Scan(&readAt)
	if err != nil {
		return time.Time{}, err
	}
	return readAt, nil
}

// ClampNotificationLimit keeps a missing or nonsensical limit at the default
// and refuses to let one request ask for the entire queue.
func ClampNotificationLimit(limit int) int {
	if limit <= 0 {
		return DefaultNotificationLimit
	}
	if limit > MaxNotificationLimit {
		return MaxNotificationLimit
	}
	return limit
}

// slaArg sends SQL NULL when no SLA is configured. A typed nil pointer is not a
// driver value, so the distinction has to be made here.
func slaArg(minutes *int) any {
	if minutes == nil {
		return nil
	}
	return *minutes
}
