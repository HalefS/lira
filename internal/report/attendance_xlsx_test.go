package report

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/HalefS/lira/internal/data"
)

// ── decoding helpers ──────────────────────────────────────────────────────────
//
// The writer is hand-rolled XML editing, so these tests do not trust it: they
// decode the produced workbook the way a spreadsheet application would -- resolving
// every shared-string index through the pool -- and assert on what a reader would
// actually see. A workbook that is structurally valid but whose indices point at
// the wrong strings still opens, and still looks complete.

type xlsxDoc struct {
	parts map[string]string
	order []string
}

func decodeXLSX(t *testing.T, b []byte) xlsxDoc {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("the result is not a zip: %v", err)
	}
	d := xlsxDoc{parts: map[string]string{}}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("opening %s: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("reading %s: %v", f.Name, err)
		}
		d.order = append(d.order, f.Name)
		if strings.HasSuffix(f.Name, ".xml") || strings.HasSuffix(f.Name, ".rels") {
			d.parts[f.Name] = string(data)
			var probe any
			if err := xml.Unmarshal(data, &probe); err != nil {
				t.Errorf("%s is not well-formed XML: %v", f.Name, err)
			}
		}
	}
	return d
}

// pool returns the shared string table, unescaped, indexed as the sheets index it.
func (d xlsxDoc) pool(t *testing.T) []string {
	t.Helper()
	sst, ok := d.parts[xlsxStrings]
	if !ok {
		t.Fatal("the workbook has no shared string table")
	}
	var out []string
	for _, si := range reSiBody.FindAllStringSubmatch(sst, -1) {
		var b strings.Builder
		for _, m := range reT.FindAllStringSubmatch(si[1], -1) {
			b.WriteString(m[1])
		}
		out = append(out, unescapeXML(b.String()))
	}
	return out
}

// cell resolves one cell to the text a reader would see: the shared string for a
// string cell, the literal number for a numeric one, empty for a blank.
func (d xlsxDoc) cell(t *testing.T, sheet, ref string) string {
	t.Helper()
	x, ok := d.parts[sheet]
	if !ok {
		t.Fatalf("no %s in the workbook", sheet)
	}
	re := regexp.MustCompile(`<c r="` + ref + `"(?:\s[^>]*)?/>|<c r="` + ref + `"(?:\s[^>]*)?>.*?</c>`)
	m := re.FindString(x)
	if m == "" {
		return ""
	}
	v := regexp.MustCompile(`<v>(.*?)</v>`).FindStringSubmatch(m)
	if v == nil {
		return ""
	}
	if strings.Contains(m, `t="s"`) {
		p := d.pool(t)
		i := 0
		for _, ch := range v[1] {
			i = i*10 + int(ch-'0')
		}
		if i >= len(p) {
			t.Fatalf("%s%s points at shared string %d but the pool holds %d", sheet, ref, i, len(p))
		}
		return p[i]
	}
	return v[1]
}

var reRowNum = regexp.MustCompile(`<row r="(\d+)"`)

func (d xlsxDoc) rows(t *testing.T, sheet string) []int {
	t.Helper()
	var out []int
	for _, m := range reRowNum.FindAllStringSubmatch(d.parts[sheet], -1) {
		n := 0
		for _, ch := range m[1] {
			n = n*10 + int(ch-'0')
		}
		out = append(out, n)
	}
	return out
}

func (d xlsxDoc) merges(t *testing.T, sheet string) []string {
	t.Helper()
	var out []string
	for _, m := range reMerge.FindAllStringSubmatch(d.parts[sheet], -1) {
		out = append(out, m[0])
	}
	return out
}

// ── fixtures ──────────────────────────────────────────────────────────────────

func sampleWeekXLSX() *data.ScheduleWeek {
	halef := &data.ScheduleMember{UserID: 1, Name: "Halef Spencer", Role: "manager", Active: true}
	halef.Days = [7]*data.Shift{attNight("22:00", "06:00"), nil, nil, nil, nil, nil, nil}

	fabio := &data.ScheduleMember{UserID: 2, Name: "Fabio Garcia", Role: "technician", Active: true}
	fabio.Days = [7]*data.Shift{nil, nil, attShift("16:00", "00:00", false), nil, nil, nil, nil}

	lira := &data.ScheduleMember{UserID: 3, Name: "lira", Role: "manager", Active: true}

	return &data.ScheduleWeek{
		Week: attWeek("2026-10-05"),
		Members: []*data.ScheduleMember{
			halef, fabio, lira,
		},
		Absences: []*data.AbsenceRef{
			attAbsence(3, data.AbsenceVacation, "2026-10-05", "2026-10-11"),
		},
	}
}

func renderXLSX(t *testing.T, w *data.ScheduleWeek) ([]byte, xlsxDoc) {
	t.Helper()
	b, err := RenderAttendanceXLSX(w)
	if err != nil {
		t.Fatalf("RenderAttendanceXLSX: %v", err)
	}
	return b, decodeXLSX(t, b)
}

var colPair = [7][2]string{
	{"C", "D"}, {"E", "F"}, {"G", "H"}, {"I", "J"},
	{"K", "L"}, {"M", "N"}, {"O", "P"},
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestRenderAttendanceXLSXRejectsNil(t *testing.T) {
	if _, err := RenderAttendanceXLSX(nil); err == nil {
		t.Error("a nil week must be an error, not an empty workbook")
	}
	if _, err := RenderAttendanceXLSX(&data.ScheduleWeek{}); err == nil {
		t.Error("a week with no Week header must be an error")
	}
}

// The reason the template is a template. A person name compiled into the binary is
// a person name in the repository, in every build artefact and in anybody with a
// copy of the source.
func TestAttendanceTemplateCarriesNoPersonalData(t *testing.T) {
	if len(attendanceTemplateXLSX) == 0 {
		t.Fatal("the attendance template is not embedded")
	}
	d := decodeXLSX(t, attendanceTemplateXLSX)
	blob := strings.Join(d.pool(t), " | ")
	for _, name := range []string{
		"Fredfon", "Ronaldo", "Eder ony", "Halef Silva Lopes",
		"Halef Spencer", "Fabio Garcia", "UI Review", "Sched Technician", "Rota Tech",
	} {
		if strings.Contains(blob, name) {
			t.Errorf("the embedded template contains %q", name)
		}
	}
	// And no dates or times either, so a shipped workbook cannot imply a rota.
	for _, v := range []string{"09:00", "18:00", "13:00", "22:00", "Outubro", "Janeiro"} {
		if strings.Contains(blob, v) {
			t.Errorf("the embedded template contains the value %q", v)
		}
	}
	// What it must keep.
	for _, label := range []string{"Folha de Ponto/Turnos", "Nome", "Observação", "Folga", "Feria", "Falta"} {
		if !strings.Contains(blob, label) {
			t.Errorf("the embedded template is missing %q", label)
		}
	}
}

// The workbook identity must survive untouched: these are the parts a user would
// notice losing, and none of them is a value this code is allowed to change.
func TestAttendanceXLSXPreservesWorkbookStructure(t *testing.T) {
	tpl := decodeXLSX(t, attendanceTemplateXLSX)
	_, got := renderXLSX(t, sampleWeekXLSX())

	for _, part := range []string{
		"xl/styles.xml", "xl/theme/theme1.xml", "[Content_Types].xml",
		"xl/_rels/workbook.xml.rels", "xl/worksheets/sheet2.xml",
		"xl/worksheets/sheet3.xml", "xl/worksheets/sheet4.xml",
		"xl/printerSettings/printerSettings1.bin",
	} {
		if tpl.parts[part] != got.parts[part] {
			t.Errorf("%s was modified; only sheet1 and sharedStrings may change", part)
		}
	}
	if strings.Join(tpl.order, ",") != strings.Join(got.order, ",") {
		t.Error("the zip entry list or its order changed")
	}
}

func TestAttendanceXLSXRowNumberingIsSound(t *testing.T) {
	for _, n := range []int{0, 1, 3, 4, 5, 6, 12, 20} {
		w := &data.ScheduleWeek{Week: attWeek("2026-10-05")}
		for i := 0; i < n; i++ {
			m := &data.ScheduleMember{UserID: int64(i + 1), Name: "Pessoa Exemplo", Active: true}
			m.Days = [7]*data.Shift{attShift("08:00", "16:00", false), nil, nil, nil, nil, nil, nil}
			w.Members = append(w.Members, m)
		}
		_, d := renderXLSX(t, w)

		rows := d.rows(t, xlsxSheet)
		seen := map[int]bool{}
		last := 0
		for _, r := range rows {
			if seen[r] {
				t.Errorf("%d members: duplicate row %d", n, r)
			}
			seen[r] = true
			if r <= last {
				t.Errorf("%d members: rows out of order at %d", n, r)
			}
			last = r
		}
		for i := 1; i <= last; i++ {
			if !seen[i] {
				t.Errorf("%d members: row %d is missing, so the sheet has a hole", n, i)
			}
		}
		// Rows 1-10 are the header block, members start at 11, then the
		// signature row. So the last row is 11+n.
		want := 11 + n
		if last != want {
			t.Errorf("%d members: last row %d, want %d", n, last, want)
		}
		// The dimension must agree, or Excel truncates silently.
		if !strings.Contains(d.parts[xlsxSheet], `<dimension ref="A1:Q`+itoa(last)+`"/>`) {
			t.Errorf("%d members: dimension does not end at row %d", n, last)
		}
	}
}

// itoa lives in daily.go for this package; these tests reuse it rather than
// declaring a second one with the same name.

// Every merge must point at cells that exist, and there must be no duplicate refs.
func TestAttendanceXLSXMergesAreSound(t *testing.T) {
	for _, n := range []int{1, 4, 6, 15} {
		w := &data.ScheduleWeek{Week: attWeek("2026-10-05")}
		for i := 0; i < n; i++ {
			m := &data.ScheduleMember{UserID: int64(i + 1), Name: "Pessoa Exemplo", Active: true}
			// Worked Monday and Tuesday, away all week otherwise: exercises both a
			// split pair and a merged one in the same row.
			m.Days = [7]*data.Shift{
				attShift("08:00", "16:00", false), attShift("09:00", "17:00", false),
				nil, nil, nil, nil, nil,
			}
			w.Members = append(w.Members, m)
		}
		_, d := renderXLSX(t, w)

		seen := map[string]bool{}
		for _, m := range d.merges(t, xlsxSheet) {
			if seen[m] {
				t.Errorf("%d members: duplicate merge %s", n, m)
			}
			seen[m] = true
		}
		// Every merge endpoint must be a cell the sheet actually has.
		x := d.parts[xlsxSheet]
		for _, m := range d.merges(t, xlsxSheet) {
			g := reMerge.FindStringSubmatch(m)
			for _, ref := range [][2]string{{g[1], g[2]}, {g[3], g[4]}} {
				if !strings.Contains(x, `<c r="`+ref[0]+ref[1]+`"`) {
					t.Errorf("%d members: merge %s points at a cell that is not there", n, m)
				}
			}
		}
		cnt := regexp.MustCompile(`<mergeCells count="(\d+)"`).FindStringSubmatch(x)
		if cnt == nil || itoa(len(d.merges(t, xlsxSheet))) != cnt[1] {
			t.Errorf("%d members: mergeCells count disagrees with the refs (%v vs %d)",
				n, cnt, len(d.merges(t, xlsxSheet)))
		}
	}
}

// A worked day is two times under the Entrada and Saida headings. If the pair were
// left merged, the Saida would disappear with nothing to say so -- and this is the
// single most destructive thing this writer could do silently.
func TestAttendanceXLSXWorkedDayHasBothTimes(t *testing.T) {
	_, d := renderXLSX(t, sampleWeekXLSX())

	if got := d.cell(t, xlsxSheet, "C11"); got != "22:00" {
		t.Errorf("Monday entrada = %q, want 22:00", got)
	}
	if got := d.cell(t, xlsxSheet, "D11"); got != "06:00" {
		t.Errorf("Monday saida = %q, want 06:00", got)
	}
	if got := d.cell(t, xlsxSheet, "G12"); got != "16:00" {
		t.Errorf("Wednesday entrada = %q, want 16:00", got)
	}
	if got := d.cell(t, xlsxSheet, "H12"); got != "00:00" {
		t.Errorf("Wednesday saida = %q, want 00:00", got)
	}
	// And neither of those pairs may be merged.
	for _, pair := range []string{"C11:D11", "G12:H12"} {
		if strings.Contains(d.parts[xlsxSheet], `<mergeCell ref="`+pair+`"/>`) {
			t.Errorf("%s holds two times but is merged, so one of them is lost", pair)
		}
	}
}

// A day with nothing on it is a word across the pair. The word is what the cell
// must resolve to -- not a blank, and not the template leftover.
func TestAttendanceXLSXWordDayMergesAndResolves(t *testing.T) {
	_, d := renderXLSX(t, sampleWeekXLSX())

	// lira is away all week, so every one of her seven pairs is a word.
	for i, p := range colPair {
		if got := d.cell(t, xlsxSheet, p[0]+"13"); got != "Feria" {
			t.Errorf("lira day %d = %q, want Feria", i+1, got)
		}
		if got := d.cell(t, xlsxSheet, p[1]+"13"); got != "" {
			t.Errorf("lira day %d saida = %q, want empty behind a merged word", i+1, got)
		}
		if !strings.Contains(d.parts[xlsxSheet], `<mergeCell ref="`+p[0]+`13:`+p[1]+`13"/>`) {
			t.Errorf("lira day %d is not merged", i+1)
		}
	}
}

// The rule the whole feature rests on, read back out of the file rather than out
// of the view that produced it.
func TestAttendanceXLSXAbsenceBeatsShift(t *testing.T) {
	m := &data.ScheduleMember{UserID: 1, Name: "Ana", Role: "technician", Active: true}
	m.Days = [7]*data.Shift{
		attShift("08:00", "16:00", false), attShift("09:00", "17:00", false),
		nil, nil, nil, nil, nil,
	}
	_, d := renderXLSX(t, &data.ScheduleWeek{
		Week:     attWeek("2026-10-05"),
		Members:  []*data.ScheduleMember{m},
		Absences: []*data.AbsenceRef{attAbsence(1, data.AbsenceSick, "2026-10-05", "2026-10-05")},
	})

	if got := d.cell(t, xlsxSheet, "C11"); got != "Falta" {
		t.Errorf("the covered Monday resolved to %q, want Falta", got)
	}
	if got := d.cell(t, xlsxSheet, "D11"); got != "" {
		t.Errorf("the covered Monday still has a saida: %q", got)
	}
	// Precedence is not deletion: the uncovered Tuesday keeps its rota.
	if got := d.cell(t, xlsxSheet, "E11"); got != "09:00" {
		t.Errorf("the uncovered Tuesday resolved to %q, want 09:00", got)
	}
	if got := d.cell(t, xlsxSheet, "F11"); got != "17:00" {
		t.Errorf("the uncovered Tuesday saida resolved to %q, want 17:00", got)
	}
}

func TestAttendanceXLSXHeaderFields(t *testing.T) {
	_, d := renderXLSX(t, sampleWeekXLSX())

	if got := d.cell(t, xlsxSheet, "C4"); got != "Outubro 2026" {
		t.Errorf("Mes = %q", got)
	}
	if got := d.cell(t, xlsxSheet, "C5"); got != "05-10 a 11-10" {
		t.Errorf("Semana = %q", got)
	}
	// Data Entrega is the Monday of the week being printed, as the sheet asks for.
	if got := d.cell(t, xlsxSheet, "C6"); got != "46300" {
		t.Errorf("Data Entrega serial = %q, want 46300 (2026-10-05)", got)
	}
	for i, p := range colPair {
		want := itoa(46300 + i)
		if got := d.cell(t, xlsxSheet, p[0]+"9"); got != want {
			t.Errorf("date column %d = %q, want %q", i+1, got, want)
		}
	}
	// The dates must still be DATES, rendered dd-mm-yy -- not text.
	if !strings.Contains(d.parts["xl/styles.xml"], `formatCode="dd-mm-yy"`) {
		t.Error("the dd-mm-yy number format is gone from the workbook")
	}
}

// Member names come from users.name, which is user-supplied, and they are written
// into the string pool rather than passed through html/template.
func TestAttendanceXLSXEscapesNames(t *testing.T) {
	m := &data.ScheduleMember{UserID: 1, Name: `Ana & Sons <script>"x"`, Active: true}
	_, d := renderXLSX(t, &data.ScheduleWeek{
		Week:    attWeek("2026-10-05"),
		Members: []*data.ScheduleMember{m},
	})
	if got := d.cell(t, xlsxSheet, "B11"); got != `Ana & Sons <script>"x"` {
		t.Errorf("the name round-tripped as %q; escaping is lossy or missing", got)
	}
	raw := d.parts[xlsxStrings]
	if strings.Contains(raw, "<script>") {
		t.Error("a raw tag reached the shared string table")
	}
	if !strings.Contains(raw, "&amp;") || !strings.Contains(raw, "&lt;") {
		t.Error("the ampersand and angle bracket were not escaped")
	}
}

// The shared-string counters are what stops Excel offering to repair the file.
func TestAttendanceXLSXSharedStringBookkeeping(t *testing.T) {
	for _, n := range []int{1, 4, 9} {
		w := &data.ScheduleWeek{Week: attWeek("2026-10-05")}
		for i := 0; i < n; i++ {
			m := &data.ScheduleMember{UserID: int64(i + 1), Name: "Pessoa Exemplo", Active: true}
			m.Days = [7]*data.Shift{attShift("08:00", "16:00", false), nil, nil, nil, nil, nil, nil}
			w.Members = append(w.Members, m)
		}
		_, d := renderXLSX(t, w)

		pool := d.pool(t)
		uc := regexp.MustCompile(`uniqueCount="(\d+)"`).FindStringSubmatch(d.parts[xlsxStrings])
		if uc == nil || itoa(len(pool)) != uc[1] {
			t.Errorf("%d members: uniqueCount=%v but the pool holds %d", n, uc, len(pool))
		}
		total := 0
		for _, name := range d.order {
			if strings.HasPrefix(name, "xl/worksheets/sheet") && strings.HasSuffix(name, ".xml") {
				total += len(reString.FindAllString(d.parts[name], -1))
			}
		}
		c := regexp.MustCompile(`\scount="(\d+)"`).FindStringSubmatch(d.parts[xlsxStrings])
		if c == nil || itoa(total) != c[1] {
			t.Errorf("%d members: count=%v but %d string cells exist", n, c, total)
		}
	}
}

// The writer starts from the pristine embedded template on every call, so filling
// the same week twice must produce the same bytes. State leaking between calls
// would show up here as a workbook that grows a row each time it is downloaded.
func TestAttendanceXLSXIsRepeatable(t *testing.T) {
	w := sampleWeekXLSX()
	a, err := RenderAttendanceXLSX(w)
	if err != nil {
		t.Fatalf("first render: %v", err)
	}
	b, err := RenderAttendanceXLSX(w)
	if err != nil {
		t.Fatalf("second render: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("two renders of the same week differ (%d vs %d bytes)", len(a), len(b))
	}
	// And shrinking afterwards must shrink back, not accumulate.
	small := &data.ScheduleWeek{Week: attWeek("2026-10-05")}
	m := &data.ScheduleMember{UserID: 1, Name: "Só Uma Pessoa", Active: true}
	small.Members = []*data.ScheduleMember{m}
	if _, d := renderXLSX(t, small); len(d.rows(t, xlsxSheet)) != 12 {
		t.Errorf("a one-member week left %d rows; the block did not shrink back", len(d.rows(t, xlsxSheet)))
	}
}

func TestAttendanceXLSXNamesAppearInTheRightRows(t *testing.T) {
	_, d := renderXLSX(t, sampleWeekXLSX())
	want := []string{"Halef Spencer", "Fabio Garcia", "lira"}
	for i, name := range want {
		if got := d.cell(t, xlsxSheet, "B"+itoa(11+i)); got != name {
			t.Errorf("row %d name = %q, want %q", 11+i, got, name)
		}
	}
	// The signature must still be below the block and still say what it said.
	last := d.rows(t, xlsxSheet)[len(d.rows(t, xlsxSheet))-1]
	if got := d.cell(t, xlsxSheet, "P"+itoa(last)); !strings.Contains(got, "Assinatura") {
		t.Errorf("the signature line resolved to %q at row %d", got, last)
	}
}
