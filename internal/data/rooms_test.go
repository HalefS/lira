package data

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

// The Settings field is the only way a room enters the inventory, so what it
// accepts is the whole contract. A range expands to every whole number in
// between -- the plain reading -- which is why the bounds and the width are
// checked here rather than trusted to the database.
func TestParseRoomRun(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantFrom int
		wantTo   int
		wantErr  string // substring, empty for success
	}{
		{"single room", "1214", 1214, 1214, ""},
		{"single room with spaces around it", "  1214 ", 1214, 1214, ""},
		{"range", "1201-1212", 1201, 1212, ""},
		{"range with spaces either side of the hyphen", "1201 - 1212", 1201, 1212, ""},
		{"range typed as two dots", "1201..1212", 1201, 1212, ""},
		{"range typed with an en dash", "1201–1212", 1201, 1212, ""},
		{"range typed with an em dash", "1201—1212", 1201, 1212, ""},
		{"range typed as 'to'", "1201 to 1212", 1201, 1212, ""},
		// Reversed bounds are a normal slip, so they are corrected rather than
		// refused. Refusing them would mean the manager fixes the typo and
		// resubmits for no gain.
		{"reversed range is corrected", "1212-1201", 1201, 1212, ""},
		{"single digit", "5", 5, 5, ""},
		{"one room expressed as a range", "1214-1214", 1214, 1214, ""},
		{"range crossing what look like floors", "1299-1302", 1299, 1302, ""},

		// Refusals, each with a message about what was typed rather than a
		// constraint name.
		{"empty", "", 0, 0, "required"},
		{"only spaces", "   ", 0, 0, "required"},
		{"letters", "12l4", 0, 0, "not a room number"},
		{"a word", "penthouse", 0, 0, "not a room number"},
		{"internal space", "12 14", 0, 0, "not a room number"},
		{"negative", "-1214", 0, 0, "not a room number"},
		{"decimal", "1214.5", 0, 0, "not a room number"},
		{"trailing letters after a range", "1201-1212a", 0, 0, "not a room number"},
		{"three bounds", "1201-1212-1220", 0, 0, "1201-1212"},
		{"trailing hyphen", "1201-", 0, 0, "not a room number"},
		{"leading hyphen", "-1212", 0, 0, "not a room number"},
		{"zero is not a room", "0", 0, 0, ""}, // parses; refused later as not positive
		{"too many digits", "1234567", 0, 0, "not a room number"},

		// The width bound. This is the guard against a typo that would otherwise
		// put a hundred thousand rooms that do not exist into the dropdown.
		{"a whole floor is fine", "1201-1240", 1201, 1240, ""},
		{"exactly the maximum is allowed", "1-500", 1, 500, ""},
		{"one over the maximum is refused", "1-501", 0, 0, "at most 500"},
		{"a fat-fingered range is refused", "1-99999", 0, 0, "at most 500"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			from, to, err := ParseRoomRun(tc.input)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("ParseRoomRun(%q) = %d-%d, want an error containing %q", tc.input, from, to, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("ParseRoomRun(%q) error = %q, want it to contain %q", tc.input, err, tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("ParseRoomRun(%q) returned %v, want %d-%d", tc.input, err, tc.wantFrom, tc.wantTo)
			}
			if from != tc.wantFrom || to != tc.wantTo {
				t.Errorf("ParseRoomRun(%q) = %d-%d, want %d-%d", tc.input, from, to, tc.wantFrom, tc.wantTo)
			}
		})
	}
}

// A refused range must be identifiable as such, so the handler can say "that is
// 99999 rooms" rather than reporting a generic bad request.
func TestParseRoomRunFlagsTooLarge(t *testing.T) {
	_, _, err := ParseRoomRun("1-99999")
	if !errors.Is(err, ErrRoomRangeTooLarge) {
		t.Fatalf("ParseRoomRun(1-99999) error = %v, want it to wrap ErrRoomRangeTooLarge", err)
	}
	if !strings.Contains(err.Error(), "99999 rooms") {
		t.Errorf("error %q should say how many rooms were asked for", err)
	}
}

// A run's own idea of its length has to match its bounds, because it is what the
// Settings list shows next to the label.
func TestRoomCountAndLabel(t *testing.T) {
	tests := []struct {
		from, to  int
		wantCount int
		wantLabel string
	}{
		{1214, 1214, 1, "1214"},
		{1201, 1212, 12, "1201-1212"},
		{1201, 1240, 40, "1201-1240"},
		{1, 500, 500, "1-500"},
	}
	for _, tc := range tests {
		r := &Room{FromRoom: tc.from, ToRoom: tc.to}
		if got := r.Count(); got != tc.wantCount {
			t.Errorf("Room{%d,%d}.Count() = %d, want %d", tc.from, tc.to, got, tc.wantCount)
		}
		if got := r.Label(); got != tc.wantLabel {
			t.Errorf("Room{%d,%d}.Label() = %q, want %q", tc.from, tc.to, got, tc.wantLabel)
		}
	}
}

// Which parts of a requested range are missing is the whole of "ignore the rooms
// that already exist and carry on", so it is tested directly rather than only
// through a database.
//
// Every case below states what the inventory already holds and what the manager
// typed, because the interesting situations are all about the two disagreeing.
func TestUncoveredRooms(t *testing.T) {
	run := func(from, to int) *Room { return &Room{FromRoom: from, ToRoom: to} }

	tests := []struct {
		name     string
		from, to int
		existing []*Room
		want     [][2]int // each pair is one run that would be inserted
	}{
		{
			name: "nothing exists yet, so the whole range goes in",
			from: 1401, to: 1412,
			want: [][2]int{{1401, 1412}},
		},
		{
			// The case the change was made for: the range starts where an
			// existing run ends, so only the tail is new.
			name: "range extends past an existing run",
			from: 1401, to: 1420, existing: []*Room{run(1401, 1412)},
			want: [][2]int{{1413, 1420}},
		},
		{
			// The other direction: the range ends where an existing run starts.
			name: "range stops before an existing run",
			from: 1401, to: 1420, existing: []*Room{run(1415, 1420)},
			want: [][2]int{{1401, 1414}},
		},
		{
			// A single room already listed in the middle splits the range in two.
			name: "one room in the middle splits it in two",
			from: 1401, to: 1420, existing: []*Room{run(1410, 1410)},
			want: [][2]int{{1401, 1409}, {1411, 1420}},
		},
		{
			name: "run in the middle splits it in two",
			from: 1401, to: 1420, existing: []*Room{run(1405, 1410)},
			want: [][2]int{{1401, 1404}, {1411, 1420}},
		},
		{
			name: "several runs leave several gaps",
			from: 1401, to: 1420,
			existing: []*Room{run(1403, 1405), run(1409, 1409), run(1415, 1417)},
			want:     [][2]int{{1401, 1402}, {1406, 1408}, {1410, 1414}, {1418, 1420}},
		},
		{
			// Already fully covered: nothing to do, and it is not an error.
			name: "range is already fully covered",
			from: 1401, to: 1420, existing: []*Room{run(1401, 1420)},
			want: nil,
		},
		{
			name: "range is covered by several adjacent runs",
			from: 1401, to: 1420,
			existing: []*Room{run(1401, 1410), run(1411, 1415), run(1416, 1420)},
			want:     nil,
		},
		{
			name: "exact single room already exists",
			from: 1214, to: 1214, existing: []*Room{run(1214, 1214)},
			want: nil,
		},
		{
			// Everything already listed is outside the range, so none of it is
			// relevant and the range goes in whole.
			name: "existing runs are entirely outside the range",
			from: 1401, to: 1412,
			existing: []*Room{run(1201, 1212), run(1501, 1512)},
			want:     [][2]int{{1401, 1412}},
		},
		{
			// Touching without overlapping still leaves a gap the size of one room.
			name: "existing run abuts the start of the range",
			from: 1401, to: 1412, existing: []*Room{run(1400, 1400)},
			want: [][2]int{{1401, 1412}},
		},
		{
			name: "existing run abuts the end of the range",
			from: 1401, to: 1412, existing: []*Room{run(1413, 1413)},
			want: [][2]int{{1401, 1412}},
		},
		{
			// Order of the query must not matter. The caller gets this from an
			// ORDER BY, but the function sorts anyway so a future query that
			// forgets cannot produce gaps that overlap what is already stored.
			name: "existing runs given out of order",
			from: 1401, to: 1420,
			existing: []*Room{run(1415, 1417), run(1403, 1405), run(1409, 1409)},
			want:     [][2]int{{1401, 1402}, {1406, 1408}, {1410, 1414}, {1418, 1420}},
		},
		{
			// Defensive: reversed bounds are corrected rather than producing an
			// empty range that silently adds nothing.
			name: "reversed bounds are treated as the same range",
			from: 1412, to: 1401, existing: []*Room{run(1401, 1405)},
			want: [][2]int{{1406, 1412}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := uncoveredRooms(tc.from, tc.to, tc.existing)

			if len(got) != len(tc.want) {
				t.Fatalf("uncoveredRooms(%d, %d, %d existing) = %v, want %v",
					tc.from, tc.to, len(tc.existing), spans(got), tc.want)
			}
			for i, w := range tc.want {
				if got[i].from != w[0] || got[i].to != w[1] {
					t.Errorf("gap %d = %d-%d, want %d-%d", i, got[i].from, got[i].to, w[0], w[1])
				}
			}

			// The gaps must cover only rooms that were asked for and not already
			// held, and between them they must account for every added room.
			// These two properties are what make the result safe to insert.
			covered, overlapsExisting := checkSpans(t, tc.from, tc.to, tc.existing, got)
			if overlapsExisting {
				t.Errorf("a returned gap overlaps a run that already exists")
			}
			lo, hi := tc.from, tc.to
			if lo > hi {
				lo, hi = hi, lo
			}
			if covered > hi-lo+1 {
				t.Errorf("gaps cover %d rooms, more than the %d requested", covered, hi-lo+1)
			}
		})
	}
}

// checkSpans reports how many rooms the gaps cover, and whether any of them
// collides with a room an existing run already holds.
func checkSpans(t *testing.T, from, to int, existing []*Room, gaps []roomSpan) (covered int, collided bool) {
	t.Helper()
	// The bounds are normalised the same way uncoveredRooms normalises them, or
	// the "was not requested" check below would fire on every room of a reversed
	// range and blame the function for the helper's own arithmetic.
	if from > to {
		from, to = to, from
	}
	inside := func(n, lo, hi int) bool { return n >= lo && n <= hi }
	for _, g := range gaps {
		for n := g.from; n <= g.to; n++ {
			if !inside(n, from, to) {
				t.Errorf("gap %d-%d reaches room %d, which was not requested", g.from, g.to, n)
			}
			for _, r := range existing {
				if inside(n, r.FromRoom, r.ToRoom) {
					t.Errorf("gap %d-%d includes room %d, already held by %d-%d", g.from, g.to, n, r.FromRoom, r.ToRoom)
					collided = true
				}
			}
			covered++
		}
	}
	return covered, collided
}

// spans renders gaps readably in test failure output.
func spans(s []roomSpan) string {
	if len(s) == 0 {
		return "[]"
	}
	parts := make([]string, len(s))
	for i, g := range s {
		parts[i] = strconv.Itoa(g.from) + "-" + strconv.Itoa(g.to)
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// Expansion must produce exactly the rooms the bounds say, in order, and nothing
// more -- this is what generate_series does in the database, and the two have to
// agree or the dropdown and the count would disagree.
func TestRoomExpand(t *testing.T) {
	r := &Room{FromRoom: 1201, ToRoom: 1205}
	got := r.Expand()
	want := []int{1201, 1202, 1203, 1204, 1205}
	if len(got) != len(want) {
		t.Fatalf("Expand() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Expand()[%d] = %d, want %d", i, got[i], want[i])
		}
	}

	single := &Room{FromRoom: 1214, ToRoom: 1214}
	if got := single.Expand(); len(got) != 1 || got[0] != 1214 {
		t.Errorf("a single-room run should expand to itself, got %v", got)
	}
}

// A LIKE wildcard typed into the search box must search for that character, not
// turn the query into "match everything".
func TestEscapeLike(t *testing.T) {
	tests := []struct{ in, want string }{
		{"1214", "1214"},
		{"%", `\%`},
		{"_", `\_`},
		{`\`, `\\`},
		{"1%", `1\%`},
	}
	for _, tc := range tests {
		if got := escapeLike(tc.in); got != tc.want {
			t.Errorf("escapeLike(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The search pattern is what makes the room search match from the left. Written
// out as its own test because it is a one-line rule that is invisible once it is
// folded into a query string, and because the failure is silent: reverting it to
// a substring search does not error, it just starts offering the wrong rooms.
//
// A prefix pattern has exactly one wildcard, at the end, and it is ours.
func TestRoomSearchPatternIsPrefixOnly(t *testing.T) {
	tests := []struct {
		query string
		want  string
	}{
		// The prefix cases: these anchor the start of the number.
		{"14", "14%"},
		{"1426", "1426%"},
		{"1", "1%"},

		// Nothing leading. A leading % is exactly the bug this test exists for:
		// it makes "14" match 4114, which is not the room being looked for.
		{"14", "14%"},

		// An empty query matches nothing. "%" would match every room there is,
		// and this is the one place that could happen without a caller noticing.
		{"", ""},

		// Wildcards belonging to the user are escaped, so they cannot act as
		// wildcards -- and cannot reintroduce the substring match either.
		{"%", `\%` + "%"},
		{"_", `\_` + "%"},
		{"1_4", `1\_4%`},
		{"14%", `14\%%`},
		{`1\4`, `1\\4%`},
	}
	for _, tc := range tests {
		got := roomSearchPattern(tc.query)
		if got != tc.want {
			t.Errorf("roomSearchPattern(%q) = %q, want %q", tc.query, got, tc.want)
		}
		if strings.HasPrefix(got, "%") {
			t.Errorf("roomSearchPattern(%q) = %q: a pattern starting with %% matches anywhere in the number, not from the start", tc.query, got)
		}
	}
}
