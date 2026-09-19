package tmux

import "math/rand/v2"

// Color identifies a member's persistent display color, independent of its status.
type Color string

// Named colors form the supported member palette.
const (
	Red    Color = "red"
	Orange Color = "orange"
	Amber  Color = "amber"
	Yellow Color = "yellow"
	Lime   Color = "lime"
	Green  Color = "green"
	Mint   Color = "mint"
	Teal   Color = "teal"
	Cyan   Color = "cyan"
	Sky    Color = "sky"
	Blue   Color = "blue"
	Indigo Color = "indigo"
	Violet Color = "violet"
	Purple Color = "purple"
	Pink   Color = "pink"
	Rose   Color = "rose"
)

var palette = []struct {
	name  Color
	value string
}{
	{Red, "colour203"}, {Orange, "colour215"}, {Amber, "colour214"}, {Yellow, "colour221"},
	{Lime, "colour155"}, {Green, "colour114"}, {Mint, "colour121"}, {Teal, "colour80"},
	{Cyan, "colour87"}, {Sky, "colour117"}, {Blue, "colour111"}, {Indigo, "colour105"},
	{Violet, "colour141"}, {Purple, "colour183"}, {Pink, "colour213"}, {Rose, "colour211"},
}

// ColorNames returns the palette names in display order for help and completion.
func ColorNames() []string {
	names := make([]string, len(palette))
	for i, entry := range palette {
		names[i] = string(entry.name)
	}
	return names
}

// RandomColor chooses a visual label; colors need not be unique within a team.
func RandomColor() Color { return palette[rand.IntN(len(palette))].name }

// StyleValue returns the indexed terminal color, or a neutral fallback for old state.
func (c Color) StyleValue() string {
	for _, entry := range palette {
		if entry.name == c {
			return entry.value
		}
	}
	return "colour252"
}
