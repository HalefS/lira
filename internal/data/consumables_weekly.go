package data

import (
	"context"
	"database/sql"
	"time"
)

// WeekRange returns the half-open window [start, end) covering the
// Monday-to-Sunday week that date falls in.
//
// end is the following Monday rather than the following Sunday, so a caller
// can pass it straight to a query as an exclusive bound: a row logged exactly
// at midnight on the next Monday belongs to the next week, and cannot be
// counted in two of them.
func WeekRange(date time.Time) (start, end time.Time) {
	d := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	// time.Weekday has Sunday = 0, so shift it to make Monday the first day.
	offset := (int(d.Weekday()) + 6) % 7
	start = d.AddDate(0, 0, -offset)
	return start, start.AddDate(0, 0, 7)
}

// ConsumablesReportEntry is one recorded use of a consumable, carrying the
// context needed to answer "what was used, where, and on what".
//
// The item name is the snapshot stored on the row (so it survives the catalog
// entry being deleted or renamed), and the location and issue details are read
// from the issue rather than copied, so they can never go stale.
type ConsumablesReportEntry struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Item      string    `json:"item"`
	Icon      string    `json:"icon"`
	Quantity  int       `json:"quantity"`

	// Mode and Location come from the issue the consumable was used on.
	Mode         string `json:"mode"`
	Location     string `json:"location"`
	IssueID      int64  `json:"issue_id"`
	IssueType    string `json:"issue_type"`
	IssueStatus  string `json:"issue_status"`
	IssueProblem string `json:"issue_problem"`

	// LoggedBy is nullable: a user removed from the database leaves the
	// consumable history behind (the foreign key is ON DELETE SET NULL), and
	// that history must still appear in the report.
	LoggedBy     *int64 `json:"logged_by"`
	LoggedByName string `json:"logged_by_name"`
	LoggedByIdx  int    `json:"logged_by_idx"`
}

// ConsumableItemTotal is the quantity of one item used across the whole period.
type ConsumableItemTotal struct {
	Item     string `json:"item"`
	Icon     string `json:"icon"`
	Quantity int    `json:"quantity"`
	Records  int    `json:"records"`
}

// ConsumableLocationTotal is the quantity used at one place across the period.
type ConsumableLocationTotal struct {
	Mode     string `json:"mode"`
	Location string `json:"location"`
	Quantity int    `json:"quantity"`
	Records  int    `json:"records"`
}

// ConsumableTechTotal is the quantity one technician used across the period.
type ConsumableTechTotal struct {
	UserID    *int64 `json:"user_id"`
	Name      string `json:"name"`
	AvatarIdx int    `json:"avatar_idx"`
	Quantity  int    `json:"quantity"`
	Records   int    `json:"records"`
}

// ConsumableDayTotal is the quantity used on one day of the period, so a
// reader can see at a glance which day the stock went.
type ConsumableDayTotal struct {
	Date     string `json:"date"`
	Quantity int    `json:"quantity"`
	Records  int    `json:"records"`
}

// ConsumablesWeeklyReport is everything the weekly consumables report shows:
// the detail rows, and four roll-ups of the same numbers.
type ConsumablesWeeklyReport struct {
	// From is inclusive and To is exclusive, matching WeekRange.
	From        time.Time `json:"from"`
	To          time.Time `json:"to"`
	FromDate    string    `json:"from_date"`
	ToDate      string    `json:"to_date"`
	GeneratedAt time.Time `json:"generated_at"`

	TotalRecords   int `json:"total_records"`
	TotalQuantity  int `json:"total_quantity"`
	DistinctItems  int `json:"distinct_items"`
	LocationsCount int `json:"locations_count"`

	ByItem       []*ConsumableItemTotal     `json:"by_item"`
	ByLocation   []*ConsumableLocationTotal `json:"by_location"`
	ByTechnician []*ConsumableTechTotal     `json:"by_technician"`
	ByDay        []*ConsumableDayTotal      `json:"by_day"`
	Entries      []*ConsumablesReportEntry  `json:"entries"`
}

// GetWeeklyConsumablesReport builds the report for the week containing anchor.
// Pass a zero anchor to report on the current week.
func (m ConsumableModel) GetWeeklyConsumablesReport(anchor time.Time) (*ConsumablesWeeklyReport, error) {
	if anchor.IsZero() {
		anchor = time.Now()
	}
	start, end := WeekRange(anchor)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	report := &ConsumablesWeeklyReport{
		From:         start,
		To:           end,
		FromDate:     start.Format("2006-01-02"),
		ToDate:       end.AddDate(0, 0, -1).Format("2006-01-02"),
		GeneratedAt:  time.Now(),
		ByItem:       []*ConsumableItemTotal{},
		ByLocation:   []*ConsumableLocationTotal{},
		ByTechnician: []*ConsumableTechTotal{},
		ByDay:        []*ConsumableDayTotal{},
		Entries:      []*ConsumablesReportEntry{},
	}

	// ── Detail rows ──
	// LEFT JOIN on users on purpose: logged_by is ON DELETE SET NULL, so an
	// inner join would silently drop a consumable from the report the moment
	// the technician who used it was removed from the database.
	entryQ := `
		SELECT c.id, c.created_at, c.item, COALESCE(ci.icon, 'box'), c.quantity,
		       i.mode, i.location, i.id, i.type, i.status, i.problem,
		       c.logged_by, u.name, COALESCE(u.avatar_idx, 0)
		FROM consumables c
		INNER JOIN issues i ON i.id = c.issue_id
		LEFT JOIN consumable_items ci ON ci.id = c.item_id
		LEFT JOIN users u ON u.id = c.logged_by
		WHERE c.created_at >= $1 AND c.created_at < $2
		ORDER BY i.mode, LOWER(TRIM(i.location)), c.item, c.created_at`

	rows, err := m.DB.QueryContext(ctx, entryQ, start, end)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e ConsumablesReportEntry
		var loggedBy sql.NullInt64
		var name sql.NullString
		if err := rows.Scan(
			&e.ID, &e.CreatedAt, &e.Item, &e.Icon, &e.Quantity,
			&e.Mode, &e.Location, &e.IssueID, &e.IssueType, &e.IssueStatus, &e.IssueProblem,
			&loggedBy, &name, &e.LoggedByIdx,
		); err != nil {
			rows.Close()
			return nil, err
		}
		if loggedBy.Valid {
			id := loggedBy.Int64
			e.LoggedBy = &id
		}
		if name.Valid {
			e.LoggedByName = name.String
		} else {
			// Keep the row, but say plainly that the author is unknown
			// rather than showing a blank cell that reads as a mistake.
			e.LoggedByName = "Unknown"
		}
		report.Entries = append(report.Entries, &e)
		report.TotalRecords++
		report.TotalQuantity += e.Quantity
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	if report.TotalRecords == 0 {
		// Nothing was used this week; the roll-ups would all be empty anyway.
		return report, nil
	}

	// ── By item ──
	itemQ := `
		SELECT c.item, COALESCE(MAX(ci.icon), 'box'), COALESCE(SUM(c.quantity), 0), COUNT(*)
		FROM consumables c
		LEFT JOIN consumable_items ci ON ci.id = c.item_id
		WHERE c.created_at >= $1 AND c.created_at < $2
		GROUP BY c.item
		ORDER BY SUM(c.quantity) DESC, c.item`
	itemRows, err := m.DB.QueryContext(ctx, itemQ, start, end)
	if err != nil {
		return nil, err
	}
	for itemRows.Next() {
		var t ConsumableItemTotal
		if err := itemRows.Scan(&t.Item, &t.Icon, &t.Quantity, &t.Records); err != nil {
			itemRows.Close()
			return nil, err
		}
		report.ByItem = append(report.ByItem, &t)
	}
	if err := itemRows.Err(); err != nil {
		itemRows.Close()
		return nil, err
	}
	itemRows.Close()
	report.DistinctItems = len(report.ByItem)

	// ── By location ──
	// Grouped case- and whitespace-insensitively, the same way the recurring
	// alert detection compares locations, so "Kitchen" and "kitchen " are one
	// place rather than two. MAX keeps a consistent capitalisation to show.
	locQ := `
		SELECT i.mode, MAX(i.location), COALESCE(SUM(c.quantity), 0), COUNT(*)
		FROM consumables c
		INNER JOIN issues i ON i.id = c.issue_id
		WHERE c.created_at >= $1 AND c.created_at < $2
		GROUP BY i.mode, LOWER(TRIM(i.location))
		ORDER BY SUM(c.quantity) DESC, LOWER(TRIM(i.location))`
	locRows, err := m.DB.QueryContext(ctx, locQ, start, end)
	if err != nil {
		return nil, err
	}
	for locRows.Next() {
		var t ConsumableLocationTotal
		if err := locRows.Scan(&t.Mode, &t.Location, &t.Quantity, &t.Records); err != nil {
			locRows.Close()
			return nil, err
		}
		report.ByLocation = append(report.ByLocation, &t)
	}
	if err := locRows.Err(); err != nil {
		locRows.Close()
		return nil, err
	}
	locRows.Close()
	report.LocationsCount = len(report.ByLocation)

	// ── By technician ──
	techQ := `
		SELECT u.id, COALESCE(u.name, 'Unknown'), COALESCE(u.avatar_idx, 0),
		       COALESCE(SUM(c.quantity), 0), COUNT(*)
		FROM consumables c
		LEFT JOIN users u ON u.id = c.logged_by
		WHERE c.created_at >= $1 AND c.created_at < $2
		GROUP BY u.id, u.name, u.avatar_idx
		ORDER BY SUM(c.quantity) DESC, COALESCE(u.name, 'Unknown')`
	techRows, err := m.DB.QueryContext(ctx, techQ, start, end)
	if err != nil {
		return nil, err
	}
	for techRows.Next() {
		var t ConsumableTechTotal
		var uid sql.NullInt64
		if err := techRows.Scan(&uid, &t.Name, &t.AvatarIdx, &t.Quantity, &t.Records); err != nil {
			techRows.Close()
			return nil, err
		}
		if uid.Valid {
			id := uid.Int64
			t.UserID = &id
		}
		report.ByTechnician = append(report.ByTechnician, &t)
	}
	if err := techRows.Err(); err != nil {
		techRows.Close()
		return nil, err
	}
	techRows.Close()

	// ── By day ──
	dayQ := `
		SELECT c.created_at::date, COALESCE(SUM(c.quantity), 0), COUNT(*)
		FROM consumables c
		WHERE c.created_at >= $1 AND c.created_at < $2
		GROUP BY c.created_at::date
		ORDER BY c.created_at::date`
	dayRows, err := m.DB.QueryContext(ctx, dayQ, start, end)
	if err != nil {
		return nil, err
	}
	for dayRows.Next() {
		var t ConsumableDayTotal
		var d time.Time
		if err := dayRows.Scan(&d, &t.Quantity, &t.Records); err != nil {
			dayRows.Close()
			return nil, err
		}
		t.Date = d.Format("2006-01-02")
		report.ByDay = append(report.ByDay, &t)
	}
	if err := dayRows.Err(); err != nil {
		dayRows.Close()
		return nil, err
	}
	dayRows.Close()

	return report, nil
}
