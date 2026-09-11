// mdview renders Markdown in the terminal: an interactive pager when
// attached to a TTY, plain styled output when piped.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"golang.org/x/term"

	"mdview/render"
	"mdview/tui"
)

func main() {
	var (
		width  = flag.Int("w", 0, "content width (0 = terminal width, capped at -max)")
		maxW   = flag.Int("max", 0, "maximum content width in the pager (0 = full terminal width)")
		theme  = flag.String("theme", "mocha", "colour theme: mocha|dark, latte|light")
		pager  = flag.String("pager", "auto", "use the interactive pager: auto|always|never")
		color  = flag.String("color", "auto", "colour output: auto|always|never")
		noURLs = flag.Bool("no-urls", false, "hide link and image destinations")
		bigH   = flag.String("big-headings", "auto", "scale H1–H3 with kitty's text sizing protocol: auto|on|off")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: mdview [flags] [FILE|-]\n\n")
		fmt.Fprintf(os.Stderr, "Renders Markdown. Reads FILE, or stdin when FILE is - or omitted.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	th, ok := render.ByName(*theme)
	if !ok {
		fatalf("unknown theme %q (try mocha or latte)", *theme)
	}

	switch *color {
	case "always":
		lipgloss.SetColorProfile(termenv.TrueColor)
	case "never":
		lipgloss.SetColorProfile(termenv.Ascii)
	case "auto":
	default:
		fatalf("unknown --color %q", *color)
	}

	path, src := readInput(flag.Arg(0))

	stdoutTTY := term.IsTerminal(int(os.Stdout.Fd()))

	r := render.New(th)
	r.NoURLs = *noURLs
	switch *bigH {
	case "on":
		r.BigHeadings = true
	case "off":
	case "auto":
		// tmux/screen drop OSC 66 (and the heading text with it), so require
		// a direct kitty TERM, not just an inherited KITTY_WINDOW_ID.
		r.BigHeadings = stdoutTTY && strings.HasPrefix(os.Getenv("TERM"), "xterm-kitty")
	default:
		fatalf("unknown --big-headings %q", *bigH)
	}
	usePager := false
	switch *pager {
	case "auto":
		usePager = stdoutTTY
	case "always":
		usePager = true
	case "never":
	default:
		fatalf("unknown --pager %q", *pager)
	}

	if !usePager {
		w := *width
		if w == 0 {
			w = 80
			if stdoutTTY {
				if tw, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
					w = tw
				}
			}
		}
		out := r.Render(src, w)
		if !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		fmt.Fprint(os.Stdout, out)
		return
	}

	maxWidth := *maxW
	if *width > 0 {
		maxWidth = *width
	}
	m := tui.New(path, src, r, maxWidth)

	opts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithMouseCellMotion()}
	if path == "" {
		// stdin held the document; take keystrokes from the controlling tty.
		tty, err := os.Open("/dev/tty")
		if err != nil {
			fatalf("cannot open /dev/tty for input: %v", err)
		}
		defer tty.Close()
		opts = append(opts, tea.WithInput(tty))
	}
	if _, err := tea.NewProgram(m, opts...).Run(); err != nil {
		fatalf("%v", err)
	}
}

// readInput returns the file path (empty for stdin) and its contents.
func readInput(arg string) (string, []byte) {
	if arg == "" || arg == "-" {
		if term.IsTerminal(int(os.Stdin.Fd())) {
			flag.Usage()
			os.Exit(2)
		}
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fatalf("reading stdin: %v", err)
		}
		return "", data
	}
	data, err := os.ReadFile(arg)
	if err != nil {
		fatalf("%v", err)
	}
	return arg, data
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "mdview: "+format+"\n", args...)
	os.Exit(1)
}
