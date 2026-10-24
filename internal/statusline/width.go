package statusline

import (
	"strings"
	"unicode/utf8"
)

// cellWidth is the number of terminal cells a rune takes: 2 for East Asian
// wide and fullwidth characters, 1 otherwise.
func cellWidth(r rune) int {
	switch {
	case r >= 0x1100 && r <= 0x115f,
		r >= 0x2e80 && r <= 0xa4cf,
		r >= 0xac00 && r <= 0xd7a3,
		r >= 0xf900 && r <= 0xfaff,
		r >= 0xfe30 && r <= 0xfe4f,
		r >= 0xff00 && r <= 0xff60,
		r >= 0xffe0 && r <= 0xffe6,
		r >= 0x1f300 && r <= 0x1faff,
		r >= 0x20000 && r <= 0x3fffd:
		return 2
	}
	return 1
}

// width is the number of cells s takes.
func width(s string) int {
	w := 0
	for _, r := range s {
		w += cellWidth(r)
	}
	return w
}

// clean replaces control characters (C0, DEL, C1) and invalid UTF-8 with
// spaces, so a title cannot move the cursor or recolour the line.
func clean(s string) string {
	var b strings.Builder
	for len(s) > 0 {
		r, n := utf8.DecodeRuneInString(s)
		s = s[n:]
		if r == utf8.RuneError && n == 1 || r < 0x20 || r >= 0x7f && r <= 0x9f {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// truncate cuts s to at most max cells, ending in an ellipsis when cut, at a
// cell boundary (a wide character never straddles the edge).
func truncate(s string, max int) string {
	if width(s) <= max {
		return s
	}
	if max <= 1 {
		return ""
	}
	var b strings.Builder
	w := 0
	for _, r := range s {
		cw := cellWidth(r)
		if w+cw > max-1 {
			break
		}
		b.WriteRune(r)
		w += cw
	}
	return b.String() + "…"
}

// pad fills s with spaces to w cells.
func pad(s string, w int) string {
	if d := w - width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// padLeft right-aligns s in w cells.
func padLeft(s string, w int) string {
	if d := w - width(s); d > 0 {
		return strings.Repeat(" ", d) + s
	}
	return s
}
