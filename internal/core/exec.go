package core

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Entry is one line of the command log.
type Entry struct {
	T    time.Time
	Kind string // cmd | ok | err | info
	Text string
}

// LogBuf is a small thread-safe ring buffer.
type LogBuf struct {
	mu   sync.Mutex
	ents []Entry
}

var CmdLog = &LogBuf{}

func (l *LogBuf) Add(kind, text string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ents = append(l.ents, Entry{time.Now(), kind, text})
	if len(l.ents) > 600 {
		l.ents = l.ents[len(l.ents)-500:]
	}
}

// Last returns up to n newest entries, oldest first.
func (l *LogBuf) Last(n int) []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.ents) < n {
		n = len(l.ents)
	}
	return append([]Entry(nil), l.ents[len(l.ents)-n:]...)
}

func Logf(kind, f string, a ...any) { CmdLog.Add(kind, fmt.Sprintf(f, a...)) }

// Res is the outcome of a command.
type Res struct {
	RC  int
	Out string
	Err string
}

func (r Res) OK() bool     { return r.RC == 0 }
func (r Res) Text() string { return strings.TrimSpace(r.Out) }
func (r Res) Both() string { return strings.TrimSpace(r.Out + r.Err) }
func (r Res) Why() string {
	for _, s := range []string{r.Err, r.Out} {
		if t := strings.TrimSpace(s); t != "" {
			lines := strings.Split(t, "\n")
			return lines[len(lines)-1]
		}
	}
	return fmt.Sprintf("exit %d", r.RC)
}

// Opt tunes Run.
type Opt struct {
	Timeout time.Duration
	Quiet   bool
	Stdin   string
	Env     []string
}

// Quiet is the common option for background polling.
var Quiet = Opt{Quiet: true, Timeout: 8 * time.Second}

// Quote single-quotes s for a POSIX shell (host or Android's sh).
func Quote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789@%+=:,./-_") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func ShellJoin(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = Quote(a)
	}
	return strings.Join(q, " ")
}

// Run executes argv, capturing output; never panics, never hangs past the timeout.
func Run(argv []string, o Opt) Res {
	if len(argv) == 0 {
		return Res{RC: 127, Err: "empty command"}
	}
	if !o.Quiet {
		CmdLog.Add("cmd", "$ "+ShellJoin(argv))
	}
	if o.Timeout == 0 {
		o.Timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), o.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	if o.Stdin != "" {
		cmd.Stdin = strings.NewReader(o.Stdin)
	}
	if o.Env != nil {
		cmd.Env = append(os.Environ(), o.Env...)
	}
	err := cmd.Run()
	r := Res{Out: strings.ReplaceAll(so.String(), "\r", ""), Err: strings.ReplaceAll(se.String(), "\r", "")}
	switch e := err.(type) {
	case nil:
	case *exec.ExitError:
		r.RC = e.ExitCode()
		if ctx.Err() != nil {
			r.RC, r.Err = 124, fmt.Sprintf("timeout after %s", o.Timeout)
		}
	default:
		r.RC = 127
		if ctx.Err() != nil {
			r.RC, r.Err = 124, fmt.Sprintf("timeout after %s", o.Timeout)
		} else {
			r.Err = fmt.Sprintf("%s: %v", argv[0], e)
		}
	}
	if !o.Quiet && r.RC != 0 {
		CmdLog.Add("err", r.Why())
	}
	return r
}

// Adb runs `adb -s serial args...`.
func Adb(serial string, o Opt, args ...string) Res {
	return Run(append([]string{"adb", "-s", serial}, args...), o)
}

// Sh runs one shell string on the device (quote user input with Quote!).
func Sh(serial, script string, o Opt) Res { return Adb(serial, o, "shell", script) }

// Which reports whether a binary is on PATH.
func Which(name string) bool { _, err := exec.LookPath(name); return err == nil }

// Binary runs a command and returns raw stdout (for screencap etc.; Run() strips \r).
func Binary(argv []string, timeout time.Duration) ([]byte, error) {
	CmdLog.Add("cmd", "$ "+ShellJoin(argv))
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var se bytes.Buffer
	cmd.Stderr = &se
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s", strings.TrimSpace(se.String()+" "+err.Error()))
	}
	return out, nil
}
