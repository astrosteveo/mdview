# mdview

Your own damn Markdown viewer. A single Go binary that renders Markdown in
the terminal — an interactive pager when attached to a TTY, plain styled
output when piped.

- Syntax-highlighted code blocks, GFM tables, task lists, nested quotes
- Clickable links: follow `.md` links in place, jump to `#headings`, open URLs in the browser
- Search, mouse selection with copy, live reload while you edit
- Catppuccin Mocha and Latte themes; 2x headings on kitty
- No config files, no daemon, no JavaScript — one static binary

```
mdview README.md            # interactive pager
mdview -theme latte doc.md  # light palette
cat notes.md | mdview       # stdin (still gets the pager)
mdview -pager never doc.md  # print and exit
mdview -color always x.md | less -R
```

## Install

**Requires Go 1.27 or newer** ([download](https://go.dev/dl/)). No other
dependencies.

### One-liner

```sh
go install github.com/astrosteveo/mdview@latest
```

This drops an `mdview` binary in `$(go env GOPATH)/bin` (usually
`~/go/bin`). If `mdview` isn't found afterwards, add that directory to your
`PATH`:

```sh
# bash: ~/.bashrc    zsh: ~/.zshrc    fish: fish_add_path ~/go/bin
export PATH="$HOME/go/bin:$PATH"
```

### From source

```sh
git clone https://github.com/astrosteveo/mdview.git
cd mdview
go build -o mdview .
sudo install -m 755 mdview /usr/local/bin/   # or: mv mdview ~/.local/bin/
```

### Check it works

```sh
mdview -h
mdview README.md
```

### Optional extras

- **Clipboard**: `y` and the right-click menu use `wl-copy` (Wayland),
  `xclip`/`xsel` (X11) or `pbcopy` (macOS) when installed; without them
  mdview falls back to the terminal's own clipboard (OSC 52), which most
  modern terminals support.
- **Opening URLs**: external links are handed to `xdg-open` (Linux) — make
  sure your desktop has a default browser set.
- **Big headings**: need [kitty](https://sw.kovidgoyal.net/kitty/) ≥ 0.40.
  Everything else works in any terminal with 256-colour or truecolor support.

### Upgrade / uninstall

```sh
go install github.com/astrosteveo/mdview@latest   # upgrade
rm "$(go env GOPATH)/bin/mdview"                   # uninstall
```

## What it renders

Headings (with rules), paragraphs with **bold**/*italic*/~~strike~~/`code`,
links (text + dim URL), images, bullet/ordered/task lists with hanging
indents, nested blockquotes, syntax-highlighted fenced code (Chroma, ~250
languages), GFM tables with alignment, thematic breaks, raw HTML (dimmed).

## Keys

| Key            | Action                |
|----------------|-----------------------|
| `j`/`k`, arrows, wheel | scroll        |
| `d`/`u`        | half page             |
| `space`/`f`, `b` | full page           |
| `g`/`G`        | top / bottom          |
| `/` … `enter`  | search (case-insensitive) |
| `n`/`N`        | next / previous match |
| `r`            | reload file           |
| `tab` / `shift+tab` | focus next / previous link (shows its destination) |
| `enter`        | follow the focused link |
| `y`            | copy the selection    |
| `esc`, `backspace`, click `✕` | clear focus/selection, then back to the previous document |
| `q`            | quit (`esc` also quits at the root) |

Mouse: hover a link for a tooltip with its destination, click to follow it,
drag to select text (double-click selects a word), right-click for a menu
(open/copy link, copy selection, back, reload, quit). Copying uses
`wl-copy`/`xclip`/`xsel`/`pbcopy` when present, otherwise the terminal's
own clipboard (OSC 52).

The file is polled every 500 ms and re-rendered on change, so it works as a
live preview beside your editor.

## Links

Destinations are not printed inline in the pager; hover or focus a link to
see them (`-inline-urls` restores the old look). Left-click any link. A
relative or `file://` path to a `.md` file opens in place, stacked on the current document — a breadcrumb bar with a `✕`
appears at the top, and `esc` returns you to where you were. `#heading`
anchors (GitHub-style slugs) scroll to the heading, including
`other.md#section`. Anything else — `http(s)`, `mailto:`, images, non-markdown
files — is handed to `xdg-open`. Absolute URLs are also emitted as OSC 8
hyperlinks, so kitty's own ctrl+shift+click works too. Links inside tables
are not clickable (the table layout decides their final position).

## Flags

```
-w N           content width (0 = terminal width, capped at -max)
-max N         max content width in the pager (default 0 = full terminal width)
-theme NAME    mocha|dark (default) or latte|light
-pager MODE    auto|always|never
-color MODE    auto|always|never
-no-urls       hide link/image destinations in printed output
-inline-urls   show destinations inline in the pager too (default: tooltips)
-big-headings  auto|on|off — scale H1/H2/H3 to 2x/1.5x/1.25x using kitty's
               text sizing protocol (auto = on when TERM is xterm-kitty)
```

Big headings work in kitty ≥ 0.40 only; other terminals get bold, coloured
1x headings. tmux strips the escape (and the heading text with it), so
`auto` stays off there — pass `-big-headings on` only on a terminal that
supports OSC 66.

## Development

```sh
go build -o mdview .   # build
go test ./...          # run the test suite
go vet ./...
```

`testdata/sample.md` exercises every construct the renderer supports —
`mdview testdata/sample.md` is a quick visual smoke test.

## How it works

`render/` parses with [goldmark](https://github.com/yuin/goldmark) (GFM
extensions on) and walks the AST itself. Inline content becomes a list of
`(text, attrs)` spans that are word-wrapped *before* any ANSI is emitted, so
styles survive line breaks and container indentation. Containers (lists,
quotes) render their children at a narrower width and prefix each line.
Code blocks go through Chroma's tokenizer and get per-token colours from the
theme's Chroma style. Tables use `lipgloss/table` for column sizing.

`tui/` is a [Bubble Tea](https://github.com/charmbracelet/bubbletea)
viewport with a status bar, search over ANSI-stripped lines, and an mtime
poll for live reload. Themes live in `render/theme.go`.

## License

[MIT](LICENSE)
