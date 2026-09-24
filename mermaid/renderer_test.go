package mermaid

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func waitFor(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for worker")
}
func tinyImage() *Image {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 20, 10)))
	return &Image{b.Bytes(), 20, 10}
}

func TestSessionDedupReuseFailureAndCacheKeys(t *testing.T) {
	var calls atomic.Int32
	s := New(func(_ context.Context, src, theme string) (*Image, error) {
		calls.Add(1)
		if src == "bad" {
			return nil, errors.New("parse error")
		}
		return tinyImage(), nil
	}, "11.17.0", "mocha")
	defer s.Close()
	s.Sync("doc1", []string{"a", "a", "bad"})
	waitFor(t, func() bool { s.Poll(); return len(s.cache) == 2 })
	if calls.Load() != 2 || s.Get("a") == nil || s.Get("bad") != nil {
		t.Fatal("dedup or silent error failed")
	}
	s.Sync("doc2", []string{"bad", "a"})
	s.wg.Wait()
	s.Poll()
	if calls.Load() != 2 {
		t.Fatal("cached diagrams rerendered")
	}
	other := New(nil, "11.17.1", "mocha")
	light := New(nil, "11.17.0", "latte")
	if s.key("a") == other.key("a") || s.key("a") == light.key("a") {
		t.Fatal("version/theme absent from cache key")
	}
}

func TestSessionCancellationConcurrencyAndStaleResults(t *testing.T) {
	var running, peak, canceled atomic.Int32
	entered := make(chan string, 10)
	s := New(func(ctx context.Context, src, _ string) (*Image, error) {
		n := running.Add(1)
		defer running.Add(-1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		entered <- src
		if src == "old1" || src == "old2" {
			<-ctx.Done()
			canceled.Add(1)
			return tinyImage(), nil
		}
		return tinyImage(), nil
	}, "v", "mocha")
	defer s.Close()
	s.Sync("old", []string{"old1", "old2", "never"})
	<-entered
	<-entered
	oldRev := s.revision
	s.Sync("new", []string{"new"})
	waitFor(t, func() bool { s.Poll(); return s.Get("new") != nil })
	s.done <- result{oldRev, s.key("old1"), tinyImage(), nil}
	s.Poll()
	if peak.Load() > 2 || canceled.Load() != 2 || s.Get("old1") != nil || s.Get("never") != nil {
		t.Fatalf("concurrency=%d cancel=%d stale cached", peak.Load(), canceled.Load())
	}
}

func TestCacheEvictsOnlyUnusedAndStaysBounded(t *testing.T) {
	s := New(func(context.Context, string, string) (*Image, error) { return tinyImage(), nil }, "v", "mocha")
	defer s.Close()
	s.budget = tinyImage().Cost() * 2
	s.Sync("one", []string{"a", "b"})
	waitFor(t, func() bool { s.Poll(); return len(s.cache) == 2 })
	s.Get("b")
	s.Sync("two", []string{"b", "c"})
	waitFor(t, func() bool { s.Poll(); return s.Get("c") != nil })
	if s.Get("a") != nil || s.Get("b") == nil || s.bytes > s.budget {
		t.Fatal("unused LRU eviction failed")
	}
	s.Sync("three", []string{"b", "c", "d"})
	waitFor(t, func() bool { s.Poll(); _, ok := s.cache[s.key("d")]; return ok })
	if s.Get("d") != nil || s.Get("b") == nil || s.bytes > s.budget {
		t.Fatal("active cache must remain bounded")
	}
}

func TestCLITimeoutCancellationFailureAndCleanup(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	for _, mode := range []string{"timeout", "cancel", "failure"} {
		t.Run(mode, func(t *testing.T) {
			script := filepath.Join(dir, "fake-mmdc")
			body := "#!/bin/sh\nsleep 10 &\nwait\n"
			if mode == "failure" {
				body = "#!/bin/sh\necho malformed >&2\nexit 2\n"
			}
			if err := os.WriteFile(script, []byte(body), 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cli := CLI{Path: script, Timeout: 50 * time.Millisecond}
			if mode == "cancel" {
				cli.Timeout = time.Second
				cancel()
			}
			start := time.Now()
			img, err := cli.Render(ctx, "bad", "latte")
			if err == nil || img != nil || time.Since(start) > 2*time.Second {
				t.Fatalf("failed to stop: image=%v err=%v", img, err)
			}
			entries, _ := filepath.Glob(filepath.Join(dir, "mdview-mermaid-*"))
			if len(entries) != 0 {
				t.Fatalf("leaked %v", entries)
			}
		})
	}
}

func TestCLISuccessArgumentsAndPalette(t *testing.T) {
	dir := t.TempDir()
	fixture := filepath.Join(dir, "fixture.png")
	if err := os.WriteFile(fixture, tinyImage().PNG, 0600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "mmdc")
	argsFile := filepath.Join(dir, "args")
	configFile := filepath.Join(dir, "config")
	t.Setenv("FIXTURE", fixture)
	t.Setenv("ARGS", argsFile)
	t.Setenv("CONFIG", configFile)
	body := `#!/bin/sh
printf '%s\n' "$@" > "$ARGS"
while [ "$#" -gt 0 ]; do
 case "$1" in
 -o) shift; output="$1";;
 -c) shift; cp "$1" "$CONFIG";;
 esac
 shift
done
cp "$FIXTURE" "$output"
`
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	cli := CLI{Path: script, Timeout: time.Second}
	img, err := cli.Render(context.Background(), "flowchart LR\n A-->B", "latte")
	if err != nil || img.Width != 20 {
		t.Fatalf("%v %v", img, err)
	}
	args, _ := os.ReadFile(argsFile)
	cfg, _ := os.ReadFile(configFile)
	if !strings.Contains(string(args), "-w\n1600\n-s\n2\n") || !strings.Contains(string(cfg), "#eff1f5") || !strings.Contains(string(cfg), `"securityLevel":"strict"`) {
		t.Fatalf("arguments=%s config=%s", args, cfg)
	}
}

func TestDiscoverMissingRenderer(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := Discover(); err == nil {
		t.Fatal("missing renderer accepted")
	}
}

// Opt-in because normal Markdown use and tests require no Node installation.
func TestRealCLI(t *testing.T) {
	if os.Getenv("MDVIEW_TEST_MMDC") == "" {
		t.Skip("set MDVIEW_TEST_MMDC=1 to run real browser rendering")
	}
	cli, err := Discover()
	if err != nil {
		t.Fatal(err)
	}
	examples := map[string]string{
		"flowchart": "flowchart LR\n A[Hello 世界] --> B[Kitty]",
		"sequence":  "sequenceDiagram\n Alice->>Bob: Hello 世界\n Bob-->>Alice: Hi",
		"class":     "classDiagram\n Animal <|-- Duck\n Animal : +int age\n Duck : +quack()",
		"er":        "erDiagram\n CUSTOMER ||--o{ ORDER : places\n CUSTOMER {\n int id\n string name\n }",
		"state":     "stateDiagram-v2\n [*] --> Idle\n Idle --> Running\n Running --> [*]",
		"gantt":     "gantt\n title Build\n dateFormat YYYY-MM-DD\n section Work\n Design :a, 2026-09-24, 2d\n Ship :after a, 1d",
	}
	for name, src := range examples {
		t.Run(name, func(t *testing.T) {
			img, err := cli.Render(context.Background(), src, "mocha")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := png.Decode(bytes.NewReader(img.PNG)); err != nil {
				t.Fatal(err)
			}
			t.Logf("%s %dx%d %d PNG bytes", name, img.Width, img.Height, len(img.PNG))
		})
	}
	if _, err := cli.Render(context.Background(), "flowchart LR\n A --> [", "mocha"); err == nil {
		t.Fatal("malformed diagram accepted")
	}
	if _, err := cli.Render(context.Background(), examples["flowchart"], "latte"); err != nil {
		t.Fatal(err)
	}
}
