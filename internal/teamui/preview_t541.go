//go:build t541preview

package teamui

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// SaveT541Preview renders the production side-panel layout on a simulated
// screen. This file is excluded from normal builds by the t541preview tag.
func SaveT541Preview(dir, name, kind, current, selected string, width, height int, data Snapshot, completed bool, detailID string) error {
	p := &sidePanel{app: tview.NewApplication(), root: tview.NewFlex().SetDirection(tview.FlexRow),
		kind: kind, current: current, selectedID: selected, data: data, completed: completed,
		detailID: detailID, width: width, height: height}
	p.reconcile()
	if kind == "members" {
		p.reveal(p.memberIndex(p.selectedID))
	} else if detailID == "" {
		p.reveal(p.taskIndex(p.selectedID))
	}
	p.render()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		return err
	}
	defer screen.Fini()
	screen.SetSize(width, height)
	p.root.SetRect(0, 0, width, height)
	p.root.Draw(screen)
	const cellWidth, cellHeight = 10, 22
	var plain, colored, svg strings.Builder
	fmt.Fprintf(&svg, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`, width*cellWidth, height*cellHeight, width*cellWidth, height*cellHeight)
	fmt.Fprintf(&svg, `<rect width="100%%" height="100%%" fill="%s"/>`, previewColor(uiCanvas))
	for y := 0; y < height; y++ {
		for x := 0; x < width; {
			_, _, style, _ := screen.GetContent(x, y)
			_, background, _ := style.Decompose()
			end := x + 1
			for end < width {
				_, _, next, _ := screen.GetContent(end, y)
				_, nextBackground, _ := next.Decompose()
				if nextBackground != background {
					break
				}
				end++
			}
			fmt.Fprintf(&svg, `<rect x="%d" y="%d" width="%d" height="%d" fill="%s"/>`, x*cellWidth, y*cellHeight, (end-x)*cellWidth, cellHeight, previewColor(background))
			x = end
		}
		var prior tcell.Style
		for x := 0; x < width; x++ {
			r, combining, style, _ := screen.GetContent(x, y)
			if r == 0 {
				r = ' '
			}
			glyph := string(r) + string(combining)
			plain.WriteString(glyph)
			if x == 0 || style != prior {
				colored.WriteString(previewANSI(style))
				prior = style
			}
			colored.WriteString(glyph)
			if r == ' ' {
				continue
			}
			foreground, _, attributes := style.Decompose()
			weight := "normal"
			if attributes&tcell.AttrBold != 0 {
				weight = "bold"
			}
			fmt.Fprintf(&svg, `<text x="%d" y="%d" fill="%s" font-family="DejaVu Sans Mono, monospace" font-size="16" font-weight="%s">%s</text>`, x*cellWidth, y*cellHeight+17, previewColor(foreground), weight, html.EscapeString(glyph))
		}
		plain.WriteByte('\n')
		colored.WriteString("\x1b[0m\n")
	}
	svg.WriteString("</svg>\n")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for suffix, body := range map[string]string{"txt": plain.String(), "ansi": colored.String(), "svg": svg.String()} {
		if err := os.WriteFile(filepath.Join(dir, name+"."+suffix), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func previewColor(color tcell.Color) string {
	if color.Valid() {
		return color.CSS()
	}
	return uiCanvas.CSS()
}

func previewANSI(style tcell.Style) string {
	foreground, background, attributes := style.Decompose()
	parameters := []string{"0", previewANSIColor(foreground, "38"), previewANSIColor(background, "48")}
	if attributes&tcell.AttrBold != 0 {
		parameters = append(parameters, "1")
	}
	return "\x1b[" + strings.Join(parameters, ";") + "m"
}

func previewANSIColor(color tcell.Color, prefix string) string {
	if color.IsRGB() {
		r, g, b := color.RGB()
		return fmt.Sprintf("%s;2;%d;%d;%d", prefix, r, g, b)
	}
	return fmt.Sprintf("%s;5;%d", prefix, int(color&0xff))
}
