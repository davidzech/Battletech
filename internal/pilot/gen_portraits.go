//go:build ignore

// gen_portraits.go vendors the named-pilot portraits into internal/pilot/portraits. Give it a folder of PNG portraits
// (square, with a transparent background); it writes each one under a lowercase, filesystem-safe name, scaled down to
// 192x192 so the picker stays light (they are shown at most about 144 px wide). From internal/pilot:
//
//	go run gen_portraits.go -src <folder of portraits>
//
// Files in the folder that are not PNGs are ignored. Names that become the same once lowercased are an error.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const size = 192

var unsafeName = regexp.MustCompile(`[^a-z0-9._-]+`)

// resize scales src to size x size by averaging the source pixels each destination pixel covers (premultiplied, so
// soft transparent edges stay clean).
func resize(src image.Image) *image.NRGBA {
	b := src.Bounds()
	sw, sh := float64(b.Dx()), float64(b.Dy())
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		y0, y1 := float64(y)*sh/size, float64(y+1)*sh/size
		for x := 0; x < size; x++ {
			x0, x1 := float64(x)*sw/size, float64(x+1)*sw/size
			var r, g, bl, a, weight float64
			for yy := int(y0); float64(yy) < y1 && yy < b.Dy(); yy++ {
				wy := math.Min(float64(yy+1), y1) - math.Max(float64(yy), y0)
				for xx := int(x0); float64(xx) < x1 && xx < b.Dx(); xx++ {
					w := wy * (math.Min(float64(xx+1), x1) - math.Max(float64(xx), x0))
					pr, pg, pb, pa := src.At(b.Min.X+xx, b.Min.Y+yy).RGBA() // premultiplied, 16-bit
					r += w * float64(pr)
					g += w * float64(pg)
					bl += w * float64(pb)
					a += w * float64(pa)
					weight += w
				}
			}
			if a <= 0 || weight <= 0 {
				continue
			}
			dst.SetNRGBA(x, y, color.NRGBA{
				R: uint8(math.Round(r / a * 255)),
				G: uint8(math.Round(g / a * 255)),
				B: uint8(math.Round(bl / a * 255)),
				A: uint8(math.Round(a / weight / 257)),
			})
		}
	}
	return dst
}

func main() {
	src := flag.String("src", "", "folder of portrait PNGs")
	flag.Parse()
	if *src == "" {
		log.Fatal("give -src <folder of portraits>")
	}
	entries, err := os.ReadDir(*src)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll("portraits", 0o755); err != nil {
		log.Fatal(err)
	}
	old, _ := filepath.Glob("portraits/*.png")
	for _, f := range old {
		if err := os.Remove(f); err != nil {
			log.Fatal(err)
		}
	}

	seen := map[string]string{}
	var names []string
	var total int64
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".png") {
			continue
		}
		out := unsafeName.ReplaceAllString(strings.ToLower(e.Name()), "_")
		if prev, ok := seen[out]; ok {
			log.Fatalf("%q and %q both become %q", prev, e.Name(), out)
		}
		seen[out] = e.Name()
		f, err := os.Open(filepath.Join(*src, e.Name()))
		if err != nil {
			log.Fatal(err)
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			log.Fatalf("%s: %v", e.Name(), err)
		}
		if img.Bounds().Dx() != img.Bounds().Dy() {
			log.Printf("warning: %s is %dx%d, not square", e.Name(), img.Bounds().Dx(), img.Bounds().Dy())
		}
		o, err := os.Create(filepath.Join("portraits", out))
		if err != nil {
			log.Fatal(err)
		}
		enc := png.Encoder{CompressionLevel: png.BestCompression}
		if err := enc.Encode(o, resize(img)); err != nil {
			log.Fatal(err)
		}
		if st, err := o.Stat(); err == nil {
			total += st.Size()
		}
		o.Close()
		names = append(names, out)
	}
	sort.Strings(names)
	fmt.Printf("wrote %d portraits (%.1f MB) into portraits/ at %dx%d\n", len(names), float64(total)/1e6, size, size)
}
