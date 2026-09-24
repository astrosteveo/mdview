//go:build unix

package graphics

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

type Capability struct{ CellWidth, CellHeight float64 }

func Eligible() bool {
	return strings.HasPrefix(os.Getenv("TERM"), "xterm-kitty") && os.Getenv("TMUX") == "" && os.Getenv("STY") == "" && term.IsTerminal(int(os.Stdout.Fd()))
}

var versionRE = regexp.MustCompile(`kitty\(?([0-9]+)\.([0-9]+)`)
var cellRE = regexp.MustCompile("\x1b\\[6;([0-9]+);([0-9]+)t")

func parseCapability(reply string) (Capability, bool) {
	v := versionRE.FindStringSubmatch(reply)
	c := cellRE.FindStringSubmatch(reply)
	if len(v) != 3 || len(c) != 3 || !strings.Contains(reply, ";OK\x1b\\") {
		return Capability{}, false
	}
	major, _ := strconv.Atoi(v[1])
	minor, _ := strconv.Atoi(v[2])
	h, _ := strconv.Atoi(c[1])
	w, _ := strconv.Atoi(c[2])
	if (major == 0 && minor < 28) || h <= 0 || w <= 0 {
		return Capability{}, false
	}
	return Capability{float64(w), float64(h)}, true
}

// Probe runs before Bubble Tea owns input. A bounded, positive graphics/version/
// cell-size handshake is required; environment variables alone are insufficient.
func Probe() (Capability, bool) {
	if !Eligible() {
		return Capability{}, false
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return Capability{}, false
	}
	defer tty.Close()
	fd := int(tty.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return Capability{}, false
	}
	defer term.Restore(fd, old)
	id := int(time.Now().UnixNano()&0xffffff) + 1
	fmt.Fprintf(tty, "\x1b_Ga=q,i=%d,s=1,v=1,f=24;AAAA\x1b\\\x1b[>q\x1b[16t", id)
	reply := ""
	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) {
		p := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, e := unix.Poll(p, max(1, int(time.Until(deadline).Milliseconds())))
		if e != nil || n == 0 {
			break
		}
		var buf [1024]byte
		n, e = unix.Read(fd, buf[:])
		if e != nil {
			break
		}
		reply += string(buf[:n])
		if strings.Contains(reply, fmt.Sprintf("i=%d;OK", id)) {
			if cap, ok := parseCapability(reply); ok {
				return cap, true
			}
		}
	}
	return Capability{}, false
}

// CellSize uses the current terminal pixels on resize, including font changes.
func CellSize(fallback Capability) Capability {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err == nil && ws.Col > 0 && ws.Row > 0 && ws.Xpixel > 0 && ws.Ypixel > 0 {
		return Capability{float64(ws.Xpixel) / float64(ws.Col), float64(ws.Ypixel) / float64(ws.Row)}
	}
	return fallback
}
