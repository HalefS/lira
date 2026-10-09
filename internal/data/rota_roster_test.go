package data

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/HalefS/lira/internal/validator"
)

// Roster validation.
//
// The nil case is the important one and it is unreachable over HTTP by design -- the
// handler refuses it with a 422 before Set is called -- which is exactly why it needs a
// test. A rule no request can exercise is a rule nobody maintains, and this one is
// standing between a silently wrong board and a visibly empty one: a nil slice binds as
// SQL NULL, user_id = ANY(NULL) is NULL rather than true, both of Set's statements would
// match nothing, and the write would return 200 having changed nothing at all.
func TestValidateRosterNilIsRefusedNotTreatedAsEmpty(t *testing.T) {
	v := validator.New()
	ValidateRoster(v, nil)

	if v.Valid() {
		t.Fatal("a nil member_ids was accepted; Set would bind SQL NULL and silently " +
			"change nothing while reporting success")
	}
	if _, ok := v.Errors["member_ids"]; !ok {
		t.Errorf("expected the error on member_ids, got %v", v.Errors)
	}
}

// An EMPTY slice is the opposite of nil, and that asymmetry is the reason this function
// exists at all: empty is a decision a manager is entitled to make, and it is the whole
// justification for storing a flag instead of using a membership set.
func TestValidateRosterEmptyIsLegal(t *testing.T) {
	v := validator.New()
	ValidateRoster(v, []int64{})
	if !v.Valid() {
		t.Fatalf("an empty roster was refused: %v", v.Errors)
	}
}

// The two must be distinguishable. A future refactor that "tidies" these into one
// branch would turn an intentional empty board into a 422, or -- worse the other way --
// turn a nil into a silently empty board.
func TestValidateRosterNilAndEmptyDiffer(t *testing.T) {
	nilV, emptyV := validator.New(), validator.New()
	ValidateRoster(nilV, nil)
	ValidateRoster(emptyV, []int64{})
	if nilV.Valid() == emptyV.Valid() {
		t.Error("nil and empty must not be treated alike: one is a malformed request, " +
			"the other is a legal decision to put nobody on the rota")
	}
}

func TestValidateRoster(t *testing.T) {
	cases := []struct {
		name string
		ids  []int64
		ok   bool
	}{
		{"a normal roster", []int64{1, 2, 3}, true},
		{"one member", []int64{1}, true},
		{"zero is not an id", []int64{0}, false},
		{"negative is not an id", []int64{-4}, false},
		{"a duplicate", []int64{7, 7}, false},
		{"a duplicate among many", []int64{1, 2, 2, 3}, false},
		{"good id alongside a bad one", []int64{5, 0}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := validator.New()
			ValidateRoster(v, tc.ids)
			if got := v.Valid(); got != tc.ok {
				t.Fatalf("valid = %v, want %v (errors: %v)", got, tc.ok, v.Errors)
			}
			if !tc.ok {
				if _, ok := v.Errors["member_ids"]; !ok {
					t.Errorf("expected the error on member_ids, got %v", v.Errors)
				}
			}
		})
	}
}

// The bound is reachable through the API, so it is a real check here and not only a
// database constraint.
func TestValidateRosterTooMany(t *testing.T) {
	v := validator.New()
	tooMany := make([]int64, MaxRotaMembers+1)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	ValidateRoster(v, tooMany)
	if v.Valid() {
		t.Fatalf("expected %d ids to be refused", MaxRotaMembers+1)
	}

	atLimit := validator.New()
	exactly := make([]int64, MaxRotaMembers)
	for i := range exactly {
		exactly[i] = int64(i + 1)
	}
	ValidateRoster(atLimit, exactly)
	if !atLimit.Valid() {
		t.Fatalf("exactly %d must be allowed: %v", MaxRotaMembers, atLimit.Errors)
	}
}

// MaxRotaMembers must be far above any real team, because a manager of a large property
// must not hit it by accident. If this number ever drops to something plausible as a
// headcount, the bound has stopped being a runaway guard and become a product limit.
func TestMaxRotaMembersIsAGuardNotALimit(t *testing.T) {
	if MaxRotaMembers < 50 {
		t.Errorf("MaxRotaMembers is %d, which is close enough to a real headcount to "+
			"be mistaken for a product limit", MaxRotaMembers)
	}
}

// -- Order validation ---------------------------------------------------------
//
// ValidateRosterOrder is the same rule as ValidateRoster over a different field, so
// most of what matters about it is that it stays the same rule. The tests below are
// about the two agreeing, because that is the property that silently erodes when two
// rules live in two places.

func TestValidateRosterOrderNilIsRefusedNotTreatedAsEmpty(t *testing.T) {
	v := validator.New()
	ValidateRosterOrder(v, nil)

	if v.Valid() {
		t.Fatal("a nil user_ids was accepted; SetOrder would bind SQL NULL, unnest " +
			"would yield no rows, and the write would report success having changed " +
			"nothing at all")
	}
	if _, ok := v.Errors["user_ids"]; !ok {
		t.Errorf("expected the error on user_ids, got %v", v.Errors)
	}
	// The wrong key is the specific failure worth catching: a 422 naming member_ids
	// for a request that sent user_ids reads to a manager as the server confusing the
	// two endpoints, and it would send them looking for a bug in the roster.
	if _, ok := v.Errors["member_ids"]; ok {
		t.Error("the error was reported on member_ids; the order body field is user_ids")
	}
}

// An EMPTY order is legal and needs no acknowledgement, which is the one place the two
// validators legitimately differ: an empty ROSTER takes somebody off a public board,
// an empty ORDER puts the installation back in the state it was already in.
func TestValidateRosterOrderEmptyIsLegal(t *testing.T) {
	v := validator.New()
	ValidateRosterOrder(v, []int64{})
	if !v.Valid() {
		t.Fatalf("an empty order was refused: %v", v.Errors)
	}
}

func TestValidateRosterOrder(t *testing.T) {
	cases := []struct {
		name string
		ids  []int64
		ok   bool
	}{
		{"a normal order", []int64{1, 2, 3}, true},
		{"one member", []int64{1}, true},
		{"zero is not an id", []int64{0}, false},
		{"negative is not an id", []int64{-4}, false},
		// A duplicate is meaningless in an ORDER in a way it is not in a roster. The
		// roster check exists to stop a client submitting a set twice; here the
		// sequence IS the data, so a repeat means one of the two entries is meant to
		// occupy two places and there is no way to know which. With ORDINALITY the
		// second copy would silently overwrite the first, so the check earns its keep.
		{"a duplicate", []int64{7, 7}, false},
		{"a duplicate among many", []int64{1, 2, 2, 3}, false},
		{"good id alongside a bad one", []int64{5, 0}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := validator.New()
			ValidateRosterOrder(v, tc.ids)
			if got := v.Valid(); got != tc.ok {
				t.Fatalf("valid = %v, want %v (errors: %v)", got, tc.ok, v.Errors)
			}
			if !tc.ok {
				if _, ok := v.Errors["user_ids"]; !ok {
					t.Errorf("expected the error on user_ids, got %v", v.Errors)
				}
			}
		})
	}
}

func TestValidateRosterOrderTooMany(t *testing.T) {
	tooMany := make([]int64, MaxRotaMembers+1)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	v := validator.New()
	ValidateRosterOrder(v, tooMany)
	if v.Valid() {
		t.Fatalf("expected %d ids to be refused", MaxRotaMembers+1)
	}

	atLimit := validator.New()
	exactly := make([]int64, MaxRotaMembers)
	for i := range exactly {
		exactly[i] = int64(i + 1)
	}
	ValidateRosterOrder(atLimit, exactly)
	if !atLimit.Valid() {
		t.Fatalf("exactly %d must be allowed: %v", MaxRotaMembers, atLimit.Valid())
	}
}

// THE DRIFT GUARD.
//
// Both validators delegate to validateIDList, so they cannot disagree about the RULES.
// They are written out as separate functions anyway -- different field, different
// meaning, different caller -- and this is the test that stops the pair from being
// "tidied" into a shared call that takes the wrong field name, or into one that
// quietly drops the duplicate check because order "does not need" it.
//
// It asserts they agree on VALIDITY for every input, and that where they both fail
// they fail under their own field key. It cannot assert the messages match, and does
// not try: those are two different sentences about two different things.
func TestValidateRosterAndRosterOrderAgreeOnShape(t *testing.T) {
	cases := [][]int64{
		nil,
		{},
		{1},
		{1, 2, 3},
		{0},
		{-1},
		{5, 5},
		{1, 2, 2, 3},
		{9, 0, 9},
	}
	for _, ids := range cases {
		roster, order := validator.New(), validator.New()
		ValidateRoster(roster, ids)
		ValidateRosterOrder(order, ids)

		if roster.Valid() != order.Valid() {
			t.Errorf("%v: roster valid = %v but order valid = %v; the two rules are "+
				"meant to be the same rule and have drifted", ids,
				roster.Valid(), order.Valid())
		}
		if !roster.Valid() {
			if _, ok := roster.Errors["member_ids"]; !ok {
				t.Errorf("%v: roster error not on member_ids: %v", ids, roster.Errors)
			}
			if _, ok := order.Errors["user_ids"]; !ok {
				t.Errorf("%v: order error not on user_ids: %v", ids, order.Errors)
			}
		}
	}
}

// -- The shared ORDER BY ------------------------------------------------------

// stripGoLineComments removes // comments so a source assertion cannot be satisfied --
// or defeated -- by the word appearing in prose. Every Go file in this package uses
// line comments only, which is what makes the cheap regex sufficient here; a file that
// grew a block comment would need this to be smarter, and the test would tell it.
var lineComment = regexp.MustCompile(`//[^\n]*`)

func goCode(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return lineComment.ReplaceAllString(string(raw), "")
}

// THE ORDER BY DRIFT GUARD, and the reason rotaMemberOrder is a constant at all.
//
// internal/report/attendance.go builds the attendance workbook's rows by iterating
// w.Members in slice order, so this clause is not only the public grid's row order --
// it is the spreadsheet's. A second hand-written copy in either query would be an
// exported document whose rows had quietly stopped matching the board they came from,
// which is invisible in the app and glaring in the thing a manager hands to payroll.
//
// There is no database in the test suite to compare the two against, so the guarantee
// is made structurally instead: both queries must name the constant, and the literal
// clause must appear EXACTLY ONCE in the package -- once, in the constant's own
// definition. If somebody hand-rolls a third query with its own copy, the count goes to
// two and this fails rather than letting the order drift.
func TestRotaMemberOrderIsTheOnlyMemberSort(t *testing.T) {
	// rota_roster.go holds the constant, so the one permitted occurrence is its
	// definition. schedule.go must not have any at all.
	for path, want := range map[string]int{
		"rota_roster.go": 1,
		"schedule.go":    0,
	} {
		code := goCode(t, path)
		if !strings.Contains(code, "rotaMemberOrder") {
			t.Errorf("%s does not sort members by rotaMemberOrder", path)
		}
		if got := strings.Count(code, "u.created_at ASC"); got != want {
			t.Errorf("%s contains the literal member sort clause %d times, want %d; "+
				"every member query must order by rotaMemberOrder so the grid and the "+
				"exported workbook cannot drift apart", path, got, want)
		}
	}
}

// rotaMemberOrder carries two properties that are load-bearing and invisible: it is
// TOTAL, and placement OUTRANKS deactivation.
//
// Both are pinned here against the constant's own text rather than against a database,
// because there is no database in the suite. The last clause being the unique id is
// what makes the sort deterministic at all -- rota_position is deliberately not dense
// (migration 000033), so two placed members can carry the same value, and without
// this clause their relative order would be whatever the planner felt like.
func TestRotaMemberOrderIsTotalAndPlacesBeatsDeactivation(t *testing.T) {
	clauses := strings.Split(rotaMemberOrder, ",")
	if len(clauses) < 2 {
		t.Fatalf("rotaMemberOrder is %q, which has no clauses to check", rotaMemberOrder)
	}

	last := strings.TrimSpace(clauses[len(clauses)-1])
	if last != "u.id ASC" {
		t.Errorf("the last sort clause is %q, want u.id ASC; without a unique final "+
			"key the sort is not total and equal-positioned members can swap between "+
			"identical requests", last)
	}

	// Placement must be decided BEFORE deactivation, or a leaver a manager placed by
	// hand sinks to the bottom and their arrangement is quietly undone by a
	// deactivation they did not ask for.
	posAt, activeAt := clauseIndex(clauses, "u.rota_position"), clauseIndex(clauses, "u.active DESC")
	if posAt < 0 {
		t.Error("rotaMemberOrder does not sort by rota_position at all")
	}
	if activeAt < 0 {
		t.Error("rotaMemberOrder does not sink deactivated members")
	}
	if posAt >= 0 && activeAt >= 0 && posAt > activeAt {
		t.Error("rotaMemberOrder sorts deactivation before placement, so a leaver " +
			"somebody placed by hand loses their slot")
	}

	// The NULL tier has to lead, or unplaced members would interleave with placed ones
	// and a NULL would sort wherever the column default happened to put it.
	if first := strings.TrimSpace(clauses[0]); !strings.Contains(first, "rota_position IS NULL") {
		t.Errorf("the first sort clause is %q, want the rota_position IS NULL tier; "+
			"without it unplaced members interleave with placed ones", first)
	}
}

func clauseIndex(clauses []string, want string) int {
	for i, c := range clauses {
		if strings.TrimSpace(c) == want {
			return i
		}
	}
	return -1
}
