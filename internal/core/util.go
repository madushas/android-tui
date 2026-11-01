package core

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// Human formats a byte count (1.5G).
func Human(n float64) string {
	u := []string{"B", "K", "M", "G", "T"}
	i := 0
	for n >= 1024 && i < len(u)-1 {
		n /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%dB", int(n))
	}
	return fmt.Sprintf("%.1f%s", n, u[i])
}

// Clip shortens s to n runes with an ellipsis.
func Clip(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

var nameRE = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func ValidName(n string) bool { return nameRE.MatchString(n) }

// ---- ini helpers (config.ini, <name>.ini) ----

func IniGet(path, key string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(strings.TrimRight(l, "\r"), "="); ok && k == key {
			return v
		}
	}
	return ""
}

func IniHas(path, key string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, l := range strings.Split(string(b), "\n") {
		if k, _, ok := strings.Cut(l, "="); ok && k == key {
			return true
		}
	}
	return false
}

func IniSet(path, key, val string) error {
	var lines []string
	mode := os.FileMode(0o644)
	if b, err := os.ReadFile(path); err == nil {
		lines = strings.Split(strings.TrimRight(string(b), "\n"), "\n")
		if st, err := os.Stat(path); err == nil {
			mode = st.Mode()
		}
	}
	done := false
	for i, l := range lines {
		if k, _, ok := strings.Cut(l, "="); ok && k == key && !done {
			lines[i], done = key+"="+val, true
		}
	}
	if !done {
		lines = append(lines, key+"="+val)
	}
	tmp := path + ".emu-tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func IniSetIf(path, key, val string) {
	if IniHas(path, key) {
		_ = IniSet(path, key, val)
	}
}

// DirSize sums file sizes under path.
func DirSize(path string) int64 {
	var n int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, e := d.Info(); e == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

func CopyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}

func SortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// CopyText puts text on the clipboard using wl-copy / xclip / xsel.
func CopyText(text string) bool {
	for _, argv := range [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "-b", "-i"}} {
		if Which(argv[0]) {
			c := exec.Command(argv[0], argv[1:]...)
			c.Stdin = strings.NewReader(text)
			if c.Run() == nil {
				return true
			}
		}
	}
	return false
}

// CopyImage puts PNG bytes on the clipboard.
func CopyImage(png []byte) bool {
	for _, argv := range [][]string{{"wl-copy", "--type", "image/png"}, {"xclip", "-selection", "clipboard", "-t", "image/png"}} {
		if Which(argv[0]) {
			c := exec.Command(argv[0], argv[1:]...)
			c.Stdin = strings.NewReader(string(png))
			if c.Run() == nil {
				return true
			}
		}
	}
	return false
}

// OpenPath opens a file/folder with the configured opener (detached).
func OpenPath(p string) bool {
	if !Which(Cfg.OpenCmd) {
		return false
	}
	c := exec.Command(Cfg.OpenCmd, p)
	return c.Start() == nil
}

func PickEditor() string {
	for _, e := range []string{os.Getenv("VISUAL"), os.Getenv("EDITOR"), "nano", "vim", "vi"} {
		if e != "" {
			if f := ShellSplit(e); len(f) > 0 && Which(f[0]) {
				return e
			}
		}
	}
	return ""
}

// NormPath cleans a pasted/dropped path ('quotes', file:// URIs, ~, escaped spaces).
func NormPath(s string) string {
	s = strings.Trim(strings.TrimSpace(s), `'"`)
	s = strings.TrimPrefix(s, "file://")
	s = strings.ReplaceAll(s, `\ `, " ")
	if strings.HasPrefix(s, "~/") {
		s = filepath.Join(Home, s[2:])
	}
	return s
}

func MkdirP(p string) { _ = os.MkdirAll(p, 0o755) }

func diskFree(path string) uint64 {
	var st syscallStatfs
	if err := statfs(path, &st); err != nil {
		return 0
	}
	return st.free()
}
