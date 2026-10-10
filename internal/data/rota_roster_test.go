package data

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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

// The ORDER BY DRIFT GUARD, and the reason rotaMemberOrder is a constant at all.
//
// internal/report/attendance.go builds the attendance workbook's rows by iterating
// w.Members in slice order, so this clause is not only the public grid's row order --
// it is the spreadsheet's. A second hand-written copy would be an exported document
// whose rows had quietly stopped matching the board they came from: invisible in the
// app, glaring in the thing a manager hands to payroll.
//
// There is no database in the test suite to compare the two queries against, so the
// guarantee is made structurally instead. Both queries must name the constant, and the
// literal clause must appear EXACTLY ONCE across the package -- once, in the constant's
// own definition.
//
// WHOLE PACKAGE, NOT TWO FILES. The first version of this test globbed nothing and
// opened rota_roster.go and schedule.go by name while its comment promised "exactly
// once in the package". So the failure it exists to catch passed: a third query in
// internal/data/rota_export.go with its own copy of the clause was invisible to it.
// A drift guard that cannot see the file the drift lands in is worse than none,
// because it is reported as covered.
//
// PARSED, NOT GREPPED. Stripping // comments with a regex has two failure modes, and
// the second is the dangerous one. A /* */ block mentioning the clause is not stripped
// and produces a spurious failure -- annoying but safe. A // inside a raw string query
// -- a URL, say -- truncates the rest of that line and UNDER-COUNTS, which would let a
// genuine duplicate through. Walking the AST for string literals sidesteps both: only
// literals are considered, which is exactly where SQL lives.
func TestRotaMemberOrderIsTheOnlyMemberSort(t *testing.T) {
	found := memberSortLiterals(t)
	if len(found) != 1 {
		t.Errorf("the literal member sort clause appears %d times across the package, "+
			"want 1 (the definition of rotaMemberOrder): %v\n"+
			"every query that sorts rota members must use the constant, or the public "+
			"grid and the exported workbook will drift apart", len(found), found)
		return
	}
	// The one permitted occurrence has to be the definition, not some query that
	// happens to be the only copy because the other two were already changed.
	if !strings.HasPrefix(found[0], "rota_roster.go:") {
		t.Errorf("the clause is defined in %s; it belongs to rotaMemberOrder in "+
			"rota_roster.go", found[0])
	}

	// Both queries have to actually USE it. A constant nobody references is worse than
	// no constant, because it looks like the guard is in place.
	for _, path := range []string{"rota_roster.go", "schedule.go"} {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(src), "rotaMemberOrder") {
			t.Errorf("%s does not sort members by rotaMemberOrder", path)
		}
	}
}

// memberSortLiterals returns "file:line" for every string literal in the package that
// contains the literal member sort clause.
//
// _test.go files are skipped, and not as a convenience: this file names the clause in
// order to search for it, so counting test files would count the search.
func memberSortLiterals(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob package: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no Go files found; the test is not running in the package directory")
	}

	var found []string
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if strings.Contains(lit.Value, "u.created_at ASC") {
				found = append(found,
					fmt.Sprintf("%s:%d", path, fset.Position(lit.Pos()).Line))
			}
			return true
		})
	}
	return found
}

// THE PARTIAL-REPLACE REGRESSION GUARD.
//
// SetOrder was originally a single UPDATE touching only the ids it was given, on the
// argument that one statement cannot interleave with another. That argument was sound
// about the statement and wrong about the operation, and it cost more than a comment:
// two SEQUENTIAL orders with different id lists produced three accounts sharing each
// position, with no concurrency involved. The store now clears every unnamed position
// first, which is what makes it a whole-list replace and what makes a shared position
// structurally impossible.
//
// So the clearing statement is load-bearing behaviour, and this test is here so that
// "simplifying" SetOrder back to one UPDATE -- which is exactly what it looks like --
// fails instead of silently reintroducing the bug. There is no database in this suite,
// so as with the ORDER BY guard the guarantee is structural rather than behavioural.
//
// It also pins the transaction, because without it the two statements interleave and
// the guarantee above evaporates even though both statements are still present.
func TestSetOrderIsAWholeListReplaceNotAPartialOne(t *testing.T) {
	src, err := os.ReadFile("rota_roster.go")
	if err != nil {
		t.Fatalf("read rota_roster.go: %v", err)
	}
	body := setOrderBody(t, string(src))

	if !strings.Contains(body, "rota_position = NULL") {
		t.Error("SetOrder never clears a position; it is a PARTIAL replace, so two " +
			"sequential orders with different id lists collide and no manager chose " +
			"the result. Unnamed members must go back to NULL")
	}
	if !strings.Contains(body, "NOT (id = ANY($1))") {
		t.Error("the clearing statement must skip the submitted ids; clearing " +
			"everybody and then reassigning is not a whole-list replace either, it " +
			"is just wasteful, and the WHERE is what makes the two statements " +
			"composable")
	}
	// The guard on the clearing statement: without it an ordinary save rewrites every
	// row in the user table rather than the handful that actually moved.
	if !strings.Contains(body, "rota_position IS NOT NULL") {
		t.Error("the clearing statement must be guarded by `rota_position IS NOT " +
			"NULL`, or every reorder writes a row per account on the public grid's " +
			"read path")
	}
	for _, want := range []string{"BeginTx", "LOCK TABLE users", "tx.Commit()"} {
		if !strings.Contains(body, want) {
			t.Errorf("SetOrder is missing %q; the clear and the assign are two "+
				"statements and interleave under READ COMMITTED without a "+
				"transaction and a lock", want)
		}
	}
	if !strings.Contains(body, "RowsAffected()") {
		t.Error("SetOrder does not check RowsAffected; an account deleted between the " +
			"handler's existence check and this write returns 200 with an order that " +
			"is quietly missing somebody")
	}
}

// setOrderBody returns the source of SetOrder, so the assertions above cannot be
// satisfied by a match somewhere else in the file -- including in SetOrder's own
// comments, which describe the clearing statement at length and would otherwise
// satisfy every one of these checks on their own.
func setOrderBody(t *testing.T, src string) string {
	t.Helper()
	start := strings.Index(src, "func (m RotaRosterModel) SetOrder(")
	if start < 0 {
		t.Fatal("SetOrder not found in rota_roster.go")
	}
	// The body ends at the first line that is exactly "}" -- the method's closing
	// brace. Inner braces are indented, so this cannot stop early.
	rest := src[start:]
	if end := strings.Index(rest, "\n}\n"); end >= 0 {
		return rest[:end]
	}
	return rest
}

// rotaMemberOrder carries two properties that are load-bearing and invisible: it is
// TOTAL, and placement OUTRANKS deactivation.
//
// This test pins them against the constant's own TEXT, and it is important to be honest
// about how far that goes. It catches a mid-clause reorder and a dropped final key.
// It does NOT verify totality in the mathematical sense -- that is a property of the
// clause AND the schema, because totality needs a unique final key, and this test
// cannot see users.id's primary key. If the schema ever loses that, this still passes
// and the grid goes non-deterministic.
//
// It is kept anyway: it is a cheap change-detector for the two ways this constant is
// most likely to be broken by accident, and a change-detector that is honestly labelled
// is worth having where a misleading one would not be.
func TestRotaMemberOrderIsTotalAndPlacesBeatsDeactivation(t *testing.T) {
	clauses := strings.Split(rotaMemberOrder, ",")
	if len(clauses) < 2 {
		t.Fatalf("rotaMemberOrder is %q, which has no clauses to check", rotaMemberOrder)
	}

	last := strings.TrimSpace(clauses[len(clauses)-1])
	if last != "u.id ASC" {
		t.Errorf("the last sort clause is %q, want u.id ASC; without a unique final "+
			"key the sort is not total and rows tying on every earlier clause come "+
			"back in planner order", last)
	}

	// Placement must be decided BEFORE deactivation, or a leaver a manager placed by
	// hand sinks to the bottom and their arrangement is quietly undone by a
	// deactivation they did not ask for.
	posAt := clauseFor(clauses, "u.rota_position")
	activeAt := clauseFor(clauses, "u.active")
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
	// and a NULL would sort wherever the column default happened to put it. It is
	// redundant -- Postgres puts NULLs last under ASC anyway -- and kept on purpose: it
	// survives somebody later flipping the clause to DESC while the comment still reads
	// "placed first". That is a real hazard and this is cheaper than the bug.
	if first := strings.TrimSpace(clauses[0]); !strings.Contains(first, "rota_position IS NULL") {
		t.Errorf("the first sort clause is %q, want the rota_position IS NULL tier; "+
			"without it unplaced members interleave with placed ones", first)
	}
}

// clauseFor finds a clause by PREFIX rather than by equality.
//
// The first version compared whole strings, so the semantically identical
// "u.rota_position ASC" failed with "does not sort by rota_position at all" -- a test
// that punishes the most natural edit to the thing it guards is a test that gets
// deleted rather than fixed. The direction matters most: a prefix match still fails on
// a clause that dropped the column, which is the failure worth catching.
func clauseFor(clauses []string, prefix string) int {
	for i, c := range clauses {
		if strings.HasPrefix(strings.TrimSpace(c), prefix) {
			return i
		}
	}
	return -1
}
