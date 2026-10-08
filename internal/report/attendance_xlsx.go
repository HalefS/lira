package report

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/HalefS/lira/internal/data"
)

// The attendance sheet as a SPREADSHEET.
//
// The PDF and this share one view -- newAttendanceView -- and therefore one
// implementation of the rules that matter: an absence beats the recurring shift for
// a covered day, a day with nothing on it is Folga and never blank, hours exclude
// absent days, and "away all week" is a different fact from "not on this rota". A
// second formatter over the same view could disagree with the first about all of
// those, and the disagreement would be invisible: both documents would still open,
// still look complete, and still say something slightly different.
//
// The template is embedded, so there is no file to deploy, no path to configure and
// nothing to go missing. It carries the workbook's identity -- styles, merges,
// column widths, row heights, printer settings, the dd-mm-yy date format and the
// Portuguese captions -- and no data at all: every person name was stripped when it
// was built, so no roster of anybody's is compiled into the binary.
//
// Editing cell VALUES in place is the whole technique. An xlsx is a zip of XML, so
// only the sheet and the shared-string pool change; every other part is copied byte
// for byte. Round-tripping the workbook through a library would rewrite the styles
// and could change the appearance of cells nobody asked to change.

// attendanceTemplateXLSX is the attendance workbook template, embedded whole.
//
// It is a TEMPLATE, not a document: every date, time and person name was stripped
// when it was built, so no roster of anybody is compiled into the binary and no
// file on disk can drift from the rota. What survives is the workbook identity --
// styles, merges, column widths, row heights, printer settings, the dd-mm-yy date
// format and the Portuguese captions -- and that is all that is needed, because the
// values are written per request.
//
// Read-only, and deliberately so. There is no "save the filled workbook back"
// endpoint and no way to add one: the rota is the database, and a file on disk that
// looks authoritative while disagreeing with it is worse than no file at all.
var attendanceTemplateXLSX []byte

func init() {
	b, err := templateFS.ReadFile("templates/attendance.xlsx")
	if err != nil {
		// A missing template is a build problem, not a runtime condition. Left to be
		// discovered per request it would surface as an empty spreadsheet, which is
		// the one failure mode that looks like an answer.
		panic(fmt.Sprintf("report: reading the attendance template: %v", err))
	}
	attendanceTemplateXLSX = b
}

const (
	xlsxFirstMemberRow = 11
	// The seven weekday pairs, Monday first: C/D, E/F, G/H, I/J, K/L, M/N, O/P.
	xlsxDayCols = "CDEFGHIJKLMNOP"
	xlsxSheet   = "xl/worksheets/sheet1.xml"
	xlsxStrings = "xl/sharedStrings.xml"
	xlsxStyles  = "xl/styles.xml"

	// How the signature row is found. Matching the caption rather than a row number
	// means a re-laid-out template cannot make this write over the signature line.
	xlsxSignature = "Assinatura do Respons"
)

// RenderAttendanceXLSX fills the embedded template with the rota for one week and
// returns the workbook. A nil week, or one with no Week header, is an error rather
// than a blank workbook: a file that opens and shows nothing is worse than a
// refusal, because it looks like an answer.
func RenderAttendanceXLSX(w *data.ScheduleWeek) ([]byte, error) {
	if w == nil {
		return nil, fmt.Errorf("report: nil schedule week")
	}
	if w.Week == nil {
		return nil, fmt.Errorf("report: schedule week has no week header")
	}

	zr, err := zip.NewReader(bytes.NewReader(attendanceTemplateXLSX), int64(len(attendanceTemplateXLSX)))
	if err != nil {
		return nil, fmt.Errorf("report: opening the attendance template: %w", err)
	}
	parts := make([]zipPart, 0, len(zr.File))
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("report: reading %s from the template: %w", f.Name, err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("report: reading %s from the template: %w", f.Name, err)
		}
		parts = append(parts, zipPart{name: f.Name, method: f.Method, data: b})
	}

	x := &xlsxWriter{parts: parts, pool: map[string]int{}}
	if err := x.load(); err != nil {
		return nil, err
	}
	if err := x.fill(newAttendanceView(w), w.Week.Start.Time(), w.Week.End.Time()); err != nil {
		return nil, err
	}
	return x.write()
}

// zipPart is one entry, kept in the order the template had it.
type zipPart struct {
	name   string
	method uint16
	data   []byte
}

type xlsxWriter struct {
	parts []zipPart
	sheet string // sheet1 XML
	sst   string // the shared string table
	pool  map[string]int
	next  int // the index the next appended string will take
}

var (
	reRow       = regexp.MustCompile(`<row r="(\d+)"`)
	reRef       = regexp.MustCompile(`<c r="([A-Z]+)(\d+)"`)
	reMerge     = regexp.MustCompile(`<mergeCell ref="([A-Z]+)(\d+):([A-Z]+)(\d+)"/>`)
	reString    = regexp.MustCompile(`t="s"[^>]*><v>(\d+)</v>`)
	reSi        = regexp.MustCompile(`(?s)<si>.*?</si>`)
	reSiBody    = regexp.MustCompile(`(?s)<si>(.*?)</si>`)
	reT         = regexp.MustCompile(`(?s)<t[^>]*>(.*?)</t>`)
	reMergesBlk = regexp.MustCompile(`(?s)<mergeCells count="\d+">.*?</mergeCells>`)
	reDim       = regexp.MustCompile(`<dimension ref="[^"]+"/>`)
	reStyle     = regexp.MustCompile(`s="(\d+)"`)
	reXf        = regexp.MustCompile(`<xf [^>]*/>|<xf [^>]*>.*?</xf>`)
	reNumFmt    = regexp.MustCompile(`numFmtId="(\d+)"`)
)

func (x *xlsxWriter) cellRe(ref string) *regexp.Regexp {
	return regexp.MustCompile(`<c r="` + ref + `"(?:\s[^>]*)?/>|<c r="` + ref + `"(?:\s[^>]*)?>.*?</c>`)
}

func (x *xlsxWriter) load() error {
	var sheetPart, poolPart *zipPart
	for i := range x.parts {
		switch x.parts[i].name {
		case xlsxSheet:
			sheetPart = &x.parts[i]
		case xlsxStrings:
			poolPart = &x.parts[i]
		}
	}
	if sheetPart == nil || poolPart == nil {
		return fmt.Errorf("report: the attendance template lacks %s or %s", xlsxSheet, xlsxStrings)
	}
	x.sheet = string(sheetPart.data)
	x.sst = string(poolPart.data)

	// Index the pool by text so the template's own vocabulary -- the four status
	// words -- is reused rather than duplicated.
	for i, si := range reSiBody.FindAllStringSubmatch(x.sst, -1) {
		t := x.textOf(si[1])
		if _, seen := x.pool[t]; !seen {
			x.pool[t] = i
		}
	}
	x.next = len(reSi.FindAllString(x.sst, -1))
	return nil
}

func (x *xlsxWriter) textOf(si string) string {
	var b strings.Builder
	for _, m := range reT.FindAllStringSubmatch(si, -1) {
		b.WriteString(m[1])
	}
	return unescapeXML(b.String())
}

// sstIdx returns the pool index for text, appending it if the pool has never seen
// it. Appending rather than renumbering is what keeps sheets 2-4 resolving their
// own captions after this one changes.
func (x *xlsxWriter) sstIdx(text string) int {
	if i, ok := x.pool[text]; ok {
		return i
	}
	i := x.next
	x.next++
	x.pool[text] = i
	inner := "<t>" + escapeXML(text) + "</t>"
	if text != strings.TrimSpace(text) {
		// Excel strips padding without this hint, and several of the template's
		// captions are padded.
		inner = `<t xml:space="preserve">` + escapeXML(text) + `</t>`
	}
	x.sst = strings.Replace(x.sst, "</sst>", "<si>"+inner+"</si></sst>", 1)
	return i
}

func (x *xlsxWriter) styleOf(ref string, fallback int) int {
	m := x.cellRe(ref).FindString(x.sheet)
	if m == "" {
		return fallback
	}
	if s := reStyle.FindStringSubmatch(m); s != nil {
		if n, err := strconv.Atoi(s[1]); err == nil {
			return n
		}
	}
	return fallback
}

// setCell replaces exactly one cell. Refusing on a zero or multiple match is the
// point: a silent no-op here would leave the template's own value in place.
func (x *xlsxWriter) setCell(ref, replacement string) error {
	re := x.cellRe(ref)
	switch n := len(re.FindAllString(x.sheet, -1)); n {
	case 1:
		x.sheet = re.ReplaceAllString(x.sheet, replacement)
		return nil
	default:
		return fmt.Errorf("report: expected 1 cell %s in the attendance template, found %d", ref, n)
	}
}

func strCell(ref string, style, idx int) string {
	return fmt.Sprintf(`<c r="%s" s="%d" t="s"><v>%d</v></c>`, ref, style, idx)
}
func numCell(ref string, style int, v int64) string {
	return fmt.Sprintf(`<c r="%s" s="%d"><v>%d</v></c>`, ref, style, v)
}
func blankCell(ref string, style int) string { return fmt.Sprintf(`<c r="%s" s="%d"/>`, ref, style) }

// styleNumFmt reports the number format a style uses, or "" when it has none. The
// date cells are checked here so a template edit cannot quietly move them back to
// the built-in US format.
func (x *xlsxWriter) styleNumFmt(style int) string {
	for _, p := range x.parts {
		if p.name != xlsxStyles {
			continue
		}
		m := regexp.MustCompile(`(?s)<cellXfs count="\d+">(.*?)</cellXfs>`).FindStringSubmatch(string(p.data))
		if m == nil {
			return ""
		}
		xfs := reXf.FindAllString(m[1], -1)
		if style < 0 || style >= len(xfs) {
			return ""
		}
		if n := reNumFmt.FindStringSubmatch(xfs[style]); n != nil {
			return n[1]
		}
	}
	return ""
}

// findSignatureRow locates the row carrying the signature caption.
func (x *xlsxWriter) findSignatureRow() (int, error) {
	re := regexp.MustCompile(`<c r="[A-Z]+(\d+)"[^>]*t="s"[^>]*><v>(\d+)</v></c>`)
	for _, m := range re.FindAllStringSubmatch(x.sheet, -1) {
		i, _ := strconv.Atoi(m[2])
		if i < x.next && strings.Contains(x.poolText(i), xlsxSignature) {
			n, _ := strconv.Atoi(m[1])
			return n, nil
		}
	}
	return 0, fmt.Errorf("report: the attendance template has no %q row", xlsxSignature)
}

func (x *xlsxWriter) poolText(i int) string {
	all := reSiBody.FindAllStringSubmatch(x.sst, -1)
	if i < 0 || i >= len(all) {
		return ""
	}
	return x.textOf(all[i][1])
}

func serialFor(t time.Time) int64 {
	// The 1900 date system: workbookPr carries no date1904, which the template
	// verifier asserts, so this is the right epoch for these cells.
	base := time.Date(1899, 12, 30, 0, 0, 0, 0, t.Location())
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, base.Location())
	return int64(d.Sub(base) / (24 * time.Hour))
}

func (x *xlsxWriter) fill(v attendanceView, start, end time.Time) error {
	// The three header fields, and the seven dates.
	if err := x.setCell("C4", strCell("C4", x.styleOf("C4", 17), x.sstIdx(v.MonthLabel))); err != nil {
		return err
	}
	if err := x.setCell("C5", strCell("C5", x.styleOf("C5", 18), x.sstIdx(v.WeekLabel))); err != nil {
		return err
	}
	// Data Entrega is the current Monday, as the sheet asks for.
	if err := x.setCell("C6", numCell("C6", x.styleOf("C6", 19), serialFor(mondayOf(start)))); err != nil {
		return err
	}
	for i := 0; i < 7; i++ {
		ref := string(xlsxDayCols[i*2]) + "9"
		if err := x.setCell(ref, numCell(ref, x.styleOf(ref, 24), serialFor(start.AddDate(0, 0, i)))); err != nil {
			return err
		}
	}

	if err := x.writeMemberBlock(v); err != nil {
		return err
	}

	// The counters must agree with reality or Excel offers to "repair" the file.
	// sheet1 is counted from x.sheet because x.parts still holds the ORIGINAL bytes
	// for it; the other sheets are counted where they sit.
	strCells := len(reString.FindAllString(x.sheet, -1))
	for _, p := range x.parts {
		if p.name == xlsxSheet ||
			!strings.HasPrefix(p.name, "xl/worksheets/sheet") || !strings.HasSuffix(p.name, ".xml") {
			continue
		}
		strCells += len(reString.FindAllString(string(p.data), -1))
	}
	x.sst = regexp.MustCompile(`(<sst[^>]*?)\scount="\d+"`).
		ReplaceAllString(x.sst, `${1} count="`+strconv.Itoa(strCells)+`"`)
	x.sst = regexp.MustCompile(`(<sst[^>]*?)\suniqueCount="\d+"`).
		ReplaceAllString(x.sst, `${1} uniqueCount="`+strconv.Itoa(x.next)+`"`)
	return nil
}

// writeMemberBlock replaces the whole member block: the existing rows are removed,
// the signature row and everything below it are renumbered for the new size, and
// fresh rows are spliced in ahead of it.
//
// Remove-then-renumber-then-insert, in that order, is what makes it safe. Renaming
// first would move the signature row onto a row that is still present; inserting
// first would mean guessing where "the signature row" now is.
func (x *xlsxWriter) writeMemberBlock(v attendanceView) error {
	sigRow, err := x.findSignatureRow()
	if err != nil {
		return err
	}
	oldCount := sigRow - xlsxFirstMemberRow

	// Keep a member row's opening tag as the prototype, so an inserted row carries
	// the same height and border flags as the ones already there.
	proto := ""
	if m := regexp.MustCompile(fmt.Sprintf(`<row r="%d"[^>]*>`, xlsxFirstMemberRow)).FindString(x.sheet); m != "" {
		proto = m
	}
	if proto == "" {
		return fmt.Errorf("report: the attendance template has no member rows")
	}

	// Drop the old member-block merges FIRST, judged by their ORIGINAL row numbers.
	// Doing it after the renumber is the trap: the signature row own merge lands
	// inside the final member range, so a filter written against the new numbering
	// would delete it, while merges left over from the template own last fill -- at
	// rows the new block no longer covers -- would survive pointing at rows that do
	// not exist, or appear twice where it does.
	for row := xlsxFirstMemberRow; row < sigRow; row++ {
		re := regexp.MustCompile(fmt.Sprintf(`<row r="%d"(?:\s[^>]*)?>.*?</row>`, row))
		x.sheet = re.ReplaceAllString(x.sheet, "")
	}
	for _, m := range reMerge.FindAllString(x.sheet, -1) {
		g := reMerge.FindStringSubmatch(m)
		ra, _ := strconv.Atoi(g[2])
		if ra >= xlsxFirstMemberRow && ra < sigRow {
			x.sheet = strings.Replace(x.sheet, m, "", 1)
		}
	}

	delta := len(v.Rows) - oldCount
	if delta != 0 {
		x.sheet = reRow.ReplaceAllStringFunc(x.sheet, func(m string) string {
			old := reRow.FindStringSubmatch(m)[1]
			n, _ := strconv.Atoi(old)
			if n >= sigRow {
				n += delta
			}
			return strings.Replace(m, "r="+strconv.Quote(old), "r="+strconv.Quote(strconv.Itoa(n)), 1)
		})
		x.sheet = reRef.ReplaceAllStringFunc(x.sheet, func(m string) string {
			g := reRef.FindStringSubmatch(m)
			n, _ := strconv.Atoi(g[2])
			if n >= sigRow {
				n += delta
			}
			old := strings.Join([]string{"<c r=", strconv.Quote(g[1] + g[2])}, "")
			return strings.Replace(m, old, "<c r="+strconv.Quote(g[1]+strconv.Itoa(n)), 1)
		})
		x.sheet = reMerge.ReplaceAllStringFunc(x.sheet, func(m string) string {
			g := reMerge.FindStringSubmatch(m)
			ra, _ := strconv.Atoi(g[2])
			rb, _ := strconv.Atoi(g[4])
			if ra >= sigRow {
				ra += delta
				rb += delta
			}
			return fmt.Sprintf(`<mergeCell ref="%s%d:%s%d"/>`, g[1], ra, g[3], rb)
		})
		sigRow += delta
	}

	// Splice the fresh rows in immediately before the signature row element.
	var block strings.Builder
	var added []string
	for i, r := range v.Rows {
		row := xlsxFirstMemberRow + i
		block.WriteString(strings.Replace(proto, fmt.Sprintf(`r="%d"`, xlsxFirstMemberRow), fmt.Sprintf(`r="%d"`, row), 1))
		block.WriteString(strCell(fmt.Sprintf("B%d", row), x.styleOf(fmt.Sprintf("B%d", row), 11), x.sstIdx(r.Name)))
		for day := 0; day < 7; day++ {
			l := fmt.Sprintf("%s%d", string(xlsxDayCols[day*2]), row)
			rt := fmt.Sprintf("%s%d", string(xlsxDayCols[day*2+1]), row)
			c := r.Cells[day]
			start, end := c.Split()
			if c.Status != "" {
				// One word across the pair, so the pair MERGES and the Saida cell
				// keeps its style while staying empty.
				block.WriteString(strCell(l, x.styleOf(l, 26), x.sstIdx(c.Status)))
				block.WriteString(blankCell(rt, x.styleOf(rt, 27)))
				added = append(added, fmt.Sprintf("<mergeCell ref=%s/>", strconv.Quote(l+":"+rt)))
			} else {
				// Two times, so the pair must be SPLIT. Left merged, the Saida
				// would vanish with nothing to say so.
				block.WriteString(strCell(l, x.styleOf(l, 7), x.sstIdx(start)))
				block.WriteString(strCell(rt, x.styleOf(rt, 7), x.sstIdx(end)))
			}
		}
		block.WriteString(blankCell(fmt.Sprintf("Q%d", row), x.styleOf(fmt.Sprintf("Q%d", row), 10)))
		block.WriteString("</row>")
	}

	reSig := regexp.MustCompile(fmt.Sprintf(`<row r="%d"(?:\s[^>]*)?>.*?</row>`, sigRow))
	m := reSig.FindString(x.sheet)
	if m == "" {
		return fmt.Errorf("report: the attendance template lost its signature row at %d", sigRow)
	}
	x.sheet = strings.Replace(x.sheet, m, block.String()+m, 1)

	// Read the merge list back AFTER the renumbering, never from a snapshot taken
	// before it: the signature row own merge has moved, and writing the old
	// reference back would point it at a row that no longer holds its cells.
	all := append(reMerge.FindAllString(x.sheet, -1), added...)
	if reMergesBlk.MatchString(x.sheet) {
		x.sheet = reMergesBlk.ReplaceAllString(x.sheet,
			fmt.Sprintf(`<mergeCells count="%d">%s</mergeCells>`, len(all), strings.Join(all, "")))
	}

	x.sheet = reDim.ReplaceAllString(x.sheet, fmt.Sprintf(`<dimension ref="A1:Q%d"/>`, sigRow))
	return nil
}

func (x *xlsxWriter) write() ([]byte, error) {
	for i := range x.parts {
		switch x.parts[i].name {
		case xlsxSheet:
			x.parts[i].data = []byte(x.sheet)
		case xlsxStrings:
			x.parts[i].data = []byte(x.sst)
		}
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range x.parts {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: p.name, Method: p.method})
		if err != nil {
			return nil, fmt.Errorf("report: writing %s: %w", p.name, err)
		}
		if _, err := w.Write(p.data); err != nil {
			return nil, fmt.Errorf("report: writing %s: %w", p.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("report: closing the workbook: %w", err)
	}
	return buf.Bytes(), nil
}

func escapeXML(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;").Replace(s)
}

func unescapeXML(s string) string {
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&quot;", `"`, "&apos;", "'", "&amp;", "&").Replace(s)
}

// mondayOf snaps a date back to the Monday of its week.
func mondayOf(t time.Time) time.Time {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	return d.AddDate(0, 0, -int(d.Weekday())+1)
}
