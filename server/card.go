package main

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const cardW, cardH = 1200, 630

var (
	colBg     = color.RGBA{0x06, 0x06, 0x06, 0xff}
	colPanel  = color.RGBA{0x0e, 0x0e, 0x10, 0xff}
	colLine   = color.RGBA{0x1f, 0x1f, 0x22, 0xff}
	colText   = color.RGBA{0xf2, 0xf1, 0xee, 0xff}
	colDim    = color.RGBA{0x8f, 0x8e, 0x89, 0xff}
	colAccent = color.RGBA{0x3b, 0x82, 0xf6, 0xff}

	// the site gradient: cyan, blue, violet
	gradStops = []color.RGBA{{0x22, 0xd3, 0xee, 0xff}, {0x3b, 0x82, 0xf6, 0xff}, {0x8b, 0x5c, 0xf6, 0xff}}
)

type faces struct {
	brand, title, spec, label, value, small font.Face
}

var (
	facesOnce sync.Once
	cardFaces faces
)

func loadFaces() {
	regular, _ := fontFS.ReadFile("fonts/JetBrainsMono-Regular.ttf")
	bold, _ := fontFS.ReadFile("fonts/JetBrainsMono-Bold.ttf")
	mk := func(ttf []byte, size float64) font.Face {
		f, err := opentype.Parse(ttf)
		if err != nil {
			panic(err)
		}
		face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			panic(err)
		}
		return face
	}
	cardFaces = faces{
		brand: mk(bold, 30),
		title: mk(bold, 48),
		spec:  mk(regular, 22),
		label: mk(regular, 18),
		value: mk(bold, 40),
		small: mk(regular, 19),
	}
}

func renderCard(w io.Writer, r *Result) error {
	facesOnce.Do(loadFaces)
	f := cardFaces

	img := image.NewRGBA(image.Rect(0, 0, cardW, cardH))
	fill(img, img.Bounds(), colBg)
	for x := 0; x < cardW; x++ {
		fill(img, image.Rect(x, 0, x+1, 5), gradAt(float64(x)/float64(cardW-1)))
	}

	const pad = 64
	mark(img, pad, 84)
	text(img, f.brand, colText, pad+44, 82, "nodebench")
	brand := "by InstantNode"
	text(img, f.spec, colDim, cardW-pad-measure(f.spec, brand), 80, brand)

	text(img, f.title, colText, pad, 190, fit(f.title, shortCPU(r.System.CPU), cardW-2*pad))
	text(img, f.spec, colDim, pad, 238, fit(f.spec, specLine(r), cardW-2*pad))

	type tile struct {
		label, value, sub string
		bars              []float64
	}
	tiles := []tile{{label: "CPU SHA256", value: "skipped"}, {label: "DISK 4K RANDOM", value: "skipped"}, {label: "BEST DOWNLOAD", value: "skipped"}}
	if c := r.CPU; c != nil {
		tiles[0] = tile{"CPU SHA256", fmtBytes(c.SHA256N), plural(c.Threads, "thread"), []float64{c.SHA256, c.SHA256N}}
	}
	if t := disk4k(r); t != nil {
		var bs []float64
		for _, d := range r.Disk.Tests {
			bs = append(bs, d.ReadKBs+d.WriteKBs)
		}
		tiles[1] = tile{"DISK " + strings.ToUpper(t.BS) + " RANDOM", fmtKBs(t.ReadKBs + t.WriteKBs), fmtIOPS(t.ReadIOPS+t.WriteIOPS) + " IOPS", bs}
	}
	if t := bestNet(r); t != nil {
		var bs []float64
		for _, n := range r.Net.Tests {
			if n.Proto == 4 {
				bs = append(bs, n.Recv)
			}
		}
		tiles[2] = tile{"BEST DOWNLOAD", fmtMbps(t.Recv), t.Location, bs}
	}

	const top, h, gap = 286, 222, 20
	tw := (cardW - 2*pad - 2*gap) / 3
	for i, t := range tiles {
		x0 := pad + i*(tw+gap)
		box := image.Rect(x0, top, x0+tw, top+h)
		fill(img, box, colLine)
		fill(img, box.Inset(1), colPanel)
		text(img, f.label, colDim, x0+24, top+44, t.label)
		vc := colText
		if t.sub == "" {
			vc = colDim
		}
		text(img, f.value, vc, x0+24, top+108, fit(f.value, t.value, tw-48))
		text(img, f.small, colDim, x0+24, top+150, fit(f.small, t.sub, tw-48))
		sparkBars(img, image.Rect(x0+24, top+170, x0+tw-24, top+200), t.bars)
	}

	fill(img, image.Rect(pad, 540, cardW-pad, 541), colLine)
	text(img, f.small, colDim, pad, 584, r.Created.Format("2 Jan 2006, 15:04 UTC"))
	link := strings.TrimPrefix(strings.TrimPrefix(*baseURL, "https://"), "http://") + "/r/" + r.ID
	text(img, f.small, colDim, cardW-pad-measure(f.small, link), 584, link)

	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	return enc.Encode(w, img)
}

// sparkBars draws small columns scaled to the largest value; the largest
// one is in the accent colour. Zero values (failed tests) stay as a stub.
func sparkBars(img *image.RGBA, box image.Rectangle, v []float64) {
	if len(v) == 0 {
		return
	}
	var top float64
	for _, x := range v {
		top = max(top, x)
	}
	if top <= 0 {
		return
	}
	const gap = 6
	w := (box.Dx() - gap*(len(v)-1)) / len(v)
	w = min(w, 40)
	for i, x := range v {
		h := int(float64(box.Dy()) * x / top)
		h = max(h, 2)
		c := colLine
		if x == top {
			c = colAccent
		} else if x > 0 {
			c = color.RGBA{0x3a, 0x3a, 0x40, 0xff}
		}
		x0 := box.Min.X + i*(w+gap)
		fill(img, image.Rect(x0, box.Max.Y-h, x0+w, box.Max.Y), c)
	}
}

// mark draws the logo bars with their baseline at y.
func mark(img *image.RGBA, x, y int) {
	heights := []int{9, 17, 12, 25, 36}
	for i, h := range heights {
		c := colText
		if i == len(heights)-1 {
			c = colAccent
		}
		fill(img, image.Rect(x+i*7, y-h, x+i*7+5, y), c)
	}
}

func gradAt(t float64) color.RGBA {
	seg := t * float64(len(gradStops)-1)
	i := int(seg)
	if i >= len(gradStops)-1 {
		return gradStops[len(gradStops)-1]
	}
	a, b, f := gradStops[i], gradStops[i+1], seg-float64(i)
	mix := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*f) }
	return color.RGBA{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B), 0xff}
}

func fill(img *image.RGBA, r image.Rectangle, c color.Color) {
	draw.Draw(img, r, image.NewUniform(c), image.Point{}, draw.Src)
}

// text draws s with its baseline at y and returns the x where it ended.
func text(img *image.RGBA, face font.Face, c color.Color, x, y int, s string) int {
	d := font.Drawer{Dst: img, Src: image.NewUniform(c), Face: face, Dot: fixed.P(x, y)}
	d.DrawString(s)
	return d.Dot.X.Round()
}

func measure(face font.Face, s string) int {
	return font.MeasureString(face, s).Round()
}

func fit(face font.Face, s string, max int) string {
	if measure(face, s) <= max {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && measure(face, string(r)+"...") > max {
		r = r[:len(r)-1]
	}
	return strings.TrimSpace(string(r)) + "..."
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}
