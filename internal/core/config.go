// Package core holds everything that is not UI: config, adb/emulator plumbing, parsers.
package core

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Version is the android-tui release. Release builds override it: -ldflags "-X github.com/madushas/android-tui/internal/core.Version=1.2.3".
var Version = "0.1.0-dev"

// Config mirrors the (bash-syntax) ~/.config/emu/config of earlier versions.
type Config struct {
	EmuFlags    []string
	DefaultGPU  string
	SDKRoot     string
	UpdateHours int
	StopTimeout int
	AVDFlags    map[string]string
	ShotDir     string
	Theme       string // default | contrast | mono
	PullDir     string
	APKDir      string
	LogLines    int
	RefreshSecs float64
	OpenCmd     string
	Mouse       bool
}

var (
	Cfg         Config
	Home        string
	AVDHome     string
	CacheDir    string
	ConfDir     string
	ConfFile    string
	DataDir     string // journal + snapshots
	SDK         string // resolved SDK root
	HaveAndroid bool   // the new `android` CLI is installed
)

func init() {
	Home, _ = os.UserHomeDir()
	AVDHome = envOr("ANDROID_AVD_HOME", filepath.Join(Home, ".android", "avd"))
	CacheDir = filepath.Join(envOr("XDG_CACHE_HOME", filepath.Join(Home, ".cache")), "android-tui")
	ConfDir = filepath.Join(envOr("XDG_CONFIG_HOME", filepath.Join(Home, ".config")), "android-tui")
	DataDir = filepath.Join(envOr("XDG_DATA_HOME", filepath.Join(Home, ".local", "share")), "android-tui")
	ConfFile = filepath.Join(ConfDir, "config")
	Cfg = Config{
		EmuFlags: []string{"-no-metrics"}, DefaultGPU: "swiftshader_indirect", UpdateHours: 12, StopTimeout: 20,
		AVDFlags: map[string]string{}, ShotDir: filepath.Join(Home, "Pictures", "android-tui"), PullDir: filepath.Join(Home, "Downloads"),
		APKDir: filepath.Join(Home, "Downloads"), LogLines: 5000, RefreshSecs: 2, OpenCmd: "xdg-open", Mouse: true,
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

var assignRE = regexp.MustCompile(`^\s*(?:export\s+)?([A-Z_][A-Z0-9_]*)(\[[^\]]+\])?=(.*)$`)

// ShellSplit splits like a POSIX shell: quotes, backslashes and # comments.
func ShellSplit(s string) []string {
	var out []string
	var cur strings.Builder
	in, q := false, rune(0)
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch {
		case q == '\'':
			if c == '\'' {
				q = 0
			} else {
				cur.WriteRune(c)
			}
		case q == '"':
			if c == '"' {
				q = 0
			} else if c == '\\' && i+1 < len(rs) && strings.ContainsRune(`"\$`+"`", rs[i+1]) {
				i++
				cur.WriteRune(rs[i])
			} else {
				cur.WriteRune(c)
			}
		case c == '\'' || c == '"':
			q, in = c, true
		case c == '\\' && i+1 < len(rs):
			i++
			cur.WriteRune(rs[i])
			in = true
		case c == '#' && !in:
			return out
		case c == ' ' || c == '\t' || c == '\n':
			if in {
				out = append(out, cur.String())
				cur.Reset()
				in = false
			}
		default:
			cur.WriteRune(c)
			in = true
		}
	}
	if in {
		out = append(out, cur.String())
	}
	return out
}

// MigrateLegacy copies settings from the old "emu" config/cache directories the first time android-tui runs.
func MigrateLegacy() {
	if _, err := os.Stat(ConfDir); err == nil {
		return
	}
	old := filepath.Join(filepath.Dir(ConfDir), "emu")
	ents, err := os.ReadDir(old)
	if err != nil {
		return
	}
	MkdirP(ConfDir)
	for _, e := range ents {
		if !e.IsDir() {
			_ = CopyFile(filepath.Join(old, e.Name()), filepath.Join(ConfDir, e.Name()))
		}
	}
	if b, err := os.ReadFile(filepath.Join(filepath.Dir(CacheDir), "emu", "shell_history")); err == nil {
		MkdirP(CacheDir)
		_ = os.WriteFile(filepath.Join(CacheDir, "shell_history"), b, 0o644)
	}
	Logf("info", "migrated settings from %s", old)
}

// LoadConfig parses ConfFile without executing it.
func LoadConfig() {
	b, err := os.ReadFile(ConfFile)
	if err != nil {
		return
	}
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := assignRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key, idx, val := m[1], m[2], strings.TrimSpace(m[3])
		var list []string
		scalar := ""
		if strings.HasPrefix(val, "(") {
			end := strings.LastIndex(val, ")")
			if end < 0 {
				continue
			}
			for _, t := range ShellSplit(val[1:end]) {
				list = append(list, os.ExpandEnv(t))
			}
			scalar = strings.Join(list, " ")
		} else if p := ShellSplit(val); len(p) > 0 {
			scalar = os.ExpandEnv(p[0])
			list = strings.Fields(scalar)
		}
		atoi := func() int { n, _ := strconv.Atoi(scalar); return n }
		switch {
		case key == "AVD_FLAGS" && idx != "":
			Cfg.AVDFlags[idx[1:len(idx)-1]] = scalar
		case key == "EMU_FLAGS":
			Cfg.EmuFlags = list
		case key == "DEFAULT_GPU":
			Cfg.DefaultGPU = scalar
		case key == "SDK_ROOT":
			Cfg.SDKRoot = scalar
		case key == "UPDATE_CHECK_HOURS":
			Cfg.UpdateHours = atoi()
		case key == "STOP_TIMEOUT":
			Cfg.StopTimeout = atoi()
		case key == "THEME":
			Cfg.Theme = scalar
		case key == "SHOT_DIR":
			Cfg.ShotDir = scalar
		case key == "PULL_DIR":
			Cfg.PullDir = scalar
		case key == "APK_DIR":
			Cfg.APKDir = scalar
		case key == "LOG_LINES":
			Cfg.LogLines = atoi()
		case key == "REFRESH":
			f, _ := strconv.ParseFloat(scalar, 64)
			if f > 0 {
				Cfg.RefreshSecs = f
			}
		case key == "OPEN_CMD":
			Cfg.OpenCmd = scalar
		case key == "MOUSE":
			Cfg.Mouse = atoi() != 0
		}
	}
}

// ConfigTemplate is written by `android-tui config` the first time.
const ConfigTemplate = `# android-tui configuration — bash syntax kept so older emu versions can still read it.

# Flags passed to every emulator launch.
#EMU_FLAGS=(-no-metrics)

# GPU used when an AVD's hw.gpu.mode is "auto" (ignored if the AVD has its own -gpu flag).
# swiftshader_indirect/guest are deprecated on recent emulators; "host" is the usual choice.
#DEFAULT_GPU="swiftshader_indirect"

# Extra flags for individual AVDs (the TUI's AVD tab → Launch flags edits these lines).
#AVD_FLAGS[Pixel_9_Pro]="-gpu host -feature -Vulkan"

# Force the SDK location (default: $ANDROID_HOME, location of ` + "`emulator`" + `, ~/Android/Sdk).
#SDK_ROOT="$HOME/Android/Sdk"

# Background SDK update check interval in hours (0 disables it).
#UPDATE_CHECK_HOURS=12

# Seconds to wait for an emulator to shut down.
#STOP_TIMEOUT=20

# Colour theme: default (your terminal palette) · contrast (brighter colours, readable greys) · mono (no colour; bold/underline/reverse only).
# NO_COLOR=1 in the environment also selects mono; ANDROID_TUI_THEME overrides this setting.
#THEME=default

# Where screenshots / pulled files / the APK picker look.
#SHOT_DIR="$HOME/Pictures/android-tui"
#PULL_DIR="$HOME/Downloads"
#APK_DIR="$HOME/Downloads"

# Logcat lines kept in memory, TUI refresh interval (seconds), mouse support (1/0).
#LOG_LINES=5000
#REFRESH=2
#MOUSE=1

# Command used to open files/folders.
#OPEN_CMD="xdg-open"
`
