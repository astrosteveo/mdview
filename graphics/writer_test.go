package graphics

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"strings"
	"sync"
	"testing"

	"github.com/astrosteveo/mdview/mermaid"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
)

func fixture() *mermaid.Image {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 120, 60)))
	return &mermaid.Image{PNG: b.Bytes(), Width: 120, Height: 60}
}
func TestFit(t *testing.T) {
	img := &mermaid.Image{Width: 1600, Height: 1200}
	for _, tc := range []struct{ w, h, c, r int }{{80, 0, 80, 30}, {80, 20, 54, 20}, {1, 0, 1, 1}, {40, 0, 40, 15}} {
		c, r := Fit(img, tc.w, tc.h, 10, 20)
		if c != tc.c || r != tc.r {
			t.Errorf("fit %dx%d = %dx%d, want %dx%d", tc.w, tc.h, c, r, tc.c, tc.r)
		}
	}
}
func TestFrameUploadPlacementClippingAndCleanup(t *testing.T) {
	var out bytes.Buffer
	w := NewWriter(&out)
	img := fixture()
	id := w.ID(img)
	row := w.Row(id, img, 80, 40, 30, 12, 70)
	if ansi.StringWidth(row) != 58 {
		t.Fatalf("placeholder width=%d", ansi.StringWidth(row))
	}
	cell := string([]rune{kitty.Placeholder, kitty.Diacritic(30), kitty.Diacritic(12), kitty.Diacritic((id >> 24) & 255)})
	if !strings.Contains(row, cell) {
		t.Fatal("missing explicit row, column or high image byte")
	}
	if out.Len() != 0 {
		t.Fatal("image commands written outside frame")
	}
	_, err := w.Write([]byte("before" + row + "after"))
	if err != nil {
		t.Fatal(err)
	}
	first := out.String()
	if strings.Contains(first, "777;mdview") || !strings.Contains(first, "f=100") || !strings.Contains(first, "U=1") || !strings.Contains(first, base64.StdEncoding.EncodeToString(img.PNG)) {
		t.Fatalf("invalid upload %q", first)
	}
	if !strings.HasPrefix(first, "before") || !strings.HasSuffix(first, "after") {
		t.Fatal("lost terminal frame")
	}
	out.Reset()
	_, _ = w.Write([]byte(row))
	if strings.Contains(out.String(), "\x1b_G") {
		t.Fatal("same geometry reuploaded")
	}
	out.Reset()
	_, _ = w.Write([]byte(w.Row(id, img, 40, 20, 19, 0, 40)))
	if strings.Contains(out.String(), "f=100") || !strings.Contains(out.String(), "U=1") {
		t.Fatal("resize must only replace placement")
	}
	out.Reset()
	w.Use(nil)
	if out.Len() != 0 {
		t.Fatal("deletion bypassed output serialization")
	}
	_, _ = w.Write([]byte("next"))
	if !strings.Contains(out.String(), fmt.Sprintf("i=%d", id)) || !strings.Contains(out.String(), "d=I") {
		t.Fatal("unused terminal image not deleted")
	}
	out.Reset()
	id2 := w.ID(img)
	_, _ = w.Write([]byte(w.Row(id2, img, 40, 20, 0, 0, 40)))
	out.Reset()
	_ = w.Close()
	if !strings.Contains(out.String(), fmt.Sprintf("i=%d", id2)) {
		t.Fatal("close leaked image")
	}
}
func TestTiledHorizontalPanAndOverlay(t *testing.T) {
	w := NewWriter(&bytes.Buffer{})
	img := fixture()
	id := w.ID(img)
	row := w.Row(id, img, 800, 600, 511, 250, 280)
	if ansi.StringWidth(row) != 30 || strings.Count(row, "777;mdview") != 2 {
		t.Fatal("missing tile boundary")
	}
	// ANSI-aware cuts must retain the encoded foreground and underline colors,
	// and explicit coordinates even when the first cells are overwritten.
	clipped := ansi.Cut(row, 12, 30)
	if !strings.Contains(clipped, string([]rune{kitty.Placeholder, kitty.Diacritic(255), kitty.Diacritic(6), kitty.Diacritic((id >> 24) & 255)})) {
		t.Fatal("pan/overlay lost coordinates")
	}
	if !strings.Contains(clipped, "38;2") || !strings.Contains(clipped, "58;2") {
		t.Fatal("overlay lost encoded IDs")
	}
}
func TestConcurrentOutputAndRegistration(t *testing.T) {
	w := NewWriter(&bytes.Buffer{})
	defer w.Close()
	img := fixture()
	id := w.ID(img)
	row := w.Row(id, img, 20, 10, 0, 0, 20)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_, _ = w.Write([]byte(row))
				w.ID(img)
				w.Use([]*mermaid.Image{img})
			}
		}()
	}
	wg.Wait()
}

func TestTilesUploadCroppedPNGsNotIgnoredSourceRectangles(t *testing.T) {
	var out bytes.Buffer
	w := NewWriter(&out)
	img := fixture()
	id := w.ID(img)
	row := w.Row(id, img, 800, 400, 300, 300, 320)
	if _, err := w.Write([]byte(row)); err != nil {
		t.Fatal(err)
	}
	stream := out.String()
	start := strings.Index(stream, ";") + 1
	end := strings.Index(stream, "\x1b\\")
	data, err := base64.StdEncoding.DecodeString(stream[start:end])
	if err != nil {
		t.Fatal(err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != 38 || config.Height != 22 {
		t.Fatalf("tile=%dx%d; virtual placements require cropped PNGs", config.Width, config.Height)
	}
	if strings.Contains(stream, "x=") || strings.Contains(stream, "w=") {
		t.Fatal("virtual placement incorrectly relies on crop options")
	}
	out.Reset()
	w.Use(nil)
	_, _ = w.Write(nil)
	if strings.Count(out.String(), "d=I") != 2 {
		t.Fatal("parent and tile images must both be cleaned up")
	}
}

func TestDeleteOwnedResourcesBeforeLeavingAlternateScreen(t *testing.T) {
	var out bytes.Buffer
	w := NewWriter(&out)
	img := fixture()
	id := w.ID(img)
	_, _ = w.Write([]byte(w.Row(id, img, 40, 10, 0, 0, 40)))
	out.Reset()
	_, _ = w.Write([]byte("\x1b[?1049l"))
	if strings.Index(out.String(), "d=I") > strings.Index(out.String(), "\x1b[?1049l") || !strings.Contains(out.String(), "d=I") {
		t.Fatal("cleanup occurred in wrong terminal screen")
	}
	if w.terminalBytes != 0 {
		t.Fatal("terminal resource accounting not reset")
	}
	// A terminal restore can upload the same source again.
	out.Reset()
	_, _ = w.Write([]byte(w.Row(id, img, 40, 10, 0, 0, 40)))
	if !strings.Contains(out.String(), "f=100") {
		t.Fatal("restore used deleted image")
	}
}

func TestInlineNaturalSizeAndWidthLimit(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		pw, ph, width, cols, rows int
	}{
		{"narrow class", 250, 624, 120, 13, 16},
		{"same class narrow terminal", 250, 624, 20, 13, 16},
		{"wide gantt shrinks", 3168, 296, 80, 80, 4},
		{"nested available width", 1600, 800, 36, 36, 9},
		{"tall chart remains scrollable", 400, 4000, 100, 20, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := &mermaid.Image{Width: tc.pw, Height: tc.ph}
			cols, rows := FitInline(img, tc.width, 10, 20)
			if cols != tc.cols || rows != tc.rows {
				t.Fatalf("got %dx%d, want %dx%d", cols, rows, tc.cols, tc.rows)
			}
		})
	}
	img := &mermaid.Image{Width: 250, Height: 624}
	cols, rows := Fit(img, 120, 40, 10, 20)
	if cols <= 13 || rows != 40 {
		t.Fatal("dedicated viewer must still fit and magnify the diagram")
	}
}
