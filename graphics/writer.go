// Package graphics serializes Kitty image commands with Bubble Tea's frame writes.
package graphics

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"math"
	"math/rand/v2"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/astrosteveo/mdview/mermaid"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
)

type placement struct {
	imageID  int
	tile     bool
	cost     int
	id       int
	used     uint64
	uploaded bool
}

type stored struct {
	decoded       image.Image
	used          uint64
	image         *mermaid.Image
	uploaded      bool
	placements    map[string]*placement
	nextPlacement int
}
type Writer struct {
	clock         uint64
	mu            sync.Mutex
	out           io.Writer
	images        map[int]*stored
	ids           map[*mermaid.Image]int
	retired       []int
	next          uint32
	terminalBytes int
}

func NewWriter(out io.Writer) *Writer {
	return &Writer{out: out, images: map[int]*stored{}, ids: map[*mermaid.Image]int{}, next: rand.Uint32() | 0x01000000}
}

// Use drops image data no longer used by the current document. Actual terminal
// deletion happens on the output goroutine, never concurrently with a frame.
func (w *Writer) Use(images []*mermaid.Image) {
	w.mu.Lock()
	defer w.mu.Unlock()
	active := map[*mermaid.Image]bool{}
	for _, img := range images {
		active[img] = true
	}
	for img, id := range w.ids {
		if !active[img] {
			w.retired = append(w.retired, id)
			if w.images[id].uploaded {
				w.terminalBytes -= img.Width * img.Height * 4
			}
			for _, plan := range w.images[id].placements {
				if plan.tile && plan.uploaded {
					w.retired = append(w.retired, plan.imageID)
					w.terminalBytes -= plan.cost
				}
			}
			delete(w.images, id)
			delete(w.ids, img)
		}
	}
}
func (w *Writer) ID(img *mermaid.Image) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if id, ok := w.ids[img]; ok {
		return id
	}
	w.next++
	if w.next == 0 {
		w.next++
	}
	id := int(w.next)
	w.ids[img] = id
	w.images[id] = &stored{image: img, placements: map[string]*placement{}}
	return id
}

// Row uses 256-cell tiles so every coordinate has a representable diacritic,
// including very tall diagrams and zoomed views. All cells encode all 3 values.
func (w *Writer) Row(id int, img *mermaid.Image, cols, rows, row, from, to int) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	item := w.images[id]
	if item == nil {
		return ""
	}
	var out strings.Builder
	for col := from; col < to; {
		tx, ty := col/256*256, row/256*256
		cw, rh := min(256, cols-tx), min(256, rows-ty)
		x, y := tx*img.Width/cols, ty*img.Height/rows
		right, bottom := (tx+cw)*img.Width/cols, (ty+rh)*img.Height/rows
		spec := fmt.Sprintf("%d;%d;%d;%d;%d;%d;%d", id, cw, rh, x, y, max(1, right-x), max(1, bottom-y))
		plan := item.placements[spec]
		if plan == nil {
			item.nextPlacement++
			plan = &placement{id: item.nextPlacement, imageID: id}
			if x != 0 || y != 0 || right != img.Width || bottom != img.Height {
				w.next++
				if w.next == 0 {
					w.next++
				}
				plan.imageID = int(w.next)
				plan.tile = true
				plan.cost = max(1, right-x) * max(1, bottom-y) * 4
			}
			item.placements[spec] = plan
		}
		w.clock++
		plan.used = w.clock
		if !plan.tile {
			item.used = w.clock
		}
		pid := plan.id
		spec += fmt.Sprintf(";%d", pid)

		fmt.Fprintf(&out, "\x1b]777;mdview;%s\x1b\\\x1b[0;38;2;%d;%d;%dm\x1b[58;2;%d;%d;%dm", spec, (plan.imageID>>16)&255, (plan.imageID>>8)&255, plan.imageID&255, (pid>>16)&255, (pid>>8)&255, pid&255)
		end := min(to, tx+cw)
		for ; col < end; col++ {
			out.WriteRune(kitty.Placeholder)
			out.WriteRune(kitty.Diacritic(row - ty))
			out.WriteRune(kitty.Diacritic(col - tx))
			out.WriteRune(kitty.Diacritic((plan.imageID >> 24) & 255))
		}
		out.WriteString("\x1b[0m")
	}
	return out.String()
}

var marker = regexp.MustCompile("\x1b\\]777;mdview;([0-9;]+)\x1b\\\\")

func command(o kitty.Options) string { return ansi.KittyGraphics(nil, o.Options()...) }
func deletion(id int) string {
	return command(kitty.Options{Action: kitty.Delete, Delete: kitty.DeleteID, DeleteResources: true, ID: id, Quiet: 2})
}

func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out bytes.Buffer
	for _, id := range w.retired {
		out.WriteString(deletion(id))
	}
	w.retired = nil
	// Resolve frame-local markers here: image uploads and placements cannot race
	// frame output, and skipped/overwritten Bubble Tea frames upload nothing.
	data := marker.ReplaceAllStringFunc(string(p), func(token string) string {
		spec := marker.FindStringSubmatch(token)[1]
		parts := strings.Split(spec, ";")
		if len(parts) != 8 {
			return ""
		}
		v := make([]int, 8)
		for i, s := range parts {
			v[i], _ = strconv.Atoi(s)
		}
		item := w.images[v[0]]
		if item == nil {
			return ""
		}
		var b strings.Builder
		plan := item.placements[strings.Join(parts[:7], ";")]
		if plan == nil {
			return ""
		}
		if plan.tile && !plan.uploaded {
			// Kitty's Unicode placements ignore x/y/w/h. Crop real PNG tiles,
			// rather than relying on source rectangles that only normal placements use.
			if item.decoded == nil {
				item.decoded, _ = png.Decode(bytes.NewReader(item.image.PNG))
			}
			if item.decoded == nil {
				return ""
			}
			crop := image.Rect(v[3], v[4], v[3]+v[5], v[4]+v[6]).Intersect(item.decoded.Bounds())
			var tile image.Image
			if sub, ok := item.decoded.(interface {
				SubImage(image.Rectangle) image.Image
			}); ok {
				tile = sub.SubImage(crop)
			} else {
				copied := image.NewNRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
				draw.Draw(copied, copied.Bounds(), item.decoded, crop.Min, draw.Src)
				tile = copied
			}
			var data bytes.Buffer
			if png.Encode(&data, tile) != nil {
				return ""
			}
			transmit(&b, plan.imageID, data.Bytes())
			w.terminalBytes += plan.cost
		} else if !plan.tile && !item.uploaded {
			transmit(&b, v[0], item.image.PNG)
			item.uploaded = true
			w.terminalBytes += item.image.Width * item.image.Height * 4
		}
		if !plan.uploaded {
			b.WriteString(command(kitty.Options{Action: kitty.Put, ID: plan.imageID, PlacementID: plan.id, Quiet: 2, VirtualPlacement: true, Columns: v[1], Rows: v[2], DoNotMoveCursor: true}))
			plan.uploaded = true
		}

		return b.String()
	})
	if strings.Contains(data, "\x1b[?1049l") {
		var cleanup strings.Builder
		for id, item := range w.images {
			if item.uploaded {
				cleanup.WriteString(deletion(id))
				item.uploaded = false
			}
			for _, plan := range item.placements {
				if plan.tile && plan.uploaded {
					cleanup.WriteString(deletion(plan.imageID))
				}
				plan.uploaded = false
			}
		}
		w.terminalBytes = 0
		data = strings.ReplaceAll(data, "\x1b[?1049l", cleanup.String()+"\x1b[?1049l")
	}
	out.WriteString(data)
	// Retain a bounded set of recent virtual placements across resize/zoom.
	// Frame markers name exact placements; evicted geometry is recreated on use.
	for id, item := range w.images {
		for len(item.placements) > 1024 {
			oldest := ""
			age := ^uint64(0)
			for spec, plan := range item.placements {
				if plan.used < age {
					oldest, age = spec, plan.used
				}
			}
			plan := item.placements[oldest]
			if plan.uploaded {
				if plan.tile {
					out.WriteString(deletion(plan.imageID))
					w.terminalBytes -= plan.cost
				} else {
					out.WriteString(command(kitty.Options{Action: kitty.Delete, Delete: kitty.DeleteID, ID: id, PlacementID: plan.id, Quiet: 2}))
				}
			}
			delete(item.placements, oldest)
		}
	}
	// Bound terminal pixel resources too. Recently visible geometry wins over
	// older inline/viewer sizes; evicted data is uploaded again when revisited.
	for w.terminalBytes > mermaid.Budget {
		age := ^uint64(0)
		imageID := 0
		var victim *placement
		var parent *stored
		for id, item := range w.images {
			if item.uploaded && item.used < age {
				age = item.used
				imageID = id
				parent = item
				victim = nil
			}
			for _, plan := range item.placements {
				if plan.tile && plan.uploaded && plan.used < age {
					age = plan.used
					imageID = plan.imageID
					parent = item
					victim = plan
				}
			}
		}
		if imageID == 0 {
			break
		}
		out.WriteString(deletion(imageID))
		if victim != nil {
			victim.uploaded = false
			w.terminalBytes -= victim.cost
		} else {
			parent.uploaded = false
			w.terminalBytes -= parent.image.Width * parent.image.Height * 4
			for _, plan := range parent.placements {
				if !plan.tile {
					plan.uploaded = false
				}
			}
		}
	}

	_, err := w.out.Write(out.Bytes())
	if err != nil {
		return 0, err
	}
	return len(p), nil
}
func transmit(b *strings.Builder, id int, pngData []byte) {
	// PNG is streamed in protocol-sized base64 chunks, never via filesystem paths.
	for start := 0; start < len(pngData); start += 3072 {
		end := min(start+3072, len(pngData))
		more := 0
		if end < len(pngData) {
			more = 1
		}
		opts := []string{fmt.Sprintf("m=%d", more), "q=2"}
		if start == 0 {
			opts = append(opts, "a=t", "f=100", fmt.Sprintf("i=%d", id))
		}
		payload := base64.StdEncoding.EncodeToString(pngData[start:end])
		b.WriteString(ansi.KittyGraphics([]byte(payload), opts...))
	}
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	var b strings.Builder
	for id, item := range w.images {
		b.WriteString(deletion(id))
		for _, plan := range item.placements {
			if plan.tile && plan.uploaded {
				b.WriteString(deletion(plan.imageID))
			}
		}
	}
	for _, id := range w.retired {
		b.WriteString(deletion(id))
	}
	_, err := io.WriteString(w.out, b.String())
	w.images = map[int]*stored{}
	w.ids = map[*mermaid.Image]int{}
	w.retired = nil
	w.terminalBytes = 0
	return err
}

// FitInline shrinks diagrams to the available width without magnifying small
// charts. Mermaid's 2x raster is for sharpness; its natural display size is half
// its PNG dimensions. The dedicated viewer still fits the whole available area.
func FitInline(img *mermaid.Image, width int, cellW, cellH float64) (int, int) {
	scale := math.Min(1/float64(mermaid.RasterScale), float64(max(1, width))*cellW/float64(img.Width))
	return max(1, int(math.Ceil(float64(img.Width)*scale/cellW-1e-9))), max(1, int(math.Ceil(float64(img.Height)*scale/cellH-1e-9)))
}

// Fit preserves aspect ratio in physical pixels, not square character cells.
func Fit(img *mermaid.Image, width, height int, cellW, cellH float64) (int, int) {
	scale := float64(max(1, width)) * cellW / float64(img.Width)
	if height > 0 {
		scale = math.Min(scale, float64(height)*cellH/float64(img.Height))
	}
	return max(1, int(math.Ceil(float64(img.Width)*scale/cellW-1e-9))), max(1, int(math.Ceil(float64(img.Height)*scale/cellH-1e-9)))
}

// Fd preserves Bubble Tea's terminal sizing and raw-mode detection.
func (w *Writer) Fd() uintptr {
	if f, ok := w.out.(interface{ Fd() uintptr }); ok {
		return f.Fd()
	}
	return ^uintptr(0)
}

func (w *Writer) Read(p []byte) (int, error) {
	if r, ok := w.out.(io.Reader); ok {
		return r.Read(p)
	}
	return 0, io.EOF
}
