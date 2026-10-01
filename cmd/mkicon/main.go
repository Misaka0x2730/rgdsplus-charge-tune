// Command mkicon renders the menu icon from its SVG source:
//
//	go run ./cmd/mkicon                  # platforms/rgdsplus/icon.svg -> icon.png
//	go run ./cmd/mkicon -check           # fail if icon.png is out of date (CI)
//
// The renderer is oksvg/rasterx (pure Go, already used by gabagool), so the
// PNG comes out the same on every machine.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

func main() {
	in := flag.String("in", "platforms/rgdsplus/icon.svg", "SVG source")
	out := flag.String("out", "platforms/rgdsplus/icon.png", "PNG to write")
	check := flag.Bool("check", false, "compare with the existing PNG instead of writing it")
	flag.Parse()

	img, err := render(*in)
	if err != nil {
		fail(err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		fail(err)
	}
	if *check {
		f, err := os.Open(*out)
		if err != nil {
			fail(err)
		}
		defer f.Close()
		old, err := png.Decode(f)
		if err != nil {
			fail(err)
		}
		// Compare what a PNG holds: both images went through the encoder
		// (premultiplied RGBA becomes NRGBA), and the zlib stream may differ
		// between Go versions while the pixels do not.
		fresh, err := png.Decode(bytes.NewReader(buf.Bytes()))
		if err != nil {
			fail(err)
		}
		if !samePixels(old, fresh) {
			fail(fmt.Errorf("%s is out of date: run `task icon`", *out))
		}
		return
	}
	if err := os.WriteFile(*out, buf.Bytes(), 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("%s: %dx%d\n", *out, img.Bounds().Dx(), img.Bounds().Dy())
}

func render(path string) (*image.RGBA, error) {
	icon, err := oksvg.ReadIcon(path, oksvg.StrictErrorMode)
	if err != nil {
		return nil, err
	}
	w, h := int(icon.ViewBox.W), int(icon.ViewBox.H)
	icon.SetTarget(0, 0, float64(w), float64(h))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	scanner := rasterx.NewScannerGV(w, h, img, img.Bounds())
	icon.Draw(rasterx.NewDasher(w, h, scanner), 1)
	return img, nil
}

// samePixels compares two renderings with a small tolerance. arm64 fuses
// multiply-adds (FMA) and amd64 does not, so the anti-aliased edges can
// differ by one step in a few pixels between a Mac and CI; a real change of
// the icon differs in many pixels or by much more.
func samePixels(a, b image.Image) bool {
	const maxStep, maxShare = 2, 0.005 // 8-bit steps per channel, share of pixels
	if a.Bounds() != b.Bounds() {
		return false
	}
	differ := 0
	for y := b.Bounds().Min.Y; y < b.Bounds().Max.Y; y++ {
		for x := b.Bounds().Min.X; x < b.Bounds().Max.X; x++ {
			r1, g1, b1, a1 := a.At(x, y).RGBA()
			r2, g2, b2, a2 := b.At(x, y).RGBA()
			d := max(step(r1, r2), step(g1, g2), step(b1, b2), step(a1, a2))
			if d > maxStep {
				return false
			}
			if d > 0 {
				differ++
			}
		}
	}
	return float64(differ) <= maxShare*float64(b.Bounds().Dx()*b.Bounds().Dy())
}

// step is the difference of two 16-bit channel values in 8-bit steps.
func step(a, b uint32) uint32 {
	a, b = a>>8, b>>8
	if a > b {
		return a - b
	}
	return b - a
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "mkicon:", err)
	os.Exit(1)
}
