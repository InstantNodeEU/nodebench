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
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const cardW, cardH = 1200, 630

var (
	colBg     = color.RGBA{0x11, 0x13, 0x15, 0xff}
	colPanel  = color.RGBA{0x18, 0x1b, 0x1e, 0xff}
	colLine   = color.RGBA{0x27, 0x2b, 0x30, 0xff}
	colText   = color.RGBA{0xe4, 0xe6, 0xe8, 0xff}
	colDim    = color.RGBA{0x8b, 0x91, 0x99, 0xff}
	colAccent = color.RGBA{0xf0, 0xa8, 0x30, 0xff}
)

type faces struct {
	brand, title, spec, label, value, small font.Face
}

var (
	facesOnce sync.Once
	cardFaces faces
)

func loadFaces() {
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
		brand: mk(gomonobold.TTF, 30),
		title: mk(gobold.TTF, 54),
		spec:  mk(goregular.TTF, 26),
		label: mk(goregular.TTF, 19),
		value: mk(gomonobold.TTF, 46),
		small: mk(goregular.TTF, 21),
	}
}

func renderCard(w io.Writer, r *Result) error {
	facesOnce.Do(loadFaces)
	f := cardFaces

	img := image.NewRGBA(image.Rect(0, 0, cardW, cardH))
	fill(img, img.Bounds(), colBg)
	fill(img, image.Rect(0, 0, cardW, 6), colAccent)

	const pad = 64
	x := text(img, f.brand, colText, pad, 82, "nodebench")
	text(img, f.brand, colAccent, x, 82, "_")
	brand := "InstantNode"
	text(img, f.spec, colDim, cardW-pad-measure(f.spec, brand), 80, brand)

	text(img, f.title, colText, pad, 190, fit(f.title, shortCPU(r.System.CPU), cardW-2*pad))
	text(img, f.spec, colDim, pad, 238, fit(f.spec, specLine(r), cardW-2*pad))

	type tile struct{ label, value, sub string }
	tiles := []tile{{"CPU SHA256", "skipped", ""}, {"DISK 4K RANDOM", "skipped", ""}, {"BEST DOWNLOAD", "skipped", ""}}
	if c := r.CPU; c != nil {
		tiles[0] = tile{"CPU SHA256", fmtBytes(c.SHA256N), plural(c.Threads, "thread")}
	}
	if t := disk4k(r); t != nil {
		tiles[1] = tile{"DISK " + strings.ToUpper(t.BS) + " RANDOM", fmtKBs(t.ReadKBs + t.WriteKBs), fmtIOPS(t.ReadIOPS+t.WriteIOPS) + " IOPS"}
	}
	if t := bestNet(r); t != nil {
		tiles[2] = tile{"BEST DOWNLOAD", fmtMbps(t.Recv), t.Location}
	}

	const top, h, gap = 290, 180, 20
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
	}

	fill(img, image.Rect(pad, 540, cardW-pad, 541), colLine)
	text(img, f.small, colDim, pad, 584, r.Created.Format("2 Jan 2006, 15:04 UTC"))
	link := strings.TrimPrefix(strings.TrimPrefix(*baseURL, "https://"), "http://") + "/r/" + r.ID
	text(img, f.small, colDim, cardW-pad-measure(f.small, link), 584, link)

	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	return enc.Encode(w, img)
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
