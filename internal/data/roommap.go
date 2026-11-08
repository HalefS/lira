package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/HalefS/lira/internal/validator"
)

// The five states a room can be in on the map. They are strings rather than an
// enum because they travel to the browser as-is, and the translation layer keys
// off them directly.
const (
	RoomStatusUnassigned = "unassigned"
	RoomStatusUnchecked  = "unchecked"
	RoomStatusOK         = "ok"
	RoomStatusDegraded   = "degraded"
	RoomStatusDown       = "down"
)

var (
	// ErrDuplicateRoomCategory: a category with this name already exists. The
	// name is what the room cards and the category picker show, so two
	// "Standard" rows would be indistinguishable.
	ErrDuplicateRoomCategory = errors.New("duplicate room category")

	// ErrDuplicateEquipmentType: the equipment catalog is keyed by name for the
	// same reason.
	ErrDuplicateEquipmentType = errors.New("duplicate equipment type")

	// ErrUnknownEquipmentType is returned when a category or a room names an
	// equipment type that is not in the catalog. The foreign key would refuse it
	// too, but the message it produces is about a constraint rather than about
	// what the caller asked for.
	ErrUnknownEquipmentType = errors.New("unknown equipment type")

	// ErrRoomNotInInventory: the number is not covered by any range in `rooms`.
	// A room cannot be classified before it exists.
	ErrRoomNotInInventory = errors.New("room is not in the inventory")
)

const roomCategoryMaxName = 50

var equipmentIconPattern = regexp.MustCompile(`^[a-z0-9-]{1,20}$`)

// EquipmentType is one kind of equipment the hotel tracks, such as TV or Phone.
// It is a catalog row: the same type is shared by every category and every room.
type EquipmentType struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Icon      string    `json:"icon"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy *int64    `json:"created_by,omitempty"`
}

// CategoryEquipment is one line of a category's template: this type, this many
// of them. Name and Icon are denormalised into the response so the map can draw
// a room without a second lookup.
type CategoryEquipment struct {
	EquipmentTypeID int64  `json:"equipment_type_id"`
	Name            string `json:"name"`
	Icon            string `json:"icon"`
	Quantity        int    `json:"quantity"`
}

// RoomCategory is a template a room can be assigned to: "Standard = one TV and
// one phone". It says what SHOULD be in a room, never what is actually there.
type RoomCategory struct {
	ID        int64               `json:"id"`
	Name      string              `json:"name"`
	Color     string              `json:"color"`
	CreatedAt time.Time           `json:"created_at"`
	CreatedBy *int64              `json:"created_by,omitempty"`
	Equipment []CategoryEquipment `json:"equipment"`
}

// RoomEquipmentCount is what a survey found for one equipment type in one room,
// beside what the room's category expects. Expected is zero for equipment the
// category does not call for but a survey still recorded (a router somebody
// added to one room, for instance).
type RoomEquipmentCount struct {
	EquipmentTypeID int64  `json:"equipment_type_id"`
	Name            string `json:"name"`
	Icon            string `json:"icon"`
	Expected        int    `json:"expected"`
	Present         int    `json:"present"`
	Working         int    `json:"working"`
}

// MappedRoom is one room on the map.
type MappedRoom struct {
	RoomNumber    int                  `json:"room_number"`
	Floor         int                  `json:"floor"`
	CategoryID    *int64               `json:"category_id"`
	CategoryName  string               `json:"category_name"`
	CategoryColor string               `json:"category_color"`
	SurveyedAt    *time.Time           `json:"surveyed_at"`
	Notes         string               `json:"notes"`
	UpdatedAt     *time.Time           `json:"updated_at"`
	UpdatedBy     *int64               `json:"updated_by"`
	Status        string               `json:"status"`
	Equipment     []RoomEquipmentCount `json:"equipment"`
}

// RoomMapSummary is the count behind each summary tile. Five buckets, and they
// add up to Total: unassigned + unchecked + ok + degraded + down.
type RoomMapSummary struct {
	Total      int `json:"total"`
	Unassigned int `json:"unassigned"`
	Unchecked  int `json:"unchecked"`
	OK         int `json:"ok"`
	Degraded   int `json:"degraded"`
	Down       int `json:"down"`
}

// RoomMap is the whole page in one response: the catalogs the UI needs to edit
// categories, and every room with its derived status.
type RoomMap struct {
	Categories     []*RoomCategory  `json:"categories"`
	EquipmentTypes []*EquipmentType `json:"equipment_types"`
	Rooms          []*MappedRoom    `json:"rooms"`
	Summary        RoomMapSummary   `json:"summary"`
}

// SaveRoomInput is what PUT /v1/room-map/:room accepts. CategoryID nil means
// "unassigned"; Equipment is the full set of counts the technician recorded, not
// a patch, so removing a row in the form removes it here.
type SaveRoomInput struct {
	CategoryID *int64
	Notes      string
	Surveyed   bool
	Equipment  []RoomEquipmentInput
}

type RoomEquipmentInput struct {
	EquipmentTypeID int64 `json:"equipment_type_id"`
	Present         int   `json:"present"`
	Working         int   `json:"working"`
}

// RoomFloor derives a floor from the room number's leading digit. It is a v1
// convention -- 1214 is on floor 1, 4120 on floor 4 -- and deliberately lives in
// one function so a hotel whose numbering works differently can change it
// without touching the map.
func RoomFloor(room int) int {
	if room <= 0 {
		return 0
	}
	s := strconv.Itoa(room)
	d, err := strconv.Atoi(s[:1])
	if err != nil {
		return 0
	}
	return d
}

// ComputeRoomStatus is the whole verdict, in one place, so the grid, the room
// dialog and the summary tiles cannot disagree.
//
// A room with no category is unassigned even if somebody recorded equipment in
// it: there is no template to compare against, so calling it ok or degraded
// would be inventing an expectation.
//
// A classified room nobody has surveyed is unchecked, not ok. "We have not
// looked" and "we looked and it is fine" are different operational facts, and
// collapsing them would hide every room on the first day of a rollout.
//
// After that, everything the category expects must be both present and working
// in the expected quantity. If nothing expected is working at all the room is
// down; anything in between is degraded.
func ComputeRoomStatus(category *RoomCategory, equipment []RoomEquipmentCount, surveyed bool) string {
	if category == nil {
		return RoomStatusUnassigned
	}
	if !surveyed {
		return RoomStatusUnchecked
	}
	if len(category.Equipment) == 0 {
		return RoomStatusOK
	}

	counts := make(map[int64]RoomEquipmentCount, len(equipment))
	for _, e := range equipment {
		counts[e.EquipmentTypeID] = e
	}

	allOK := true
	anyWorking := false
	for _, want := range category.Equipment {
		got := counts[want.EquipmentTypeID]
		if got.Working > 0 {
			anyWorking = true
		}
		if got.Present >= want.Quantity && got.Working >= want.Quantity {
			continue
		}
		allOK = false
	}
	if allOK {
		return RoomStatusOK
	}
	if !anyWorking {
		return RoomStatusDown
	}
	return RoomStatusDegraded
}

// ValidateRoomCategory checks the name and colour of a category. The equipment
// lines are checked separately, because their errors belong to individual rows.
func ValidateRoomCategory(v *validator.Validator, name, color string) {
	name = strings.TrimSpace(name)
	v.Check(name != "", "name", "must be provided")
	v.Check(len(name) <= roomCategoryMaxName,
		"name", fmt.Sprintf("must not be more than %d characters", roomCategoryMaxName))
	if color != "" {
		ValidateColorHex(v, "color", color)
	}
}

// ValidateCategoryEquipment checks a category's template lines.
func ValidateCategoryEquipment(v *validator.Validator, equipment []CategoryEquipment) {
	seen := make(map[int64]bool, len(equipment))
	for i, e := range equipment {
		field := fmt.Sprintf("equipment.%d", i)
		v.Check(e.EquipmentTypeID > 0, field, "must name an equipment type")
		v.Check(e.Quantity >= 1 && e.Quantity <= 99,
			field, "quantity must be between 1 and 99")
		v.Check(!seen[e.EquipmentTypeID], field, "equipment type is listed twice")
		seen[e.EquipmentTypeID] = true
	}
}

// ValidateRoomEquipment checks the counts recorded in one room.
func ValidateRoomEquipment(v *validator.Validator, equipment []RoomEquipmentInput) {
	seen := make(map[int64]bool, len(equipment))
	for i, e := range equipment {
		field := fmt.Sprintf("equipment.%d", i)
		v.Check(e.EquipmentTypeID > 0, field, "must name an equipment type")
		v.Check(!seen[e.EquipmentTypeID], field, "equipment type is listed twice")
		seen[e.EquipmentTypeID] = true
		v.Check(e.Present >= 0 && e.Present <= 99,
			field, "present count must be between 0 and 99")
		v.Check(e.Working >= 0 && e.Working <= 99,
			field, "working count must be between 0 and 99")
		v.Check(e.Working <= e.Present, field, "working cannot be more than present")
	}
}

func ValidateEquipmentType(v *validator.Validator, name, icon string) {
	name = strings.TrimSpace(name)
	v.Check(name != "", "name", "must be provided")
	v.Check(len(name) <= roomCategoryMaxName,
		"name", fmt.Sprintf("must not be more than %d characters", roomCategoryMaxName))
	if icon != "" {
		v.Check(equipmentIconPattern.MatchString(icon),
			"icon", "must be a short lowercase token")
	}
}

// RoomMapModel owns both the catalogs (categories, equipment types) and the
// per-room records.
type RoomMapModel struct {
	DB *sql.DB
}

const categoryColumns = `c.id, c.name, c.color, c.created_at, c.created_by`

// Categories returns every category with its template lines, ordered by name.
func (m RoomMapModel) Categories() ([]*RoomCategory, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, `
		SELECT `+categoryColumns+`,
		       ce.equipment_type_id, e.name, e.icon, ce.quantity
		FROM room_categories c
		LEFT JOIN room_category_equipment ce ON ce.category_id = c.id
		LEFT JOIN equipment_types e ON e.id = ce.equipment_type_id
		ORDER BY c.name, e.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	categories := []*RoomCategory{}
	byID := make(map[int64]*RoomCategory)
	for rows.Next() {
		var (
			c         RoomCategory
			createdBy sql.NullInt64
			etID      sql.NullInt64
			etName    sql.NullString
			etIcon    sql.NullString
			quantity  sql.NullInt64
		)
		if err := rows.Scan(&c.ID, &c.Name, &c.Color, &c.CreatedAt, &createdBy,
			&etID, &etName, &etIcon, &quantity); err != nil {
			return nil, err
		}
		if createdBy.Valid {
			c.CreatedBy = &createdBy.Int64
		}
		found, ok := byID[c.ID]
		if !ok {
			c.Equipment = []CategoryEquipment{}
			found = &c
			byID[c.ID] = found
			categories = append(categories, found)
		}
		if etID.Valid && quantity.Valid {
			found.Equipment = append(found.Equipment, CategoryEquipment{
				EquipmentTypeID: etID.Int64,
				Name:            etName.String,
				Icon:            etIcon.String,
				Quantity:        int(quantity.Int64),
			})
		}
	}
	return categories, rows.Err()
}

// Category returns one category, with its template lines, or ErrRecordNotFound.
func (m RoomMapModel) Category(id int64) (*RoomCategory, error) {
	categories, err := m.Categories()
	if err != nil {
		return nil, err
	}
	for _, c := range categories {
		if c.ID == id {
			return c, nil
		}
	}
	return nil, ErrRecordNotFound
}

// EquipmentTypes returns the equipment catalog, ordered by name.
func (m RoomMapModel) EquipmentTypes() ([]*EquipmentType, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, `
		SELECT id, name, icon, created_at, created_by
		FROM equipment_types
		ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	types := []*EquipmentType{}
	for rows.Next() {
		var (
			et        EquipmentType
			createdBy sql.NullInt64
		)
		if err := rows.Scan(&et.ID, &et.Name, &et.Icon, &et.CreatedAt, &createdBy); err != nil {
			return nil, err
		}
		if createdBy.Valid {
			et.CreatedBy = &createdBy.Int64
		}
		types = append(types, &et)
	}
	return types, rows.Err()
}

// RoomMap assembles the whole map. The inventory decides which rooms exist; the
// unit and equipment tables only say what has been recorded about them.
func (m RoomMapModel) RoomMap() (*RoomMap, error) {
	categories, err := m.Categories()
	if err != nil {
		return nil, err
	}
	types, err := m.EquipmentTypes()
	if err != nil {
		return nil, err
	}
	rooms, err := m.rooms()
	if err != nil {
		return nil, err
	}
	counts, err := m.equipmentCounts()
	if err != nil {
		return nil, err
	}

	catByID := make(map[int64]*RoomCategory, len(categories))
	for _, c := range categories {
		catByID[c.ID] = c
	}

	summary := RoomMapSummary{Total: len(rooms)}
	for _, room := range rooms {
		category := categoryFor(room.CategoryID, catByID)
		room.Equipment = addExpected(buildRoomEquipment(counts[room.RoomNumber]), category)
		room.Status = ComputeRoomStatus(category, room.Equipment, room.SurveyedAt != nil)

		switch room.Status {
		case RoomStatusUnassigned:
			summary.Unassigned++
		case RoomStatusUnchecked:
			summary.Unchecked++
		case RoomStatusOK:
			summary.OK++
		case RoomStatusDegraded:
			summary.Degraded++
		case RoomStatusDown:
			summary.Down++
		}
	}

	return &RoomMap{
		Categories:     categories,
		EquipmentTypes: types,
		Rooms:          rooms,
		Summary:        summary,
	}, nil
}

// Room returns one room from the assembled map. The map is a few hundred short
// rows, so reusing RoomMap keeps a single definition of how a room is derived
// rather than a second query path that can drift.
func (m RoomMapModel) Room(number int) (*MappedRoom, error) {
	snapshot, err := m.RoomMap()
	if err != nil {
		return nil, err
	}
	for _, r := range snapshot.Rooms {
		if r.RoomNumber == number {
			return r, nil
		}
	}
	return nil, ErrRecordNotFound
}

func categoryFor(id *int64, byID map[int64]*RoomCategory) *RoomCategory {
	if id == nil {
		return nil
	}
	return byID[*id]
}

// rooms reads every room the inventory covers, with whatever is recorded against
// it. DISTINCT protects against overlapping runs: Add refuses to create them,
// but a map that silently doubled a room would be a poor place to discover that
// one slipped in.
func (m RoomMapModel) rooms() ([]*MappedRoom, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, `
		WITH inventory AS (
			SELECT DISTINCT g.n AS room_number
			FROM rooms, LATERAL generate_series(rooms.from_room, rooms.to_room) AS g(n)
		)
		SELECT i.room_number, u.category_id, u.surveyed_at, u.notes, u.updated_at, u.updated_by,
		       c.name, c.color
		FROM inventory i
		LEFT JOIN room_units u ON u.room_number = i.room_number
		LEFT JOIN room_categories c ON c.id = u.category_id
		ORDER BY i.room_number`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rooms := []*MappedRoom{}
	for rows.Next() {
		var (
			r             MappedRoom
			categoryID    sql.NullInt64
			surveyedAt    sql.NullTime
			notes         sql.NullString
			updatedAt     sql.NullTime
			updatedBy     sql.NullInt64
			categoryName  sql.NullString
			categoryColor sql.NullString
		)
		if err := rows.Scan(&r.RoomNumber, &categoryID, &surveyedAt, &notes,
			&updatedAt, &updatedBy, &categoryName, &categoryColor); err != nil {
			return nil, err
		}
		if categoryID.Valid {
			r.CategoryID = &categoryID.Int64
		}
		if surveyedAt.Valid {
			r.SurveyedAt = &surveyedAt.Time
		}
		if notes.Valid {
			r.Notes = notes.String
		}
		if updatedAt.Valid {
			r.UpdatedAt = &updatedAt.Time
		}
		if updatedBy.Valid {
			r.UpdatedBy = &updatedBy.Int64
		}
		r.CategoryName = categoryName.String
		r.CategoryColor = categoryColor.String
		r.Floor = RoomFloor(r.RoomNumber)
		r.Equipment = []RoomEquipmentCount{}
		rooms = append(rooms, &r)
	}
	return rooms, rows.Err()
}

// equipmentCounts reads every recorded count, keyed by room. Rooms with nothing
// recorded simply have no entry.
func (m RoomMapModel) equipmentCounts() (map[int][]RoomEquipmentCount, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, `
		SELECT re.room_number, re.equipment_type_id, e.name, e.icon, re.present, re.working
		FROM room_equipment re
		JOIN equipment_types e ON e.id = re.equipment_type_id
		ORDER BY re.room_number, e.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byRoom := make(map[int][]RoomEquipmentCount)
	for rows.Next() {
		var room int
		var e RoomEquipmentCount
		if err := rows.Scan(&room, &e.EquipmentTypeID, &e.Name, &e.Icon, &e.Present, &e.Working); err != nil {
			return nil, err
		}
		byRoom[room] = append(byRoom[room], e)
	}
	return byRoom, rows.Err()
}

// buildRoomEquipment sorts the counts recorded for one room. Merging the
// category's expected lines happens in addExpected, because only the room knows
// which category it was given.
func buildRoomEquipment(actual []RoomEquipmentCount) []RoomEquipmentCount {
	out := make([]RoomEquipmentCount, 0, len(actual))
	for _, e := range actual {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].EquipmentTypeID < out[j].EquipmentTypeID
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// addExpected fills in the category's template lines, including ones nobody has
// counted yet.
func addExpected(out []RoomEquipmentCount, category *RoomCategory) []RoomEquipmentCount {
	if category == nil {
		return out
	}
	byType := make(map[int64]RoomEquipmentCount, len(out))
	for _, e := range out {
		byType[e.EquipmentTypeID] = e
	}
	for _, want := range category.Equipment {
		row, ok := byType[want.EquipmentTypeID]
		if !ok {
			row = RoomEquipmentCount{
				EquipmentTypeID: want.EquipmentTypeID,
				Name:            want.Name,
				Icon:            want.Icon,
			}
		}
		row.Expected = want.Quantity
		byType[want.EquipmentTypeID] = row
	}
	merged := make([]RoomEquipmentCount, 0, len(byType))
	for _, e := range byType {
		merged = append(merged, e)
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Name == merged[j].Name {
			return merged[i].EquipmentTypeID < merged[j].EquipmentTypeID
		}
		return merged[i].Name < merged[j].Name
	})
	return merged
}

// CreateCategory inserts a category and its template in one transaction.
func (m RoomMapModel) CreateCategory(name, color string, createdBy int64, equipment []CategoryEquipment) (*RoomCategory, error) {
	name = strings.TrimSpace(name)
	if color == "" {
		color = "#1769AA"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO room_categories (name, color, created_by)
		VALUES ($1, $2, $3)
		RETURNING id`, name, color, createdBy).Scan(&id)
	if err != nil {
		if isDuplicateConstraint(err, "room_categories_name_key") {
			return nil, ErrDuplicateRoomCategory
		}
		return nil, err
	}

	if err := replaceCategoryEquipment(ctx, tx, id, equipment); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return m.Category(id)
}

// UpdateCategory replaces a category's name, colour and template. The room
// records are untouched: changing what a category expects changes each room's
// status, which is the point, but it must not rewrite what a survey found.
func (m RoomMapModel) UpdateCategory(id int64, name, color string, equipment []CategoryEquipment) (*RoomCategory, error) {
	name = strings.TrimSpace(name)
	if color == "" {
		color = "#1769AA"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Checked before the write so the caller gets a proper 404 rather than a
	// successful UPDATE of zero rows.
	var exists int64
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM room_categories WHERE id = $1`, id).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE room_categories SET name = $1, color = $2 WHERE id = $3`,
		name, color, id)
	if err != nil {
		if isDuplicateConstraint(err, "room_categories_name_key") {
			return nil, ErrDuplicateRoomCategory
		}
		return nil, err
	}
	if err := replaceCategoryEquipment(ctx, tx, id, equipment); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return m.Category(id)
}

func replaceCategoryEquipment(ctx context.Context, tx *sql.Tx, categoryID int64, equipment []CategoryEquipment) error {
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM room_category_equipment WHERE category_id = $1`, categoryID); err != nil {
		return err
	}
	seen := make(map[int64]bool, len(equipment))
	for _, e := range equipment {
		if seen[e.EquipmentTypeID] {
			continue
		}
		seen[e.EquipmentTypeID] = true

		var exists int64
		if err := tx.QueryRowContext(ctx,
			`SELECT id FROM equipment_types WHERE id = $1`, e.EquipmentTypeID).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrUnknownEquipmentType
			}
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO room_category_equipment (category_id, equipment_type_id, quantity)
			VALUES ($1, $2, $3)`, categoryID, e.EquipmentTypeID, e.Quantity); err != nil {
			return err
		}
	}
	return nil
}

// DeleteCategory removes a category. Rooms keep their equipment records and
// become unassigned through ON DELETE SET NULL.
func (m RoomMapModel) DeleteCategory(id int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := m.DB.ExecContext(ctx, `DELETE FROM room_categories WHERE id = $1`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrRecordNotFound
	}
	return nil
}

func (m RoomMapModel) CreateEquipmentType(name, icon string, createdBy int64) (*EquipmentType, error) {
	name = strings.TrimSpace(name)
	if icon == "" {
		icon = "box"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	et := &EquipmentType{Name: name, Icon: icon}
	err := m.DB.QueryRowContext(ctx, `
		INSERT INTO equipment_types (name, icon, created_by)
		VALUES ($1, $2, $3)
		RETURNING id, created_at`, name, icon, createdBy).
		Scan(&et.ID, &et.CreatedAt)
	if err != nil {
		if isDuplicateConstraint(err, "equipment_types_name_key") {
			return nil, ErrDuplicateEquipmentType
		}
		return nil, err
	}
	return et, nil
}

// DeleteEquipmentType removes a type from the catalog. The foreign keys remove
// it from every category template and every room record, so the caller is
// expected to have confirmed the consequence.
func (m RoomMapModel) DeleteEquipmentType(id int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := m.DB.ExecContext(ctx, `DELETE FROM equipment_types WHERE id = $1`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrRecordNotFound
	}
	return nil
}

// SaveRoom records one room's classification and survey. It replaces the room's
// equipment rows rather than patching them, so a type removed in the form is
// removed here too.
func (m RoomMapModel) SaveRoom(number int, input SaveRoomInput, userID int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists int
	err = tx.QueryRowContext(ctx,
		`SELECT 1 FROM rooms WHERE $1 BETWEEN from_room AND to_room LIMIT 1`, number).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrRoomNotInInventory
	}
	if err != nil {
		return err
	}

	if input.CategoryID != nil {
		var id int64
		err := tx.QueryRowContext(ctx,
			`SELECT id FROM room_categories WHERE id = $1`, *input.CategoryID).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRecordNotFound
		}
		if err != nil {
			return err
		}
	}

	// The unit row has to exist before its equipment rows: room_equipment's
	// foreign key points at room_units, not at the inventory range.
	var surveyedAt *time.Time
	if input.Surveyed {
		now := time.Now().UTC()
		surveyedAt = &now
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO room_units (room_number, category_id, surveyed_at, notes, updated_at, updated_by)
		VALUES ($1, $2, $3, $4, NOW(), $5)
		ON CONFLICT (room_number) DO UPDATE
		SET category_id = EXCLUDED.category_id,
		    surveyed_at = EXCLUDED.surveyed_at,
		    notes = EXCLUDED.notes,
		    updated_at = NOW(),
		    updated_by = EXCLUDED.updated_by`,
		number, input.CategoryID, surveyedAt, strings.TrimSpace(input.Notes), userID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM room_equipment WHERE room_number = $1`, number); err != nil {
		return err
	}

	seen := make(map[int64]bool, len(input.Equipment))
	for _, e := range input.Equipment {
		if seen[e.EquipmentTypeID] {
			continue
		}
		seen[e.EquipmentTypeID] = true

		var id int64
		err := tx.QueryRowContext(ctx,
			`SELECT id FROM equipment_types WHERE id = $1`, e.EquipmentTypeID).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUnknownEquipmentType
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO room_equipment (room_number, equipment_type_id, present, working)
			VALUES ($1, $2, $3, $4)`, number, e.EquipmentTypeID, e.Present, e.Working); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// isDuplicateConstraint recognises PostgreSQL's unique-violation text for the
// catalog tables. Matching the constraint name rather than any unique error
// keeps a future unique constraint from being reported as a duplicate name.
func isDuplicateConstraint(err error, constraint string) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), `duplicate key value violates unique constraint "`+constraint+`"`)
}
