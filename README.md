# mdview

Your own damn Markdown viewer. A single Go binary that renders Markdown in
the terminal — an interactive pager when attached to a TTY, plain styled
output when piped.

- Syntax-highlighted code blocks, GFM tables, task lists, nested quotes
- Clickable links: follow `.md` links in place, jump to `#headings`, open URLs in the browser
- Search, mouse selection with copy, live reload while you edit
- Catppuccin Mocha and Latte themes; 2x headings on kitty
- Core Markdown rendering is a single Go binary; optional Mermaid diagrams use a local Node/browser renderer
- Inline Mermaid diagrams in Kitty, with zoom, pan, and source copy/search

```
mdview README.md            # interactive pager
mdview -theme latte doc.md  # light palette
cat notes.md | mdview       # stdin (still gets the pager)
mdview -pager never doc.md  # print and exit
mdview -color always x.md | less -R
```

## Install

**Building requires Go 1.27 or newer** ([download](https://go.dev/dl/)).
Ordinary Markdown rendering needs no external renderer. Mermaid graphics use
the optional dependencies below; opening links and copying may use desktop tools.

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
| `esc`, `backspace` | clear focus/selection, then back one document |
| Click `✕` | back one document |
| Click an earlier breadcrumb | return directly to that document |
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
appears at the top. Click any earlier document name to return directly to
it, restoring its scroll position and saved search and discarding the later
trail. The current document name is bold and inactive. When the trail is too
wide, the nearest ancestors stay visible and older entries appear in a
clickable `…` menu, ordered oldest to newest. Use the mouse or
`↑`/`↓` (`k`/`j`, `tab`/`shift+tab`) and `enter` to select; `esc` dismisses
the menu. Long menus scroll with the wheel or keyboard.

Click `✕` to go back one document, or use `esc`/`backspace` (which first
clear active focus or selection). Returning to the original document hides
the breadcrumb bar. `#heading`
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
-mermaid MODE  auto|off (default auto; graphics only in a supported Kitty pager)
-no-urls       hide link/image destinations in printed output
-inline-urls   show destinations inline in the pager too (default: tooltips)
-big-headings  auto|on|off — scale H1/H2/H3 to 2x/1.5x/1.25x using kitty's
               text sizing protocol (auto = on when TERM is xterm-kitty)
```

Big headings work in kitty ≥ 0.40 only; other terminals get bold, coloured
1x headings. tmux strips the escape (and the heading text with it), so
`auto` stays off there — pass `-big-headings on` only on a terminal that
supports OSC 66.

## Mermaid diagrams

Fenced `mermaid` blocks render asynchronously as inline images in a direct
[Kitty](https://sw.kovidgoyal.net/kitty/) session, version **0.28 or newer**.
Inline diagrams retain their natural size (accounting for the 2× PNG density)
and shrink when needed to fit the document width; narrow charts are not
stretched across the terminal. Tall diagrams still scroll with the document.
Click an image or use `tab` to focus its caption and `enter` to open the
full-screen diagram viewer. The document position and keyboard focus return
when the viewer closes; viewing diagrams does not add breadcrumbs.

| Viewer input | Action |
| --- | --- |
| `+` / `-` (or mouse wheel) | Zoom by 1.25, from 25% to 800% of the fitted size |
| Arrows or `h/j/k/l` | Pan |
| Left-button drag | Pan |
| `0` | Reset to fit the whole diagram |
| `y` | Copy Mermaid source |
| `esc` or `q` | Return to the document |
| `ctrl+c` | Quit mdview |

Right-click a diagram for **Copy Mermaid source**. Search (`/`) matches its
retained source and scrolls to the diagram. Selecting diagram rows copies the
source once, without image placeholders. Rendered diagrams are images: links
inside them are **not independently clickable**.

### Optional renderer installation

Install Node.js **22.12 or newer** (required by the Puppeteer version used in
verification), npm, the official
[Mermaid CLI](https://github.com/mermaid-js/mermaid-cli), and its Chromium
browser. The initially tested CLI version is pinned to **11.17.0**:

```sh
npm install --prefix "$HOME/.local/share/mdview/mermaid" \
  @mermaid-js/mermaid-cli@11.17.0 puppeteer@25.11.0
# If npm skipped Puppeteer's install script, explicitly install its browser:
node "$HOME/.local/share/mdview/mermaid/node_modules/puppeteer/install.mjs"
export PATH="$HOME/.local/share/mdview/mermaid/node_modules/.bin:$PATH"
mmdc --version
```

Puppeteer's normal install downloads a compatible Chrome for Testing; that browser also
needs the system libraries documented by
[Puppeteer](https://pptr.dev/troubleshooting). Alternatively, use an installed
Chromium browser:

```sh
export PUPPETEER_EXECUTABLE_PATH=/usr/bin/chromium-browser
```

Ensure `mmdc` and `node` are on the pager's `PATH`. Test that the browser works:

```sh
printf 'flowchart LR\n A --> B\n' > /tmp/diagram.mmd
mmdc -i /tmp/diagram.mmd -o /tmp/diagram.png -w 1600 -s 2
mdview testdata/mermaid.md
```

mdview never downloads dependencies while viewing a document. It uses a
1600-pixel browser viewport at scale 2, with Mocha/Latte colors and an opaque
matching background. At most two render jobs run concurrently, each with a
30-second timeout. Completed diagrams are cached for the session by source,
theme, renderer version, and settings, with a **128 MiB** budget accounting
for PNG bytes and decoded pixel size. Unused entries are evicted; diagrams
that cannot fit the budget or Kitty’s 10,000-pixel per-dimension limit remain as source. Resizing and zooming reuse the
PNG. Document changes cancel obsolete jobs; exit cleans up subprocesses,
temporary files, and owned terminal images.

The original code block stays visible during rendering, and silently remains
if Kitty, `mmdc`, or its browser is unavailable, capability detection fails,
or rendering errors or times out. Piped output, `-pager never`, other
terminals, and tmux/screen always retain code blocks. `-mermaid off` disables
graphics explicitly.

## Development

```sh
go build -o mdview .   # build
go test ./...          # run the test suite
go test -race ./...
MDVIEW_TEST_MMDC=1 go test ./mermaid -run TestRealCLI -v # optional real browser checks
go vet ./...
```

`testdata/sample.md` exercises every construct the renderer supports —
`mdview testdata/sample.md` is a quick visual smoke test.

The Mermaid verification record and real Kitty screenshots are in
[docs/mermaid-verification.md](docs/mermaid-verification.md).

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
poll for live reload. `mermaid/` owns cancellable renderer jobs and the bounded
session cache. `graphics/` verifies Kitty support and inserts streamed PNG
uploads and virtual placements into the same output writes as terminal frames.
Every placeholder cell carries its image, placement, row, and column identity,
so scrolling, panning, and overlays can clip images. Themes live in
`render/theme.go`.

## License

[MIT](LICENSE)
