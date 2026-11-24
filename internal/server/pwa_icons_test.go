package server

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
)

const webPublicDir = "../../web/public"

// maxTileRadius is the largest corner radius, as a share of the icon size,
// an "any" icon's tile may use; scripts/generate-icons.mjs uses 0.22.
const maxTileRadius = 0.25

func decodePublicPNG(t *testing.T, src string) image.Image {
	t.Helper()
	f, err := os.Open(filepath.Join(webPublicDir, filepath.FromSlash(src)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("%s: %v", src, err)
	}
	return img
}

// insideRoundedTile reports whether pixel (x, y) lies inside a size×size
// tile with corner radius r, shrunk by one pixel for antialiased edges.
func insideRoundedTile(x, y, size int, r float64) bool {
	px, py := float64(x)+0.5, float64(y)+0.5
	lo, hi := r+1, float64(size)-r-1
	cx, cy := math.Max(lo, math.Min(px, hi)), math.Max(lo, math.Min(py, hi))
	return math.Hypot(px-cx, py-cy) <= r
}

// firstTransparent returns the first pixel inside the tile that is not fully
// opaque, or "" when there is none. r < 0 checks the whole image.
func firstTransparent(img image.Image, r float64) string {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if r >= 0 && !insideRoundedTile(x-b.Min.X, y-b.Min.Y, b.Dx(), r) {
				continue
			}
			if _, _, _, a := img.At(x, y).RGBA(); a != 0xffff {
				return fmt.Sprintf("(%d,%d) alpha %d", x, y, a>>8)
			}
		}
	}
	return ""
}

// TestManifestIconsAreOpaqueTiles guards #1155: Windows and ChromeOS show the
// manifest's "any" icon as-is, so it must be an opaque tile rather than the
// bare transparent mark, and maskable icons must be full-bleed.
func TestManifestIconsAreOpaqueTiles(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(webPublicDir, "manifest.webmanifest"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		BackgroundColor string `json:"background_color"`
		Icons           []struct {
			Src, Sizes, Purpose string
		} `json:"icons"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.BackgroundColor != "#ffffff" {
		t.Errorf("background_color = %q, want #ffffff", manifest.BackgroundColor)
	}

	sizes := map[string]map[string]bool{}
	for _, icon := range manifest.Icons {
		img := decodePublicPNG(t, icon.Src)
		b := img.Bounds()
		if got := fmt.Sprintf("%dx%d", b.Dx(), b.Dy()); got != icon.Sizes {
			t.Errorf("%s is %s, manifest says %s", icon.Src, got, icon.Sizes)
		}
		if sizes[icon.Purpose] == nil {
			sizes[icon.Purpose] = map[string]bool{}
		}
		sizes[icon.Purpose][icon.Sizes] = true

		switch icon.Purpose {
		case "any":
			if at := firstTransparent(img, maxTileRadius*float64(b.Dx())); at != "" {
				t.Errorf("%s: transparent pixel inside the rounded tile at %s", icon.Src, at)
			}
		case "maskable":
			if at := firstTransparent(img, -1); at != "" {
				t.Errorf("%s: maskable icon is not full-bleed, transparent at %s", icon.Src, at)
			}
		default:
			t.Errorf("%s: unexpected purpose %q", icon.Src, icon.Purpose)
		}
	}
	for _, purpose := range []string{"any", "maskable"} {
		for _, size := range []string{"192x192", "512x512"} {
			if !sizes[purpose][size] {
				t.Errorf("manifest has no %s %q icon", size, purpose)
			}
		}
	}

	if at := firstTransparent(decodePublicPNG(t, "icons/apple-touch-icon.png"), -1); at != "" {
		t.Errorf("apple-touch-icon.png is not opaque, transparent at %s", at)
	}
}
