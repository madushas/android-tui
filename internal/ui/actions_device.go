package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

func (m *Model) screenSize() (int, int) {
	var w, h int
	if n, _ := fmt.Sscanf(regexp.MustCompile(`\d+x\d+`).FindString(m.devInfo()["wm"]), "%dx%d", &w, &h); n == 2 {
		return w, h
	}
	return 1080, 1920
}

var keycodes = [][2]string{{"Home", "KEYCODE_HOME"}, {"Back", "KEYCODE_BACK"}, {"Recents", "KEYCODE_APP_SWITCH"}, {"Power", "KEYCODE_POWER"},
	{"Volume up", "KEYCODE_VOLUME_UP"}, {"Volume down", "KEYCODE_VOLUME_DOWN"}, {"Mute", "KEYCODE_VOLUME_MUTE"}, {"Enter", "KEYCODE_ENTER"},
	{"Tab", "KEYCODE_TAB"}, {"Escape", "KEYCODE_ESCAPE"}, {"Delete", "KEYCODE_DEL"}, {"Menu", "KEYCODE_MENU"}, {"Search", "KEYCODE_SEARCH"},
	{"D-pad up", "KEYCODE_DPAD_UP"}, {"D-pad down", "KEYCODE_DPAD_DOWN"}, {"D-pad left", "KEYCODE_DPAD_LEFT"}, {"D-pad right", "KEYCODE_DPAD_RIGHT"},
	{"D-pad center", "KEYCODE_DPAD_CENTER"}, {"Wake up", "KEYCODE_WAKEUP"}, {"Sleep", "KEYCODE_SLEEP"}, {"Notifications", "KEYCODE_NOTIFICATION"},
	{"Camera", "KEYCODE_CAMERA"}, {"Play/pause", "KEYCODE_MEDIA_PLAY_PAUSE"}}

func (m *Model) logcatFor(pkg string) tea.Cmd {
	m.gotoTab("logcat")
	return m.tabs["logcat"].(*logcatTab).setApp(m, pkg)
}

func (m *Model) installAPK(path string) tea.Cmd {
	t := m.tgt()
	if !t.online() {
		m.say("select a running device first")
		return nil
	}
	s := t.serial()
	if path == "" {
		seen := map[string]bool{}
		var cands []string
		wd, _ := os.Getwd()
		for _, d := range []string{core.Cfg.APKDir, wd, filepath.Join(core.Home, "Downloads")} {
			g, _ := filepath.Glob(filepath.Join(d, "*.apk"))
			for _, f := range g {
				if !seen[f] {
					seen[f] = true
					cands = append(cands, f)
				}
			}
		}
		sort.Slice(cands, func(i, j int) bool {
			a, _ := os.Stat(cands[i])
			b, _ := os.Stat(cands[j])
			return a != nil && b != nil && a.ModTime().After(b.ModTime())
		})
		if len(cands) > 20 {
			cands = cands[:20]
		}
		var names []string
		for _, f := range cands {
			st, _ := os.Stat(f)
			names = append(names, fmt.Sprintf("%s   %s  ·  %s", filepath.Base(f), human(float64(st.Size())), filepath.Dir(f)))
		}
		m.choose("Install APK", append(names, "Type or paste a path (file, or folder of split APKs)…"), func(m *Model, i int) tea.Cmd {
			if i < len(cands) {
				return m.installAPK(cands[i])
			}
			m.askPath("APK path (file, or folder of split APKs)", "", "drag & drop a file onto the terminal to paste its path", func(m *Model, v string) tea.Cmd {
				if v != "" {
					return m.installAPK(core.NormPath(v))
				}
				return nil
			})
			return nil
		})
		return nil
	}
	st, err := os.Stat(path)
	if err != nil {
		m.report(false, "not found: "+path)
		return nil
	}
	files := []string{path}
	if st.IsDir() {
		files, _ = filepath.Glob(filepath.Join(path, "*.apk"))
		sort.Strings(files)
	}
	if len(files) == 0 {
		m.report(false, "no .apk files found there")
		return nil
	}
	opts := [][2]string{{"-r", "Replace existing app (-r)"}, {"-g", "Grant all permissions (-g)"}, {"-d", "Allow version downgrade (-d)"}, {"-t", "Allow test packages (-t)"}}
	var labels []string
	for _, o := range opts {
		labels = append(labels, o[1])
	}
	m.multi("Install options (Space toggles, Enter installs)", labels, []int{0}, true, func(m *Model, idx []int) tea.Cmd {
		verb := "install"
		if len(files) > 1 {
			verb = "install-multiple"
		}
		argv := []string{"adb", "-s", s, verb}
		for _, i := range idx {
			argv = append(argv, opts[i][0])
		}
		argv = append(argv, files...)
		m.push(newTask("adb "+verb+" — "+filepath.Base(files[0]), [][]string{argv}, func(m *Model, rc int, lines []string) tea.Cmd {
			ok := rc == 0 && strings.Contains(strings.Join(lines, "\n"), "Success")
			m.report(ok, map[bool]string{true: "installed " + filepath.Base(files[0]), false: "install failed — see the log (try -r or -d)"}[ok])
			m.tabs["apps"].Reload()
			return nil
		}))
		return nil
	})
	return nil
}

func (m *Model) installURL() tea.Cmd {
	if !m.tgt().online() {
		m.say("select a running device first")
		return nil
	}
	m.ask("URL of an .apk", "https://", func(m *Model, u string) tea.Cmd {
		if u == "" || u == "https://" {
			return nil
		}
		name := core.SafeName(filepath.Base(strings.SplitN(u, "?", 2)[0])) // the URL is typed or remote: never trust its basename
		if !strings.HasSuffix(name, ".apk") {
			name += ".apk"
		}
		dest := filepath.Join(core.CacheDir, "dl", name)
		core.MkdirP(filepath.Dir(dest))
		m.push(newWork("Download — "+name, true, func(emit core.Emit, c func() bool) any {
			if _, err := core.Download(u, dest, emit, c); err != nil {
				emit("✗ "+err.Error(), false)
				return nil
			}
			return dest
		}, func(m *Model, v any) tea.Cmd {
			if p, ok := v.(string); ok {
				return m.installAPK(p)
			}
			return nil
		}))
		return nil
	})
	return nil
}

func (m *Model) extractAPKs() tea.Cmd {
	t := m.tgt()
	s, name := t.serial(), t.name()
	m.push(newWork("Extract all user APKs", false, func(emit core.Emit, c func() bool) any {
		r := core.Sh(s, "pm list packages -f -3", core.Opt{Timeout: 30 * time.Second})
		base := filepath.Join(core.Cfg.PullDir, core.SafeName(name)+"-apks")
		n, total := 0, 0
		for _, l := range strings.Split(r.Out, "\n") {
			if !strings.HasPrefix(l, "package:") {
				continue
			}
			total++
			i := strings.LastIndex(l, "=")
			path, pkg := l[8:i], l[i+1:]
			if c() {
				break
			}
			if !core.IsPackageName(pkg) {
				emit(fmt.Sprintf("skipped unsafe package name %q from the device", pkg), false)
				continue
			}
			dest, err := core.JoinWithin(base, pkg)
			if err != nil {
				continue
			}
			core.MkdirP(dest)
			pr := core.Adb(s, core.Opt{Timeout: 300 * time.Second}, "pull", path, dest+"/")
			emit(pkg+": "+core.AdbMsg(pr), false)
			if pr.OK() {
				n++
			}
		}
		emit(fmt.Sprintf("%d/%d base APKs saved under %s", n, total, base), false)
		return n
	}, nil))
	return nil
}

func (m *Model) screenshot(mode string) tea.Cmd {
	t := m.tgt()
	if !t.online() {
		m.say("select a running device first")
		return nil
	}
	s, name := t.serial(), t.name()
	return m.act("screenshot", func() (bool, string) {
		core.MkdirP(core.Cfg.ShotDir)
		f := filepath.Join(core.Cfg.ShotDir, fmt.Sprintf("%s-%s.png", core.SafeName(name), time.Now().Format("20060102-150405")))
		b, err := core.Binary([]string{"adb", "-s", s, "exec-out", "screencap", "-p"}, 30*time.Second)
		if err != nil || len(b) < 100 {
			return false, "screenshot failed: " + fmt.Sprint(err)
		}
		if err := core.WritePrivate(f, b); err != nil {
			return false, err.Error()
		}
		extra := ""
		switch mode {
		case "clip":
			extra = map[bool]string{true: " · copied to clipboard", false: " · no clipboard tool"}[core.CopyImage(b)]
		case "open":
			extra = map[bool]string{true: " · opened", false: " · xdg-open missing"}[core.OpenPath(f)]
		}
		return true, "saved " + f + extra
	})
}

func (m *Model) record(extra ...string) tea.Cmd {
	t := m.tgt()
	if m.rec != nil {
		rec := m.rec
		m.rec = nil
		return m.act("stop recording", func() (bool, string) {
			core.Sh(rec.serial, "kill -2 $(pidof screenrecord)", core.Opt{Timeout: 10 * time.Second})
			done := make(chan struct{})
			go func() { _, _ = rec.proc.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(8 * time.Second):
				_ = rec.proc.Kill()
			}
			time.Sleep(time.Second)
			core.MkdirP(core.Cfg.ShotDir)
			dest := filepath.Join(core.Cfg.ShotDir, fmt.Sprintf("%s-%s.mp4", core.SafeName(rec.name), time.Now().Format("20060102-150405")))
			r := core.Adb(rec.serial, core.Opt{Timeout: 300 * time.Second}, "pull", rec.remote, dest)
			core.Sh(rec.serial, "rm -f "+core.Quote(rec.remote), core.Opt{Quiet: true})
			if r.OK() {
				return true, "saved " + dest
			}
			return false, "pull failed: " + r.Why()
		})
	}
	if !t.online() {
		m.say("select a running device first")
		return nil
	}
	remote := fmt.Sprintf("/sdcard/android-tui-rec-%d.mp4", time.Now().Unix())
	argv := []string{"adb", "-s", t.serial(), "shell", "screenrecord"}
	if len(extra) == 0 {
		extra = []string{"--time-limit", "180"}
	}
	argv = append(append(argv, extra...), remote)
	core.Logf("cmd", "$ %s", core.ShellJoin(argv))
	c := exec.Command(argv[0], argv[1:]...)
	if err := c.Start(); err != nil {
		m.report(false, err.Error())
		return nil
	}
	m.rec = &recording{serial: t.serial(), remote: remote, name: t.name(), proc: c.Process, t0: time.Now()}
	m.say("recording (max 3 min) — run 'Screen record' again to stop")
	return nil
}

func (m *Model) scrcpy() tea.Cmd {
	t := m.tgt()
	if !core.Which("scrcpy") {
		m.say("scrcpy not installed (pacman -S scrcpy)")
		return nil
	}
	core.Logf("cmd", "$ scrcpy -s %s", t.serial())
	if err := exec.Command("scrcpy", "-s", t.serial()).Start(); err != nil {
		m.report(false, err.Error())
		return nil
	}
	m.say("scrcpy started for " + t.serial())
	return nil
}

func (m *Model) layoutDump() tea.Cmd {
	s := m.tgt().serial()
	return m.job("UI layout", false, func() cont {
		var out string
		if core.HaveAndroid {
			if r := core.Run([]string{"android", "layout", "--pretty", "--device", s}, core.Opt{Timeout: time.Minute}); r.OK() && strings.TrimSpace(r.Out) != "" {
				out = r.Out
			}
		}
		if out == "" {
			r := core.Sh(s, "uiautomator dump /sdcard/android-tui-ui.xml >/dev/null 2>&1; cat /sdcard/android-tui-ui.xml; rm -f /sdcard/android-tui-ui.xml", core.Opt{Timeout: 40 * time.Second})
			out = strings.ReplaceAll(r.Out, "><", ">\n<")
		}
		return func(m *Model) tea.Cmd { m.viewText("UI layout", strings.Split(out, "\n")); return nil }
	})
}

func (m *Model) sensors() tea.Cmd {
	opts := [][3]string{{"acceleration", "x:y:z  m/s²  (gravity pointing down is 0:9.8:0)", "0:9.8:0"}, {"magnetic-field", "x:y:z  µT", "0:30:0"},
		{"proximity", "distance cm (0 = near)", "0"}, {"light", "lux", "400"}, {"temperature", "°C", "22"}, {"humidity", "%", "50"}, {"pressure", "hPa", "1013"}}
	var names []string
	for _, o := range opts {
		names = append(names, fmt.Sprintf("%-16s %s", o[0], o[1]))
	}
	m.choose("Virtual sensor", names, func(m *Model, i int) tea.Cmd {
		m.ask(opts[i][0]+" value", opts[i][2], func(m *Model, v string) tea.Cmd { return m.emuCmd("sensor set", "sensor", "set", opts[i][0], v) })
		return nil
	})
	return nil
}

func (m *Model) dumpsysPicker() tea.Cmd {
	s := m.tgt().serial()
	return m.job("dumpsys -l", false, func() cont {
		r := core.Sh(s, "dumpsys -l", core.Opt{Timeout: 20 * time.Second})
		var names []string
		for _, l := range strings.Split(r.Out, "\n") {
			if strings.HasPrefix(l, "  ") && strings.TrimSpace(l) != "" {
				names = append(names, strings.TrimSpace(l))
			}
		}
		return func(m *Model) tea.Cmd {
			if len(names) == 0 {
				m.say("could not list services")
				return nil
			}
			var its []item
			for _, n := range names {
				its = append(its, item{text: n})
			}
			m.push(&chooseModal{title: "dumpsys …", items: its, flt: newEdit(""), typeahead: true, filtering: true, marks: map[int]bool{},
				pick: func(m *Model, i int) tea.Cmd { return m.showAdb("dumpsys "+names[i], "dumpsys "+core.Quote(names[i])) }})
			return nil
		}
	})
}

func (m *Model) ports() tea.Cmd {
	s := m.tgt().serial()
	opts := []string{"List forwards + reverses", "Forward  host → device", "Reverse  device → host", "Remove all forwards", "Remove all reverses"}
	m.choose("Port forwarding", opts, func(m *Model, i int) tea.Cmd {
		switch i {
		case 0:
			return m.job("ports", false, func() cont {
				f := core.Adb(s, core.Opt{}, "forward", "--list")
				r := core.Adb(s, core.Opt{}, "reverse", "--list")
				ls := append(append([]string{"forward:"}, splitLines(strings.TrimSpace(orStr(f.Out, "  (none)")))...), "", "reverse:")
				ls = append(ls, splitLines(strings.TrimSpace(orStr(r.Out, "  (none)")))...)
				return func(m *Model) tea.Cmd { m.viewText("forward / reverse", ls); return nil }
			})
		case 1, 2:
			verb := []string{"", "forward", "reverse"}[i]
			m.ask("local → remote  (e.g. tcp:8080 tcp:8080)", "tcp:8080 tcp:8080", func(m *Model, v string) tea.Cmd {
				if len(strings.Fields(v)) == 0 {
					return nil
				}
				return m.hostAdb(verb, append([]string{verb}, strings.Fields(v)...)...)
			})
		default:
			verb := map[int]string{3: "forward", 4: "reverse"}[i]
			return m.hostAdb("remove "+verb+"s", verb, "--remove-all")
		}
		return nil
	})
	return nil
}

// startShizuku runs the adb bootstrap for Shizuku and re-probes the profile afterwards.
func (m *Model) startShizuku() tea.Cmd {
	s := m.tgt().serial()
	return m.job("start Shizuku", false, func() cont {
		ok, msg := core.ShizukuBootstrap(s)
		return func(m *Model) tea.Cmd { delete(m.profiles, s); m.report(ok, msg); return nil }
	})
}
