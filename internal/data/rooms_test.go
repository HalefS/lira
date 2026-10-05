package data

import (
	"errors"
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

// The overlap message names the run in the way, because "rooms 1201-1210 are
// already in the inventory" is something a manager can act on and "conflict" is
// not.
func TestRoomOverlapMessageNamesTheRun(t *testing.T) {
	err := &RoomOverlap{Existing: &Room{FromRoom: 1201, ToRoom: 1210}}
	if !errors.Is(err, ErrRoomOverlap) {
		t.Errorf("RoomOverlap should unwrap to ErrRoomOverlap")
	}
	for _, want := range []string{"1201-1210", "already in the inventory"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("overlap message %q should contain %q", err, want)
		}
	}
	// An overlap with nothing known about it still has to be reportable, so a
	// nil Existing cannot panic the error path.
	bare := &RoomOverlap{}
	if bare.Error() == "" {
		t.Error("a RoomOverlap with no existing run should still say something")
	}
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
