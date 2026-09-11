// Package tui is the interactive pager: a viewport over rendered markdown
// with search, live reload, and a status bar.
package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"mdview/render"
)

const (
	marginX   = 1 // keeps borders and rules off the terminal edge
	pollEvery = 500 * time.Millisecond
)

type keymap struct {
	Quit, Top, Bottom, Search, Next, Prev, Reload, Cancel, Accept key.Binding
}

var keys = keymap{
	Quit:   key.NewBinding(key.WithKeys("q", "esc", "ctrl+c")),
	Top:    key.NewBinding(key.WithKeys("g", "home")),
	Bottom: key.NewBinding(key.WithKeys("G", "end")),
	Search: key.NewBinding(key.WithKeys("/")),
	Next:   key.NewBinding(key.WithKeys("n")),
	Prev:   key.NewBinding(key.WithKeys("N")),
	Reload: key.NewBinding(key.WithKeys("r")),
	Cancel: key.NewBinding(key.WithKeys("esc", "ctrl+c")),
	Accept: key.NewBinding(key.WithKeys("enter")),
}

type tickMsg time.Time

type Model struct {
	path     string // "" when reading from stdin
	src      []byte
	renderer *render.Renderer
	maxWidth int

	vp      viewport.Model
	ready   bool
	termW   int
	termH   int
	plain   []string // ANSI-stripped rendered lines, for search
	lastMod time.Time

	search    textinput.Model
	searching bool
	query     string
	matches   []int
	matchIdx  int
	notice    string
}

func New(path string, src []byte, r *render.Renderer, maxWidth int) Model {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.CharLimit = 200
	m := Model{path: path, src: src, renderer: r, maxWidth: maxWidth, search: ti}
	if path != "" {
		if fi, err := os.Stat(path); err == nil {
			m.lastMod = fi.ModTime()
		}
	}
	return m
}

func (m Model) Init() tea.Cmd {
	if m.path == "" {
		return nil
	}
	return tick()
}

func tick() tea.Cmd {
	return tea.Tick(pollEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) contentWidth() int {
	w := m.termW - marginX*2
	if m.maxWidth > 0 && w > m.maxWidth {
		w = m.maxWidth
	}
	return max(w, 20)
}

// rerender re-renders the source at the current width, preserving scroll.
func (m *Model) rerender() {
	rendered := m.renderer.Render(m.src, m.contentWidth())
	pad := strings.Repeat(" ", marginX)
	lines := strings.Split(rendered, "\n")
	m.plain = m.plain[:0]
	for i, l := range lines {
		m.plain = append(m.plain, ansi.Strip(l))
		lines[i] = pad + l
	}
	off := m.vp.YOffset
	m.vp.SetContent(strings.Join(lines, "\n"))
	m.vp.SetYOffset(off)
	if m.query != "" {
		m.findMatches()
	}
}

func (m *Model) reload() bool {
	if m.path == "" {
		return false
	}
	fi, err := os.Stat(m.path)
	if err != nil || !fi.ModTime().After(m.lastMod) {
		return false
	}
	data, err := os.ReadFile(m.path)
	if err != nil {
		return false
	}
	m.lastMod = fi.ModTime()
	m.src = data
	m.rerender()
	m.notice = "reloaded"
	return true
}

func (m *Model) findMatches() {
	m.matches = m.matches[:0]
	q := strings.ToLower(m.query)
	for i, l := range m.plain {
		if strings.Contains(strings.ToLower(l), q) {
			m.matches = append(m.matches, i)
		}
	}
}

func (m *Model) jumpToMatch(delta int) {
	if len(m.matches) == 0 {
		return
	}
	m.matchIdx = ((m.matchIdx+delta)%len(m.matches) + len(m.matches)) % len(m.matches)
	m.vp.SetYOffset(m.matches[m.matchIdx])
}

func (m *Model) startSearchFrom() {
	// Pick the first match at or below the current top line.
	m.matchIdx = 0
	for i, ln := range m.matches {
		if ln >= m.vp.YOffset {
			m.matchIdx = i
			break
		}
	}
	if len(m.matches) > 0 {
		m.vp.SetYOffset(m.matches[m.matchIdx])
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termW, m.termH = msg.Width, msg.Height
		h := max(msg.Height-1, 1)
		if !m.ready {
			m.vp = viewport.New(msg.Width, h)
			m.ready = true
		} else {
			m.vp.Width, m.vp.Height = msg.Width, h
		}
		m.rerender()
		return m, nil

	case tickMsg:
		m.reload()
		return m, tick()

	case tea.KeyMsg:
		m.notice = ""
		if m.searching {
			switch {
			case key.Matches(msg, keys.Cancel):
				m.searching = false
				m.search.Blur()
				return m, nil
			case key.Matches(msg, keys.Accept):
				m.searching = false
				m.search.Blur()
				m.query = strings.TrimSpace(m.search.Value())
				if m.query == "" {
					m.matches = nil
					return m, nil
				}
				m.findMatches()
				m.startSearchFrom()
				if len(m.matches) == 0 {
					m.notice = "no matches"
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.search, cmd = m.search.Update(msg)
			return m, cmd
		}

		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, keys.Top):
			m.vp.GotoTop()
			return m, nil
		case key.Matches(msg, keys.Bottom):
			m.vp.GotoBottom()
			return m, nil
		case key.Matches(msg, keys.Search):
			m.searching = true
			m.search.SetValue("")
			return m, m.search.Focus()
		case key.Matches(msg, keys.Next):
			m.jumpToMatch(1)
			return m, nil
		case key.Matches(msg, keys.Prev):
			m.jumpToMatch(-1)
			return m, nil
		case key.Matches(msg, keys.Reload):
			m.lastMod = time.Time{}
			if !m.reload() {
				m.notice = "nothing to reload"
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	if !m.ready {
		return ""
	}
	return m.vp.View() + "\n" + m.statusBar()
}

func (m Model) statusBar() string {
	th := m.renderer.Theme()
	bar := lipgloss.NewStyle().Background(th.Mantle).Foreground(th.Subtle)
	name := lipgloss.NewStyle().Background(th.Accent).Foreground(th.Mantle).Bold(true).Padding(0, 1)
	dim := bar.Foreground(th.Muted)

	if m.searching {
		left := name.Render("search")
		field := bar.Render(" " + m.search.View())
		return padBar(left+field, m.termW, bar)
	}

	title := m.path
	if title == "" {
		title = "stdin"
	}
	left := name.Render(title)
	if m.notice != "" {
		left += bar.Render(" " + m.notice)
	} else if m.query != "" {
		if len(m.matches) == 0 {
			left += bar.Render(fmt.Sprintf(" /%s: none", m.query))
		} else {
			left += bar.Render(fmt.Sprintf(" /%s: %d/%d", m.query, m.matchIdx+1, len(m.matches)))
		}
	}

	pct := fmt.Sprintf(" %3.0f%% ", m.vp.ScrollPercent()*100)
	right := dim.Render("j/k scroll · / search · n/N next · r reload · q quit") + bar.Bold(true).Render(pct)

	gap := m.termW - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		right = bar.Bold(true).Render(pct)
		gap = max(m.termW-lipgloss.Width(left)-lipgloss.Width(right), 0)
	}
	return left + bar.Render(strings.Repeat(" ", gap)) + right
}

func padBar(s string, width int, style lipgloss.Style) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + style.Render(strings.Repeat(" ", width-w))
}
