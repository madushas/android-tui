package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

// cmdSpec is a device shell command, or an emulator-console command when emu != nil.
type cmdSpec struct {
	shell string
	emu   []string
}

type ctlItem struct {
	label, kind string // hdr toggle choice input action
	get         string
	val         func(out string) string
	set         func(arg string) []cmdSpec
	choices     [][2]string
	prompt      string
	emu         bool
}

func yn(o string) string {
	o = strings.ToLower(strings.TrimSpace(o))
	switch {
	case o == "1" || o == "true" || o == "yes" || o == "on" || strings.Contains(o, "night mode: yes"):
		return "on"
	case o == "0" || o == "false" || o == "no" || o == "off" || o == "null" || o == "" || strings.Contains(o, "night mode: no"):
		return "off"
	}
	return ""
}

func trim(o string) string { return strings.TrimSpace(o) }

func lastWord(o string) string {
	o = strings.TrimSpace(o)
	if o == "" {
		return ""
	}
	l := strings.Split(o, "\n")
	last := l[len(l)-1]
	if i := strings.LastIndex(last, ": "); i >= 0 {
		return last[i+2:]
	}
	return last
}

func sh1(f string, a ...any) []cmdSpec { return []cmdSpec{{shell: fmt.Sprintf(f, a...)}} }
func emuC(a ...string) []cmdSpec       { return []cmdSpec{{emu: a}} }
func onoff(a string) int               { return map[string]int{"on": 1}[a] }
func putSet(ns, key string) func(string) []cmdSpec {
	return func(a string) []cmdSpec { return sh1("settings put %s %s %d", ns, key, onoff(a)) }
}

var control = []ctlItem{
	{label: "Display", kind: "hdr"},
	{label: "Dark mode", kind: "toggle", get: "cmd uimode night", val: yn, set: func(a string) []cmdSpec {
		return sh1("cmd uimode night %s", map[string]string{"on": "yes", "off": "no"}[a])
	}},
	{label: "Auto-rotate", kind: "toggle", get: "settings get system accelerometer_rotation", val: yn, set: putSet("system", "accelerometer_rotation")},
	{label: "Font scale", kind: "choice", get: "settings get system font_scale", val: trim, set: func(a string) []cmdSpec { return sh1("settings put system font_scale %s", a) },
		choices: [][2]string{{"0.85", "0.85"}, {"1.0 (default)", "1.0"}, {"1.15", "1.15"}, {"1.3", "1.3"}, {"1.5", "1.5"}, {"2.0", "2.0"}}},
	{label: "Screen size", kind: "input", get: "wm size", val: lastWord, prompt: "WxH (e.g. 1080x1920) or 'reset'",
		set: func(a string) []cmdSpec {
			if a = strings.TrimSpace(a); a == "" || a == "reset" {
				return sh1("wm size reset")
			}
			return sh1("wm size %s", core.Quote(a))
		}},
	{label: "Density (dpi)", kind: "input", get: "wm density", val: lastWord, prompt: "dpi (e.g. 320) or 'reset'",
		set: func(a string) []cmdSpec {
			if a = strings.TrimSpace(a); a == "" || a == "reset" {
				return sh1("wm density reset")
			}
			return sh1("wm density %s", core.Quote(a))
		}},
	{label: "Animation scale", kind: "choice", get: "settings get global window_animation_scale", val: trim,
		set: func(a string) []cmdSpec {
			var c []cmdSpec
			for _, k := range []string{"window_animation_scale", "transition_animation_scale", "animator_duration_scale"} {
				c = append(c, cmdSpec{shell: fmt.Sprintf("settings put global %s %s", k, a)})
			}
			return c
		},
		choices: [][2]string{{"off (0)", "0"}, {"0.5x", "0.5"}, {"1x (default)", "1"}, {"2x", "2"}, {"5x", "5"}, {"10x", "10"}}},
	{label: "Network", kind: "hdr"},
	{label: "Wi-Fi", kind: "toggle", get: "settings get global wifi_on", val: yn, set: func(a string) []cmdSpec {
		return sh1("svc wifi %s", map[string]string{"on": "enable", "off": "disable"}[a])
	}},
	{label: "Mobile data", kind: "toggle", get: "settings get global mobile_data", val: yn, set: func(a string) []cmdSpec {
		return sh1("svc data %s", map[string]string{"on": "enable", "off": "disable"}[a])
	}},
	{label: "Airplane mode", kind: "toggle", get: "settings get global airplane_mode_on", val: yn, set: func(a string) []cmdSpec {
		return sh1("cmd connectivity airplane-mode %s 2>/dev/null || (settings put global airplane_mode_on %d; am broadcast -a android.intent.action.AIRPLANE_MODE --ez state %v)",
			map[string]string{"on": "enable", "off": "disable"}[a], onoff(a), a == "on")
	}},
	{label: "HTTP proxy", kind: "input", get: "settings get global http_proxy", prompt: "host:port (host machine is 10.0.2.2) or 'clear'",
		val: func(o string) string {
			if o = strings.TrimSpace(o); o == "null" || o == ":0" {
				return ""
			}
			return o
		},
		set: func(a string) []cmdSpec {
			if a = strings.TrimSpace(a); a == "" || a == "clear" {
				return sh1("settings put global http_proxy :0")
			}
			return sh1("settings put global http_proxy %s", core.Quote(a))
		}},
	{label: "Network speed", kind: "choice", emu: true, set: func(a string) []cmdSpec { return emuC("network", "speed", a) },
		choices: [][2]string{{"full", "full"}, {"lte", "lte"}, {"hsdpa", "hsdpa"}, {"umts", "umts"}, {"edge", "edge"}, {"gprs", "gprs"}, {"gsm", "gsm"}}},
	{label: "Network latency", kind: "choice", emu: true, set: func(a string) []cmdSpec { return emuC("network", "delay", a) },
		choices: [][2]string{{"none", "none"}, {"umts", "umts"}, {"edge", "edge"}, {"gprs", "gprs"}}},
	{label: "Power & sensors  (emulator console)", kind: "hdr"},
	{label: "Battery level", kind: "input", emu: true, prompt: "0-100", set: func(a string) []cmdSpec { return emuC("power", "capacity", strings.TrimSpace(a)) }},
	{label: "Charger (AC)", kind: "toggle", emu: true, set: func(a string) []cmdSpec { return emuC("power", "ac", a) }},
	{label: "Battery status", kind: "choice", emu: true, set: func(a string) []cmdSpec { return emuC("power", "status", a) },
		choices: [][2]string{{"charging", "charging"}, {"discharging", "discharging"}, {"not-charging", "not-charging"}, {"full", "full"}}},
	{label: "Rotate device", kind: "action", emu: true, set: func(string) []cmdSpec { return emuC("rotate") }},
	{label: "Fingerprint touch", kind: "action", emu: true, set: func(string) []cmdSpec { return emuC("finger", "touch", "1") }},
	{label: "Location & telephony  (emulator console)", kind: "hdr"},
	{label: "GPS location", kind: "action", emu: true},
	{label: "Send SMS", kind: "action", emu: true},
	{label: "Phone call", kind: "action", emu: true},
	{label: "Developer options", kind: "hdr"},
	{label: "Show touches", kind: "toggle", get: "settings get system show_touches", val: yn, set: putSet("system", "show_touches")},
	{label: "Pointer location", kind: "toggle", get: "settings get system pointer_location", val: yn, set: putSet("system", "pointer_location")},
	{label: "Layout bounds", kind: "toggle", get: "getprop debug.layout", val: yn, set: func(a string) []cmdSpec {
		return sh1("setprop debug.layout %v; service call activity 1599295570 >/dev/null", a == "on")
	}},
	{label: "Stay awake (charging)", kind: "toggle", get: "settings get global stay_on_while_plugged_in", set: func(a string) []cmdSpec { return sh1("svc power stayon %v", a == "on") },
		val: func(o string) string {
			if o = strings.TrimSpace(o); o == "0" || o == "null" || o == "" {
				return "off"
			}
			return "on"
		}},
	{label: "Don't keep activities", kind: "toggle", get: "settings get global always_finish_activities", val: yn, set: putSet("global", "always_finish_activities")},
}

var geo = [][3]string{{"San Francisco", "-122.4194", "37.7749"}, {"New York", "-74.0060", "40.7128"}, {"London", "-0.1276", "51.5072"},
	{"Berlin", "13.4050", "52.5200"}, {"Tokyo", "139.6917", "35.6895"}, {"Sydney", "151.2093", "-33.8688"}}

type controlTab struct {
	rowsTab
	vals   map[int]string
	shadow map[string]string
}

func newControl() Tab {
	c := &controlTab{rowsTab: rowsTab{baseTab: baseTab{ident: "control", name: "Control"}}, vals: map[int]string{}, shadow: map[string]string{}}
	c.build = c.buildRows
	return c
}

func (c *controlTab) Tick(m *Model) tea.Cmd {
	if c.stale(m) {
		c.vals = map[int]string{}
		if m.tgt().online() {
			return c.load(m)
		}
	}
	return nil
}

func (c *controlTab) load(m *Model) tea.Cmd {
	s, gen := m.tgt().serial(), c.gen
	var parts []string
	for i, it := range control {
		if it.get != "" {
			parts = append(parts, fmt.Sprintf(`echo "@@%d"; %s 2>/dev/null`, i, it.get))
		}
	}
	script := strings.Join(parts, "; ")
	return m.job("read settings", true, func() cont {
		r := core.Sh(s, script, core.Opt{Timeout: 20 * time.Second, Quiet: true})
		return func(m *Model) tea.Cmd {
			if gen != c.gen {
				return nil
			}
			cur, buf := -1, map[int][]string{}
			for _, l := range strings.Split(r.Out, "\n") {
				if strings.HasPrefix(l, "@@") {
					_, _ = fmt.Sscanf(l[2:], "%d", &cur) // best effort: a malformed marker keeps the previous index
					buf[cur] = nil
				} else if cur >= 0 {
					buf[cur] = append(buf[cur], l)
				}
			}
			for i, ls := range buf {
				if i >= 0 && i < len(control) && control[i].val != nil {
					c.vals[i] = control[i].val(strings.Join(ls, "\n"))
				}
			}
			return nil
		}
	})
}

func (c *controlTab) buildRows(m *Model) []row {
	t := m.tgt()
	var rows []row
	for i, it := range control {
		if it.kind == "hdr" {
			rows = append(rows, hdrRow(it.label))
			continue
		}
		v, ok := c.vals[i]
		if !ok {
			v = c.shadow[it.label]
		}
		var plain, styled string
		switch {
		case it.emu && !t.console():
			plain, styled = "emulator only", sDim.Render("emulator only")
		case it.kind == "toggle":
			plain = map[string]string{"on": "[x] on", "off": "[ ] off"}[v]
			if plain == "" {
				plain = "[?]"
			}
			styled = map[string]string{"on": sOK.Render(plain)}[v]
			if styled == "" {
				styled = plain
			}
		case it.kind == "action":
			plain, styled = "⏎ run", sDim.Render("⏎ run")
		default:
			if v == "null" || v == "" {
				v = "default"
			}
			plain = "‹" + v + "›"
			styled = sAcc.Render(plain)
		}
		lab := "  " + padR(it.label, 24)
		rows = append(rows, row{text: lab + plain, sty: lab + styled, d: i})
	}
	return rows
}

func (c *controlTab) Hints() []hint { return []hint{{"⏎/Space", "toggle / edit"}, {"r", "reload"}} }

func (c *controlTab) View(m *Model, w, h int, focused bool) []string {
	if o, ok := offline(m, w, h); ok {
		return o
	}
	return c.viewRows(m, w, h, focused)
}

// settingPut recognises a spec that is exactly `settings put <global|system|secure> <key> <value>`;
// those go through the journal (revertible) instead of a raw shell call.
func settingPut(specs []cmdSpec) (ns, key, val string, ok bool) {
	if len(specs) != 1 || specs[0].emu != nil {
		return
	}
	a := core.ShellSplit(specs[0].shell)
	if len(a) == 5 && a[0] == "settings" && a[1] == "put" && (a[2] == "global" || a[2] == "system" || a[2] == "secure") {
		return a[2], a[3], a[4], true
	}
	return
}

func (c *controlTab) run(m *Model, it ctlItem, arg, label string) tea.Cmd {
	s := m.tgt().serial()
	specs := it.set(arg)
	if ns, key, val, ok := settingPut(specs); ok && !it.emu {
		return m.apply(it.label+": "+label, core.Op{Kind: "setting", NS: ns, Key: key}, val, false, func(m *Model, e core.JEntry) tea.Cmd {
			if e.Err != "" {
				return nil // apply() already reported the error
			}
			c.shadow[it.label] = label
			return c.load(m)
		})
	}
	return m.job(it.label, false, func() cont {
		var r core.Res
		for _, sp := range specs {
			if sp.emu != nil {
				r = core.Adb(s, core.Opt{Timeout: 15 * time.Second}, append([]string{"emu"}, sp.emu...)...)
			} else {
				r = core.Sh(s, sp.shell, core.Opt{Timeout: 15 * time.Second})
			}
			if !r.OK() || strings.Contains(r.Out, "KO") {
				r = core.Res{RC: max(1, r.RC), Out: r.Out, Err: r.Err}
				break
			}
		}
		return func(m *Model) tea.Cmd {
			if r.OK() {
				m.report(true, it.label+": "+label)
				c.shadow[it.label] = label
				return c.load(m)
			}
			m.report(false, it.label+": "+orStr(strings.TrimSpace(r.Out), r.Why()))
			return nil
		}
	})
}

func (c *controlTab) Key(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	s := k.String()
	if c.nav(s) {
		return true, nil
	}
	switch s {
	case "r":
		c.Reload()
	case "enter", " ":
		if r := c.cur(); r != nil {
			return true, c.activate(m, r.d.(int))
		}
	default:
		return false, nil
	}
	return true, nil
}

func (c *controlTab) activate(m *Model, i int) tea.Cmd {
	it, t := control[i], m.tgt()
	if it.emu && !t.console() {
		m.say("this item needs the emulator console — running emulators only")
		return nil
	}
	switch {
	case it.kind == "toggle":
		cur := c.vals[i]
		if _, ok := c.vals[i]; !ok {
			cur = c.shadow[it.label]
		}
		next := "on"
		if cur == "on" || cur == "true" {
			next = "off"
		}
		if it.get == "" {
			c.shadow[it.label] = next
		}
		return c.run(m, it, next, next)
	case it.kind == "choice":
		var names []string
		for _, ch := range it.choices {
			names = append(names, ch[0])
		}
		m.choose(it.label, names, func(m *Model, j int) tea.Cmd { return c.run(m, it, it.choices[j][1], it.choices[j][0]) })
	case it.kind == "input":
		m.ask(it.label+"  ("+it.prompt+")", c.vals[i], func(m *Model, v string) tea.Cmd { return c.run(m, it, v, orStr(v, "reset")) })
	case it.label == "GPS location":
		names := []string{}
		for _, g := range geo {
			names = append(names, g[0])
		}
		m.choose("Set GPS location", append(names, "Custom (lat,lon)…"), func(m *Model, j int) tea.Cmd {
			set := func(name, lon, lat string) tea.Cmd {
				return c.run(m, ctlItem{label: "GPS location", set: func(string) []cmdSpec { return emuC("geo", "fix", lon, lat) }}, "", name)
			}
			if j < len(geo) {
				return set(geo[j][0], geo[j][1], geo[j][2])
			}
			m.ask("lat,lon (e.g. 37.7749,-122.4194)", "", func(m *Model, v string) tea.Cmd {
				p := strings.Split(v, ",")
				if len(p) != 2 {
					m.say("expected lat,lon")
					return nil
				}
				return set(strings.TrimSpace(v), strings.TrimSpace(p[1]), strings.TrimSpace(p[0]))
			})
			return nil
		})
	case it.label == "Send SMS":
		m.ask("From number", "5551234", func(m *Model, num string) tea.Cmd {
			m.ask("Message", "hello from emu", func(m *Model, txt string) tea.Cmd {
				return c.run(m, ctlItem{label: "Send SMS", set: func(string) []cmdSpec { return emuC("sms", "send", num, txt) }}, "", "sent")
			})
			return nil
		})
	case it.label == "Phone call":
		m.choose("Phone call", []string{"Incoming call…", "Accept", "Hang up"}, func(m *Model, j int) tea.Cmd {
			verb, done := []string{"call", "accept", "cancel"}[j], []string{"calling", "accepted", "ended"}[j]
			do := func(num string) tea.Cmd {
				return c.run(m, ctlItem{label: "Phone call", set: func(string) []cmdSpec { return emuC("gsm", verb, num) }}, "", done)
			}
			if j == 0 {
				m.ask("Number", "5551234", func(m *Model, v string) tea.Cmd { return do(v) })
				return nil
			}
			return do("5551234")
		})
	default:
		return c.run(m, it, "", "done")
	}
	return nil
}
