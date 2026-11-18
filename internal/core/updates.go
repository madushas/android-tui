package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type SDKUpdate struct{ Pkg, Old, New string }

func updFile() string { return filepath.Join(CacheDir, "sdk-updates") }

// SDKUpdates asks the android CLI which packages have updates (nil, false = could not check).
func SDKUpdates() ([]SDKUpdate, bool) {
	r := Run([]string{"android", "sdk", "list"}, Opt{Timeout: 90 * time.Second, Quiet: true})
	if !r.OK() {
		return nil, false
	}
	var out []SDKUpdate
	for _, l := range strings.Split(r.Out, "\n") {
		f := strings.Fields(l)
		for i, t := range f {
			if t == "->" && i > 0 && i < len(f)-1 {
				out = append(out, SDKUpdate{f[0], f[i-1], f[i+1]})
				break
			}
		}
	}
	return out, true
}

func SaveUpdates(u []SDKUpdate) {
	MkdirPrivate(CacheDir)
	var sb strings.Builder
	for _, x := range u {
		fmt.Fprintf(&sb, "%s %s %s\n", x.Pkg, x.Old, x.New)
	}
	_ = WritePrivate(updFile(), []byte(sb.String()))
}

func CachedUpdateCount() int {
	b, err := os.ReadFile(updFile())
	if err != nil {
		return 0
	}
	n := 0
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n
}

// CheckUpdatesIfStale refreshes the cache when older than UpdateHours; returns the count or -1.
func CheckUpdatesIfStale() int {
	if Cfg.UpdateHours <= 0 || !HaveAndroid {
		return -1
	}
	if st, err := os.Stat(updFile()); err == nil && time.Since(st.ModTime()) < time.Duration(Cfg.UpdateHours)*time.Hour {
		return -1
	}
	u, ok := SDKUpdates()
	if !ok {
		return -1
	}
	SaveUpdates(u)
	return len(u)
}

func NeedAndroid() string {
	if HaveAndroid {
		return ""
	}
	return "needs the 'android' CLI:  curl -fsSL https://dl.google.com/android/cli/latest/linux_x86_64/install.sh | bash"
}
