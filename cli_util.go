package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/madushas/android-tui/internal/core"
)

func ask(prompt string) string {
	fmt.Print(prompt)
	s, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(s)
}

func confirm(msg string) bool {
	if assumeYes {
		return true
	}
	return strings.HasPrefix(strings.ToLower(ask(msg+" [y/N] ")), "y")
}

func fail(f string, a ...any) int { fmt.Fprintf(os.Stderr, "android-tui: "+f+"\n", a...); return 1 }

func done(ok bool, msg string) int {
	if msg != "" {
		fmt.Println(msg)
	}
	if ok {
		return 0
	}
	return 1
}

type dev struct {
	emu  *core.Emu
	conn *core.Conn
}

func (d dev) serial() string {
	if d.emu != nil {
		return d.emu.Serial
	}
	return d.conn.Serial
}

func (d dev) online() bool {
	if d.emu != nil {
		return d.emu.Online()
	}
	return d.conn.Online()
}

func resolve(sc core.Scan, q string) *dev {
	ql := strings.ToLower(q)
	var hits []dev
	for i := range sc.Emus {
		if sc.Emus[i].Name == q {
			return &dev{emu: &sc.Emus[i]}
		}
		if strings.Contains(strings.ToLower(sc.Emus[i].Name), ql) {
			hits = append(hits, dev{emu: &sc.Emus[i]})
		}
	}
	for i := range sc.Conns {
		if sc.Conns[i].Serial == q {
			return &dev{conn: &sc.Conns[i]}
		}
	}
	if len(hits) == 0 {
		for i := range sc.Conns {
			if strings.Contains(strings.ToLower(sc.Conns[i].Serial+" "+sc.Conns[i].Model), ql) {
				hits = append(hits, dev{conn: &sc.Conns[i]})
			}
		}
	}
	if len(hits) == 1 {
		return &hits[0]
	}
	if len(hits) == 0 {
		fail("no AVD or device matching '%s'", q)
	} else {
		fail("'%s' is ambiguous (%d matches)", q, len(hits))
	}
	return nil
}

func printList(sc core.Scan) {
	col := func(state string) string {
		if !isTTY() {
			return ""
		}
		return map[string]string{"running": "\x1b[32m", "device": "\x1b[32m", "booting": "\x1b[33m"}[state] + map[bool]string{true: "", false: ""}[false]
	}
	rs := func() string {
		if isTTY() {
			return "\x1b[0m"
		}
		return ""
	}
	fmt.Println("EMULATORS")
	if len(sc.Emus) == 0 {
		fmt.Println("  no AVDs yet — run: android-tui new")
	}
	for _, e := range sc.Emus {
		dot := map[string]string{"running": "●", "booting": "◐"}[e.State]
		if dot == "" {
			dot = "○"
		}
		c := col(e.State)
		if c == "" && isTTY() {
			c = "\x1b[2m"
		}
		star := ""
		if e.Custom {
			star = " ★"
		}
		fmt.Printf("  %s%s %-28s %-9s %-16s %s%s%s\n", c, dot, e.Name, e.State, orDash(e.Serial), e.Info, star, rs())
	}
	fmt.Println("ADB CONNECTIONS")
	if len(sc.Conns) == 0 {
		fmt.Println("  none — android-tui pair / android-tui connect host:port")
	}
	for _, c := range sc.Conns {
		dot := "◆"
		if c.State == "saved" {
			dot = "○"
		}
		fmt.Printf("  %s %-28s %-12s %-11s %s\n", dot, c.Name(), c.State, c.Transport, c.Serial)
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func isTTY() bool {
	st, err := os.Stdout.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func runExt(argv ...string) int {
	c := exec.Command(argv[0], argv[1:]...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		return 127
	}
	return 0
}

func fi(f *os.File) bool {
	st, err := f.Stat()
	return err != nil || st.Mode()&os.ModeCharDevice == 0
}

func termSize() (int, int) {
	c := exec.Command("stty", "size")
	c.Stdin = os.Stdin
	out, err := c.Output()
	var r, cl int
	if err == nil {
		_, _ = fmt.Sscanf(string(out), "%d %d", &r, &cl) // zero values fall through to the default size below
	}
	if r < 5 || cl < 10 {
		return 100, 30
	}
	return cl, r
}

// showImage prints a PNG with the best protocol the terminal supports, then waits for Enter.
func showImage(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return fail("%v", err)
	}
	cols, rows := termSize()
	rows -= 3
	switch core.ImageProtocol() {
	case "kitty":
		fmt.Print("\x1b[2J\x1b[H" + core.KittySeq(b, cols, rows))
	case "chafa":
		runExt("chafa", "-s", fmt.Sprintf("%dx%d", cols, rows), path)
	case "sixel":
		runExt("img2sixel", "-w", fmt.Sprint(cols*8), path)
	default:
		lines, err := core.RenderHalfBlocks(b, cols, rows)
		if err != nil {
			return fail("%v", err)
		}
		fmt.Println(strings.Join(lines, "\n"))
	}
	fmt.Print("\n\n  " + filepath.Base(path) + "  —  press Enter to return… ")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n') // any key (or EOF) returns
	return 0
}

func stateMark(s string) string {
	return map[string]string{"on": "●", "off": "○", "partial": "◐", "action": "▶", "preset": "◆", "unknown": "?"}[s]
}
