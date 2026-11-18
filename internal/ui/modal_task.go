package ui

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

// newWork runs fn in a goroutine and streams its output into a viewer.
func newWork(title string, autoclose bool, fn func(emit core.Emit, cancelled func() bool) any, then func(m *Model, v any) tea.Cmd) *viewerModal {
	s := &stream{}
	v := &viewerModal{title: title, src: s, follow: true, autoclose: autoclose, then: then}
	go func() {
		val := fn(s.emit, s.cancelled)
		s.mu.Lock()
		s.val, s.done = val, true
		s.mu.Unlock()
	}()
	return v
}

var crRE = regexp.MustCompile(`[\r\n]`)

// newTask streams one or more commands (stdout+stderr) into a viewer; value is the final exit code.
func newTask(title string, argvs [][]string, then func(m *Model, rc int, lines []string) tea.Cmd) *viewerModal {
	var s *stream
	v := newWork(title, false, func(emit core.Emit, cancelled func() bool) any {
		rc := 0
		for _, argv := range argvs {
			emitStyled := func(t, st string) { s.addStyled(t, st) }
			emitStyled("$ "+core.ShellJoin(argv), "dim")
			core.Logf("cmd", "$ %s", core.ShellJoin(argv))
			cmd := exec.Command(argv[0], argv[1:]...)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			pr, pw, err := os.Pipe()
			if err != nil {
				return 1
			}
			cmd.Stdout, cmd.Stderr = pw, pw
			if err := cmd.Start(); err != nil {
				_ = pw.Close()
				_ = pr.Close()
				emitStyled(err.Error(), "err")
				return 127
			}
			_ = pw.Close() // the child holds its own copy of the write end
			s.mu.Lock()
			s.proc = cmd.Process
			s.mu.Unlock()
			buf := make([]byte, 4096)
			var acc []byte
			flush := func(seg []byte, sep byte) {
				if len(strings.TrimSpace(string(seg))) > 0 {
					emit(string(seg), sep == '\r') // a trailing \r means "progress line: overwrite it"
				}
			}
			for {
				n, err := pr.Read(buf)
				acc = append(acc, buf[:n]...)
				for {
					loc := crRE.FindIndex(acc)
					if loc == nil {
						break
					}
					flush(acc[:loc[0]], acc[loc[0]])
					acc = acc[loc[1]:]
				}
				if err != nil {
					break
				}
			}
			_ = pr.Close()
			if len(acc) > 0 {
				flush(acc, '\n')
			}
			if err := cmd.Wait(); err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					rc = ee.ExitCode()
				} else {
					rc = 1
				}
			}
			if cancelled() {
				break
			}
		}
		st := "ok"
		if rc != 0 {
			st = "err"
		}
		s.addStyled(fmt.Sprintf("── finished (exit %d) ──", rc), st)
		return rc
	}, nil)
	s = v.src
	v.then = func(m *Model, val any) tea.Cmd {
		if then == nil {
			return nil
		}
		ls, _ := s.snapshot()
		var txt []string
		for _, l := range ls {
			txt = append(txt, l.t)
		}
		rc, _ := val.(int)
		return then(m, rc, txt)
	}
	return v
}

// ───────────────────────────── QR pairing ─────────────────────────────
