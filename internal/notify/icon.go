package notify

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
)

// The pixel ">_C" logo of docs/assets/banner.svg: x, y, width, height in
// the banner's units, from the logo's top left corner. It is 296 × 144.
var logo = [][4]float64{
	{0, 0, 24, 24}, {24, 24, 24, 24}, {48, 48, 24, 48}, {24, 96, 24, 24}, {0, 120, 24, 24}, // >
	{96, 120, 56, 24},                                                                             // _
	{216, 0, 80, 24}, {192, 24, 24, 96}, {216, 120, 80, 24}, {272, 24, 24, 24}, {272, 96, 24, 24}, // C
}

const logoW, logoH = 296.0, 144.0

var (
	iconBG   = color.NRGBA{0x1e, 0x1e, 0x2e, 0xff}
	iconMint = color.NRGBA{0x78, 0xe8, 0xaf, 0xff}
)

// Icon draws csquad's icon: the logo in mint on the banner's dark
// background, a square with round corners, size × size pixels.
func Icon(size int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	s := float64(size)
	scale := 0.72 * s / logoW
	ox, oy := (s-logoW*scale)/2, (s-logoH*scale)/2
	radius := 0.2 * s
	for y := range size {
		for x := range size {
			bg := cornerCoverage(float64(x), float64(y), s, radius)
			if bg == 0 {
				continue
			}
			// The logo's rectangles do not overlap: their coverages add up.
			fg := 0.0
			for _, r := range logo {
				fg += overlap(float64(x), float64(x)+1, ox+r[0]*scale, ox+(r[0]+r[2])*scale) *
					overlap(float64(y), float64(y)+1, oy+r[1]*scale, oy+(r[1]+r[3])*scale)
			}
			fg = math.Min(fg, 1)
			mix := func(a, b uint8) uint8 { return uint8(math.Round(float64(a)*(1-fg) + float64(b)*fg)) }
			img.SetNRGBA(x, y, color.NRGBA{mix(iconBG.R, iconMint.R), mix(iconBG.G, iconMint.G), mix(iconBG.B, iconMint.B), uint8(math.Round(255 * bg))})
		}
	}
	return img
}

func overlap(a0, a1, b0, b1 float64) float64 { return math.Max(0, math.Min(a1, b1)-math.Max(a0, b0)) }

// cornerCoverage is how much of pixel (x, y) the rounded square covers,
// sampled 4 × 4 times in the corners.
func cornerCoverage(x, y, s, r float64) float64 {
	inCorner := func(px, py float64) bool {
		cx, cy := math.Max(r, math.Min(s-r, px)), math.Max(r, math.Min(s-r, py))
		return math.Hypot(px-cx, py-cy) <= r
	}
	if x+1 > r && x < s-r || y+1 > r && y < s-r {
		return 1
	}
	n := 0
	for i := range 4 {
		for j := range 4 {
			if inCorner(x+(float64(i)+0.5)/4, y+(float64(j)+0.5)/4) {
				n++
			}
		}
	}
	return float64(n) / 16
}

// PNG is the icon as a PNG file.
func PNG(size int) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, Icon(size))
	return b.Bytes()
}

// ICNS is the icon as a macOS icns file. An icns entry can hold PNG data:
// ic07 128, ic08 256, ic09 512 pixels.
func ICNS() []byte {
	var body bytes.Buffer
	for _, e := range []struct {
		kind string
		size int
	}{{"ic07", 128}, {"ic08", 256}, {"ic09", 512}} {
		data := PNG(e.size)
		body.WriteString(e.kind)
		_ = binary.Write(&body, binary.BigEndian, uint32(8+len(data)))
		body.Write(data)
	}
	var out bytes.Buffer
	out.WriteString("icns")
	_ = binary.Write(&out, binary.BigEndian, uint32(8+body.Len()))
	out.Write(body.Bytes())
	return out.Bytes()
}
