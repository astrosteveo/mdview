// Package mermaid runs the optional, official Mermaid CLI. It never installs software.
package mermaid

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const Budget = 128 << 20

// RasterScale is the PNG pixel density, not the intended display magnification.
const RasterScale = 2
const Settings = "viewport=1600;scale=2;palette=v1"

type Image struct {
	PNG           []byte
	Width, Height int
}

func (i *Image) Cost() int { return len(i.PNG) + i.Width*i.Height*4 }

type RenderFunc func(context.Context, string, string) (*Image, error)

type CLI struct {
	Path, Version string
	Timeout       time.Duration
}

func Discover() (*CLI, error) {
	path, err := exec.LookPath("mmdc")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := command(ctx, path, "--version")
	var output cappedBuffer
	cmd.Stdout, cmd.Stderr = &output, &output
	err = cmd.Run()
	if err != nil {
		return nil, err
	}
	return &CLI{path, strings.TrimSpace(output.String()), 30 * time.Second}, nil
}

func (c *CLI) Render(parent context.Context, source, theme string) (*Image, error) {
	ctx, cancel := context.WithTimeout(parent, c.Timeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "mdview-mermaid-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	bg, text, surface, accent := "#1e1e2e", "#cdd6f4", "#313244", "#cba6f7"
	if theme == "latte" {
		bg, text, surface, accent = "#eff1f5", "#4c4f69", "#ccd0da", "#8839ef"
	}
	cfg := map[string]any{"theme": "base", "securityLevel": "strict", "themeVariables": map[string]any{
		"darkMode": theme != "latte", "background": bg, "primaryColor": surface, "primaryTextColor": text,
		"primaryBorderColor": accent, "lineColor": text, "secondaryColor": surface, "tertiaryColor": bg,
		"textColor": text, "mainBkg": surface, "nodeBorder": accent, "clusterBkg": bg, "clusterBorder": accent,
		"titleColor": text, "edgeLabelBackground": bg, "actorBkg": surface, "actorTextColor": text,
		"actorBorder": accent, "signalColor": text, "signalTextColor": text, "labelTextColor": text,
	}}
	config, _ := json.Marshal(cfg)
	browserConfig, _ := json.Marshal(map[string]any{"userDataDir": filepath.Join(dir, "browser")})
	browserConf := filepath.Join(dir, "puppeteer.json")
	input, output, conf := filepath.Join(dir, "input.mmd"), filepath.Join(dir, "output.png"), filepath.Join(dir, "config.json")
	for path, data := range map[string][]byte{input: []byte(source), conf: config, browserConf: browserConfig} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			return nil, err
		}
	}
	cmd := command(ctx, c.Path, "-i", input, "-o", output, "-c", conf, "-p", browserConf, "-b", bg, "-w", "1600", "-s", fmt.Sprint(RasterScale), "-q")
	cmd.Dir = dir
	var log cappedBuffer
	cmd.Stdout, cmd.Stderr = &log, &log
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("mermaid: %w: %s", err, log.String())
	}
	f, err := os.Open(output)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() > Budget {
		return nil, fmt.Errorf("image exceeds cache budget")
	}
	cfgPNG, err := png.DecodeConfig(f)
	if err != nil {
		return nil, err
	}
	if cfgPNG.Width <= 0 || cfgPNG.Height <= 0 || cfgPNG.Width > 10000 || cfgPNG.Height > 10000 || int64(cfgPNG.Width)*int64(cfgPNG.Height)*4+info.Size() > Budget {
		return nil, fmt.Errorf("image exceeds Kitty dimensions or cache budget")
	}
	data, err := os.ReadFile(output)
	if err != nil {
		return nil, err
	}
	return &Image{data, cfgPNG.Width, cfgPNG.Height}, nil
}

type cappedBuffer struct{ bytes.Buffer }

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len() < 8192 {
		_, _ = b.Buffer.Write(p[:min(len(p), 8192-b.Len())])
	}
	return n, nil
}

type result struct {
	revision uint64
	key      string
	image    *Image
	err      error
}
type entry struct {
	image *Image
	used  uint64
}

// Session is owned by the UI goroutine. Only workers use the result channel.
type Session struct {
	render         RenderFunc
	version, theme string
	revision       uint64
	signature      string
	cancel         context.CancelFunc
	done           chan result
	slots          chan struct{}
	wg             sync.WaitGroup
	cache          map[string]entry
	active         map[string]bool
	used           uint64
	bytes, budget  int
	closed         bool
}

func New(render RenderFunc, version, theme string) *Session {
	return &Session{render: render, version: version, theme: theme, done: make(chan result, 2), slots: make(chan struct{}, 2), cache: map[string]entry{}, budget: Budget}
}
func (s *Session) key(src string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(s.version+"\x00"+s.theme+"\x00"+Settings+"\x00"+src)))
}

// Sync cancels obsolete work, deduplicates repeated blocks, and starts at most two jobs.
func (s *Session) Sync(document string, sources []string) {
	if s.closed || document == s.signature {
		return
	}
	s.signature = document
	s.revision++
	if s.cancel != nil {
		s.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.active = map[string]bool{}
	var jobs []string
	for _, src := range sources {
		k := s.key(src)
		if s.active[k] {
			continue
		}
		s.active[k] = true
		if _, ok := s.cache[k]; !ok {
			jobs = append(jobs, src)
		}
	}
	// Bound negative-cache metadata too.
	if len(s.cache) > 1024 {
		for k, e := range s.cache {
			if !s.active[k] && e.image == nil {
				delete(s.cache, k)
			}
		}
	}
	rev := s.revision
	for worker := 0; worker < min(2, len(jobs)); worker++ {
		s.wg.Add(1)
		go func(start int) {
			defer s.wg.Done()
			for n := start; n < len(jobs); n += 2 {
				select {
				case s.slots <- struct{}{}:
				case <-ctx.Done():
					return
				}
				if ctx.Err() != nil {
					<-s.slots
					return
				}
				img, err := s.render(ctx, jobs[n], s.theme)
				<-s.slots
				select {
				case s.done <- result{rev, s.key(jobs[n]), img, err}:
				case <-ctx.Done():
					return
				}
			}
		}(worker)
	}
}
func (s *Session) Poll() bool {
	changed := false
	for {
		select {
		case r := <-s.done:
			if r.revision != s.revision {
				continue
			}
			if r.err != nil {
				r.image = nil
			}
			if r.image != nil {
				cost := r.image.Cost()
				for s.bytes+cost > s.budget {
					oldest := ""
					var age uint64 = ^uint64(0)
					for k, e := range s.cache {
						if !s.active[k] && e.image != nil && e.used < age {
							oldest, age = k, e.used
						}
					}
					if oldest == "" {
						break
					}
					s.bytes -= s.cache[oldest].image.Cost()
					delete(s.cache, oldest)
				}
				if s.bytes+cost > s.budget {
					r.image = nil
				} else {
					s.bytes += cost
					changed = true
				}
			}
			s.used++
			s.cache[r.key] = entry{r.image, s.used}
		default:
			return changed
		}
	}
}
func (s *Session) Get(source string) *Image {
	k := s.key(source)
	e, ok := s.cache[k]
	if !ok {
		return nil
	}
	s.used++
	e.used = s.used
	s.cache[k] = e
	return e.image
}
func (s *Session) Close() {
	if s.closed {
		return
	}
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
	s.cache = nil
	for {
		select {
		case <-s.done:
		default:
			return
		}
	}
}
