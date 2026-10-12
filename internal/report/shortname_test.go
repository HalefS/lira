package report

import (
	"testing"

	"github.com/HalefS/lira/internal/data"
)

// The rule is "name and last surname": the FIRST word and the LAST word, and
// nothing between them.
//
// The second case is the one that matters, and the one a first-and-second
// implementation gets wrong. "João Silva Santos" is not "João Silva" — the middle
// word is not a surname, it is whatever happens to sit there.
func TestShortNameTakesFirstAndLast(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"two words are already the answer", "Halef Spencer", "Halef Spencer"},
		{"three words drop the middle", "João Silva Santos", "João Santos"},
		{"four words drop both middles", "Ana Maria Costa Silva", "Ana Silva"},
		{"a single word has no surname to keep", "lira", "lira"},
		{"a single word is NOT doubled", "Cher", "Cher"},
		{"leading and trailing space is not a word", "  Halef Spencer  ", "Halef Spencer"},
		{"runs of internal whitespace collapse", "João   Silva  Santos", "João Santos"},
		{"a tab is still a separator", "João\tSilva\tSantos", "João Santos"},
		{"a newline is still a separator", "João\nSilva\nSantos", "João Santos"},
		{"empty stays empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shortName(tt.in); got != tt.want {
				t.Errorf("shortName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// Idempotent: shortening something already shortened must not shorten it again, or
// a name would be mangled every time it passed through a second layer -- and a
// two-word short name shortened twice is harmless only by luck of the length check,
// which is exactly the kind of luck that stops holding when somebody adds a case.
func TestShortNameIsIdempotent(t *testing.T) {
	for _, in := range []string{
		"Halef Spencer", "João Silva Santos", "Ana Maria Costa Silva", "lira", "Cher",
	} {
		once := shortName(in)
		if twice := shortName(once); twice != once {
			t.Errorf("shortName(shortName(%q)) = %q, want %q -- not idempotent", in, twice, once)
		}
	}
}

// Shortening is a DISPLAY concern and must never touch what is stored.
//
// This matters because two people can legitimately share a short name -- a father
// and a son, both "João Santos" -- so nothing may key on the shortened form. The
// rota renders the full name and the attendance workbook prints it, which is the
// only reason a shortened view is safe to show anywhere at all.
func TestShorteningDoesNotTouchTheStoredName(t *testing.T) {
	const full = "Ana Maria Costa Silva"
	m := &data.ScheduleMember{UserID: 7, Name: full}

	if shortName(m.Name) == m.Name {
		t.Fatal("shortName did not shorten this name, so the test is not exercising anything")
	}
	if m.Name != full {
		t.Errorf("the stored name changed to %q; shortening must be a display concern", m.Name)
	}
}

// The initials beside a shortened name must come from the SHORTENED name.
//
// Taking them from the full name puts "JS" over a label reading "João Santos", and
// "AM" over one reading "Ana Silva" -- the reader has to work out which letters the
// letters are. The avatar and the printed name have to agree.
func TestInitialsFollowTheShortenedName(t *testing.T) {
	tests := []struct {
		full string
		want string
	}{
		{"Halef Spencer", "HS"},
		{"Ana Maria Costa Silva", "AS"}, // shortened first: Ana, Silva
		{"João Silva Santos", "JS"},     // same either way, so this one cannot fail
		{"Ana-Maria Costa", "AC"},       // a hyphen is inside a word, not a separator
		{"lira", "L"},                   // single word, one letter
	}
	for _, tt := range tests {
		t.Run(tt.full, func(t *testing.T) {
			if got := initials(shortName(tt.full)); got != tt.want {
				t.Errorf("initials(shortName(%q)) = %q, want %q", tt.full, got, tt.want)
			}
		})
	}
}

// The helper splits on WHITESPACE AND NOTHING ELSE, which is stated here because
// it is a decision rather than an oversight.
//
// "Ana & Sons <Ltd>" is a business that could be registered as an account, and it
// shortens to "Ana <Ltd>" with the initials "A<". A filter for "words made of
// letters" would fix that and would also mangle real names: it would drop the
// particle in "João da Silva", turn "O'Brien" into something else, and turn
// "Ana-Maria" into two words and pick the wrong pair.
//
// A person's name is not a company name, and every real case in the catalogue is a
// person's. Guessing at the shape of an account that might exist is a worse trade
// than being naive about punctuation that almost certainly does not.
func TestShortNameSplitsOnWhitespaceOnly(t *testing.T) {
	// The particle goes, because it sits in the middle and the rule is first+last.
	if got := shortName("João da Silva"); got != "João Silva" {
		t.Errorf("shortName(%q) = %q, want %q", "João da Silva", got, "João Silva")
	}
	// Punctuation inside a word survives, because it is not a separator.
	if got := shortName("Ana-Maria Costa Silva"); got != "Ana-Maria Silva" {
		t.Errorf("shortName(%q) = %q, want %q", "Ana-Maria Costa Silva", got, "Ana-Maria Silva")
	}
	// And the naive behaviour is pinned rather than left to be discovered: no
	// letter-filtering, so a trailing bracket stays attached to the surname.
	if got := shortName("Ana & Sons <Ltd>"); got != "Ana <Ltd>" {
		t.Errorf("shortName(%q) = %q, want %q -- if this changes, something now filters",
			"Ana & Sons <Ltd>", got, "Ana <Ltd>")
	}
}

// THE DAILY REPORT SHORTENS.
//
// Asserted through the renderer rather than through the helper, because the risk
// is not that shortName is wrong -- it is that somebody stops calling it here, and
// a test of the helper alone would keep passing straight through that.
func TestDailyReportShortensTechnicianNames(t *testing.T) {
	const full = "Ana Maria Costa Silva"
	bars := techBars(&data.DailyReport{
		ByTechnician: []data.TechStat{{UserID: 7, Name: full, AvatarIdx: 0, Count: 3}},
	})

	if len(bars) != 1 {
		t.Fatalf("expected one technician bar, got %d: %+v", len(bars), bars)
	}
	if want := shortName(full); bars[0].Name != want {
		t.Errorf("daily bar name = %q, want %q", bars[0].Name, want)
	}
	if bars[0].Initials != initials(bars[0].Name) {
		t.Errorf("daily bar initials = %q, want initials(%q) = %q",
			bars[0].Initials, bars[0].Name, initials(bars[0].Name))
	}
}

// THE CONSUMABLES REPORT SHORTENS, for the same reason and by the same function.
func TestConsumablesReportShortensTechnicianNames(t *testing.T) {
	const full = "Ana Maria Costa Silva"
	id := int64(7)
	v := newConsumablesView(&data.ConsumablesWeeklyReport{
		ByTechnician: []*data.ConsumableTechTotal{
			{UserID: &id, Name: full, AvatarIdx: 0, Quantity: 9, Records: 3},
		},
	})

	if len(v.Techs) != 1 {
		t.Fatalf("expected one technician row, got %d: %+v", len(v.Techs), v.Techs)
	}
	if want := shortName(full); v.Techs[0].Name != want {
		t.Errorf("consumables row name = %q, want %q", v.Techs[0].Name, want)
	}
}

// THE ATTENDANCE WORKBOOK KEEPS THE WHOLE NAME.
//
// The one export that does not shorten, and the exception the rest of the rule
// exists to make room for: a signed statement about who worked, laid out as the
// rota's own grid.
//
// Again asserted through the renderer, because attendance deliberately does not
// CALL shortName. The danger is not a broken helper; it is somebody wiring it in
// here one day without noticing.
func TestAttendanceWorkbookKeepsTheFullName(t *testing.T) {
	const full = "Ana Maria Costa Silva"
	v := newAttendanceView(&data.ScheduleWeek{
		Week: attWeek("2026-10-05"),
		Members: []*data.ScheduleMember{
			{UserID: 7, Name: full, Role: "technician", Active: true, Days: [7]*data.Shift{}},
		},
	})

	if len(v.Rows) != 1 {
		t.Fatalf("expected one attendance row, got %d: %+v", len(v.Rows), v.Rows)
	}
	row := v.Rows[0]
	if row.Name != full {
		t.Errorf("attendance row name = %q, want the full %q -- this workbook is a signed"+
			" statement about who worked and must not shorten", row.Name, full)
	}
	// Initials come from the FULL name here: "AM" for four words, where the screen
	// shows "AS". Intended -- the workbook is a different document with its own
	// convention, and changing it would change a sheet somebody signs.
	if row.Initials != initials(full) {
		t.Errorf("attendance initials = %q, want initials(full name) = %q",
			row.Initials, initials(full))
	}
}
