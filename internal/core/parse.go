package core

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Device is one line of `adb devices -l`.
type Device struct{ Serial, State, Model, Transport, TID string }

func ParseDevicesL(out string) []Device {
	var res []Device
	for _, line := range strings.Split(out, "\n") {
		t := strings.Fields(line)
		if len(t) < 2 || strings.HasPrefix(line, "*") || strings.HasPrefix(line, "List of") {
			continue
		}
		kv := map[string]string{}
		for _, x := range t[2:] {
			if k, v, ok := strings.Cut(x, ":"); ok {
				kv[k] = v
			}
		}
		s := t[0]
		tr := "usb"
		switch {
		case strings.HasPrefix(s, "emulator-"):
			tr = "emulator"
		case strings.Contains(s, "_adb-tls-connect"):
			tr = "wifi (tls)"
		case regexp.MustCompile(`:\d+$`).MatchString(s):
			tr = "wifi"
		}
		res = append(res, Device{s, t[1], kv["model"], tr, kv["transport_id"]})
	}
	return res
}

// MDNS is one discovered adb service.
type MDNS struct{ Name, Kind, Addr string } // Kind: pair | connect | plain

var mdnsRE = regexp.MustCompile(`(\S+)\s+(_adb[\w-]*\._tcp)\.?\s+(\S+:\d+)`)

func ParseMDNS(out string) []MDNS {
	var res []MDNS
	for _, l := range strings.Split(out, "\n") {
		if m := mdnsRE.FindStringSubmatch(l); m != nil {
			k := "plain"
			if strings.Contains(m[2], "pairing") {
				k = "pair"
			} else if strings.Contains(m[2], "tls-connect") {
				k = "connect"
			}
			res = append(res, MDNS{m[1], k, m[3]})
		}
	}
	return res
}

// ─────────────────────────── packages ───────────────────────────

type Pkg struct {
	Name, Path, Installer     string
	VC                        int
	UID                       string
	System, Disabled, Removed bool
}

// PMScript lists installed packages with installer/version/uid plus system/disabled/all sets.
const PMScript = "pm list packages -f -i --show-versioncode -U 2>/dev/null || pm list packages -f; echo @@sys; pm list packages -s; echo @@dis; pm list packages -d; echo @@all; pm list packages -u"

func lastAfterEq(s string) string {
	if i := strings.LastIndex(s, "="); i >= 0 {
		return s[i+1:]
	}
	return s
}

func ParsePM(out string) []Pkg {
	sec := "main"
	var main []Pkg
	sys, dis, all := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "@@") {
			sec = strings.TrimSpace(l[2:])
			continue
		}
		if !strings.HasPrefix(l, "package:") {
			continue
		}
		toks := strings.Fields(l[8:])
		if len(toks) == 0 {
			continue
		}
		switch sec {
		case "sys":
			sys[lastAfterEq(toks[0])] = true
		case "dis":
			dis[lastAfterEq(toks[0])] = true
		case "all":
			all[lastAfterEq(toks[0])] = true
		default:
			p := Pkg{}
			if i := strings.LastIndex(toks[0], "="); i >= 0 {
				p.Path, p.Name = toks[0][:i], toks[0][i+1:]
			} else {
				p.Name = toks[0]
			}
			for _, t := range toks[1:] {
				switch {
				case strings.HasPrefix(t, "installer="):
					p.Installer = t[10:]
				case strings.HasPrefix(t, "versionCode:"):
					p.VC, _ = strconv.Atoi(t[12:])
				case strings.HasPrefix(t, "uid:"):
					p.UID = t[4:]
				}
			}
			main = append(main, p)
		}
	}
	have := map[string]bool{}
	for i := range main {
		have[main[i].Name] = true
		main[i].System, main[i].Disabled = sys[main[i].Name], dis[main[i].Name]
	}
	var gone []string
	for p := range all {
		if !have[p] {
			gone = append(gone, p)
		}
	}
	sort.Strings(gone)
	for _, p := range gone {
		main = append(main, Pkg{Name: p, System: sys[p], Removed: true})
	}
	return main
}

type Perm struct {
	Granted *bool
	Kind    string // runtime | install | requested
}

var permRE = regexp.MustCompile(`^([\w.]+): granted=(true|false)`)

func ParsePerms(out string) map[string]Perm {
	perms := map[string]Perm{}
	sec := ""
	for _, l := range strings.Split(out, "\n") {
		st := strings.TrimSpace(l)
		if strings.HasSuffix(st, "permissions:") {
			sec = st
			continue
		}
		if m := permRE.FindStringSubmatch(st); m != nil && (strings.Contains(sec, "runtime") || strings.Contains(sec, "install")) {
			g := m[2] == "true"
			k := "install"
			if strings.Contains(sec, "runtime") {
				k = "runtime"
			}
			perms[m[1]] = Perm{&g, k}
		} else if strings.HasPrefix(sec, "requested") && regexp.MustCompile(`^[\w.]+$`).MatchString(st) {
			if _, ok := perms[st]; !ok {
				perms[st] = Perm{nil, "requested"}
			}
		}
	}
	return perms
}

// ─────────────────────────── props / files / procs / settings ───────────────────────────

type KV struct{ K, V string }

var propRE = regexp.MustCompile(`^\[(.*?)\]: \[(.*)\]$`)

func ParseProps(out string) []KV {
	var res []KV
	for _, l := range strings.Split(out, "\n") {
		if m := propRE.FindStringSubmatch(l); m != nil {
			res = append(res, KV{m[1], m[2]})
		}
	}
	sort.Slice(res, func(i, j int) bool { return res[i].K < res[j].K })
	return res
}

type FileEnt struct {
	Perm, Owner, Date, Name, Link string
	Size                          int64
	Dir                           bool
}

var lsRE = regexp.MustCompile(`^([\-dlcbpsD][\w\-+.]{8,10})\s+\d+\s+(\S+)\s+(\S+)\s+(\d+(?:,\s*\d+)?)\s+(\d{4}-\d\d-\d\d \d\d:\d\d)\s+(.*)$`)

func ParseLs(out string) []FileEnt {
	var res []FileEnt
	for _, l := range strings.Split(out, "\n") {
		m := lsRE.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		e := FileEnt{Perm: m[1], Owner: m[2], Date: m[5], Name: m[6], Dir: m[1][0] == 'd' || m[1][0] == 'l'}
		if e.Perm[0] == 'l' {
			if n, link, ok := strings.Cut(e.Name, " -> "); ok {
				e.Name, e.Link = n, link
			}
		}
		e.Size, _ = strconv.ParseInt(strings.TrimSpace(strings.SplitN(m[4], ",", 2)[0]), 10, 64)
		res = append(res, e)
	}
	sort.SliceStable(res, func(i, j int) bool {
		if res[i].Dir != res[j].Dir {
			return res[i].Dir
		}
		return strings.ToLower(res[i].Name) < strings.ToLower(res[j].Name)
	})
	return res
}

type Proc struct {
	PID  int
	User string
	RSS  int64 // KiB
	Name string
}

func ParseProcs(out string) []Proc {
	var res []Proc
	for i, l := range strings.Split(out, "\n") {
		if i == 0 {
			continue
		}
		f := strings.Fields(l)
		if len(f) < 4 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		rss, _ := strconv.ParseInt(f[2], 10, 64)
		res = append(res, Proc{pid, f[1], rss, strings.Join(f[3:], " ")})
	}
	sort.Slice(res, func(i, j int) bool { return res[i].RSS > res[j].RSS })
	return res
}

type Setting struct{ NS, K, V string }

func ParseSettings(out string) []Setting {
	var res []Setting
	ns := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "@@") {
			ns = strings.TrimSpace(l[2:])
		} else if k, v, ok := strings.Cut(l, "="); ok {
			res = append(res, Setting{ns, k, v})
		}
	}
	return res
}

// ParseLogLine splits `logcat -v threadtime`; lvl is -1 for unstructured lines.
type LogLine struct {
	Lvl          int
	PID, Tag, TS string
	Msg, Raw     string
}

var logRE = regexp.MustCompile(`^(\d\d-\d\d) (\d\d:\d\d:\d\d\.\d+)\s+(\d+)\s+(\d+)\s+([VDIWEFS])\s+(.*?)\s*:\s(.*)$`)

func ParseLogLine(l string) LogLine {
	m := logRE.FindStringSubmatch(l)
	if m == nil {
		return LogLine{Lvl: -1, Msg: l, Raw: l}
	}
	return LogLine{Lvl: strings.Index("VDIWEFS", m[5]), PID: m[3], Tag: m[6], TS: m[2], Msg: m[7], Raw: l}
}
