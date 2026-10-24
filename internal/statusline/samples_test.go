package statusline

// The status line samples of the README: SVG files in docs/images/ledger,
// rendered from the real Layout in a dark and a light variant.
// TestGoldenReadmeSamples fails when one is out of date; -update rewrites
// them.

import (
	"fmt"
	"html"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type readmeSample struct {
	name    string
	tasks   []Task
	columns int
}

func readmeSamples() []readmeSample {
	pick := func(ids ...int) []Task {
		var out []Task
		for _, id := range ids {
			for _, t := range scenario(9) {
				if t.ID == id {
					out = append(out, t)
				}
			}
		}
		return out
	}
	return []readmeSample{
		{"ledger-80", pick(9, 4, 1, 7), 80},
		{"ledger-1-task-80", scenario(1), 80},
		{"ledger-9-tasks-124", scenario(9), 124},
		{"ledger-9-tasks-160", scenario(9), 160},
		{"ledger-long-badge-56", scenario(3), 56},
	}
}

type svgTheme struct {
	name           string
	bg, fg, border [3]int
}

// The page colours of GitHub's dark and light themes.
var svgThemes = []svgTheme{
	{"dark", [3]int{13, 17, 23}, [3]int{201, 209, 217}, [3]int{48, 54, 61}},
	{"light", [3]int{255, 255, 255}, [3]int{31, 35, 40}, [3]int{208, 215, 222}},
}

// A run is text of one style at a cell of a rendered row.
type run struct {
	cell             int
	text             string
	fg, bg           *[3]int
	bold, faint, rev bool
}

// parseRow splits a rendered row (SGR sequences only, as Layout writes
// them) into runs.
func parseRow(s string) []run {
	var out []run
	var cur run
	cell := 0
	for len(s) > 0 {
		text := s
		i := strings.Index(s, "\x1b[")
		if i >= 0 {
			text = s[:i]
		}
		if text != "" {
			r := cur
			r.cell, r.text = cell, text
			out = append(out, r)
			cell += width(text)
		}
		if i < 0 {
			break
		}
		s = s[i+2:]
		j := strings.IndexByte(s, 'm')
		params := strings.Split(s[:j], ";")
		s = s[j+1:]
		for k := 0; k < len(params); k++ {
			switch params[k] {
			case "0", "":
				cur = run{}
			case "1":
				cur.bold = true
			case "2":
				cur.faint = true
			case "7":
				cur.rev = true
			case "38", "48":
				if k+4 < len(params) && params[k+1] == "2" {
					var c [3]int
					for n := range 3 {
						c[n], _ = strconv.Atoi(params[k+2+n])
					}
					if params[k] == "38" {
						cur.fg = &c
					} else {
						cur.bg = &c
					}
					k += 4
				}
			}
		}
	}
	return out
}

// colors are the text and background colours of a run on a page of bg and
// fg (background nil: the page's); faint text is half way to the
// background.
func (r run) colors(fg, bg [3]int) (text [3]int, back *[3]int) {
	text = fg
	if r.fg != nil {
		text = *r.fg
	}
	back = r.bg
	if r.rev {
		b := text
		if back != nil {
			text = *back
		} else {
			text = bg
		}
		back = &b
	}
	if r.faint {
		under := bg
		if back != nil {
			under = *back
		}
		for i := range 3 {
			text[i] = (text[i] + under[i]) / 2
		}
	}
	return text, back
}

func hexColor(c [3]int) string { return fmt.Sprintf("#%02x%02x%02x", c[0], c[1], c[2]) }

const (
	svgFont = 14  // px
	svgCell = 8.4 // px per cell: a monospace advance at svgFont
	svgLine = 21  // px per row
	svgPad  = 12  // px around the rows
)

func px(v float64) string { return strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64) }

// renderSVG draws rendered rows as a terminal in theme th. Every glyph is
// centred in its cells (two for CJK) with its own x, so the grid holds
// with whatever font the reader has; the SVG has no fonts, styles or
// scripts.
func renderSVG(rows []string, th svgTheme) string {
	cols := 0
	for _, r := range rows {
		cols = max(cols, width(strip(r)))
	}
	w := 2*svgPad + float64(cols)*svgCell
	h := 2*svgPad + len(rows)*svgLine
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%s" height="%d" viewBox="0 0 %s %d">`+"\n", px(w), h, px(w), h)
	fmt.Fprintf(&b, `<rect x="0.5" y="0.5" width="%s" height="%d" rx="6" fill="%s" stroke="%s"/>`+"\n", px(w-1), h-1, hexColor(th.bg), hexColor(th.border))
	fmt.Fprintf(&b, `<g font-family="ui-monospace, SFMono-Regular, 'Cascadia Mono', Menlo, Consolas, 'DejaVu Sans Mono', 'Liberation Mono', monospace" font-size="%d" text-anchor="middle">`+"\n", svgFont)
	for i, row := range rows {
		top := svgPad + i*svgLine
		for _, r := range parseRow(row) {
			fg, bg := r.colors(th.fg, th.bg)
			if bg != nil {
				fmt.Fprintf(&b, `<rect x="%s" y="%d" width="%s" height="%d" fill="%s"/>`+"\n", px(svgPad+float64(r.cell)*svgCell), top, px(float64(width(r.text))*svgCell), svgLine, hexColor(*bg))
			}
			attrs := fmt.Sprintf(` y="%d" fill="%s"`, top+15, hexColor(fg))
			if r.bold {
				attrs += ` font-weight="bold"`
			}
			// One text element per word: spaces only move the cell.
			cell := r.cell
			var word strings.Builder
			var xs []string
			flush := func() {
				if word.Len() > 0 {
					fmt.Fprintf(&b, `<text x="%s"%s>%s</text>`+"\n", strings.Join(xs, " "), attrs, html.EscapeString(word.String()))
				}
				word.Reset()
				xs = xs[:0]
			}
			for _, c := range r.text {
				cw := cellWidth(c)
				if c == ' ' {
					flush()
				} else {
					word.WriteRune(c)
					xs = append(xs, px(svgPad+(float64(cell)+float64(cw)/2)*svgCell))
				}
				cell += cw
			}
			flush()
		}
	}
	b.WriteString("</g>\n</svg>\n")
	return b.String()
}

func TestGoldenReadmeSamples(t *testing.T) {
	dir := filepath.Join("..", "..", "docs", "images", "ledger")
	for _, s := range readmeSamples() {
		rows := Layout(s.tasks, s.columns, now, false)
		for _, th := range svgThemes {
			checkFile(t, filepath.Join(dir, s.name+"-"+th.name+".svg"), renderSVG(rows, th))
		}
	}
}
