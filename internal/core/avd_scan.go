package core

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Emu is an AVD with its live state.
type Emu struct {
	Name   string
	Serial string
	State  string // stopped | booting | running | offline | unauthorized
	Info   ImgInfo
	RSS    int64 // KiB of host memory
	Custom bool
}

func (e Emu) Active() bool { return e.State != "stopped" }

func (e Emu) Online() bool { return e.State == "running" && e.Serial != "" }

// Conn is one adb transport that is not an AVD of ours (USB, Wi-Fi, TLS, unknown emulator, or saved).
type Conn struct {
	Serial    string
	State     string // device | offline | unauthorized | saved ...
	Model     string
	Transport string // usb | wifi | wifi (tls) | emulator | saved
	TID       string
	Addr      string // host:port for saved entries
}

func (c Conn) Online() bool { return c.State == "device" }

func (c Conn) Name() string {
	if c.Model != "" {
		return c.Model
	}
	return c.Serial
}

// Scan is one poll result.
type Scan struct {
	Emus  []Emu
	Conns []Conn
}

type Scanner struct {
	mu     sync.Mutex
	names  map[string]string // serial -> avd name
	booted map[string]bool
}

var Scn = &Scanner{names: map[string]string{}, booted: map[string]bool{}}

var psRE = regexp.MustCompile(`(?:^|[\s/])(?:emulator|qemu-system\S*)\s+(?:.*\s)?-avd\s+(\S+)(?:\s|$)`)

var psLineRE = regexp.MustCompile(`^\s*(\d+)\s+(\d+)\s+(.*)$`)

// PsScan -> {avd name: [nprocs, rss KiB]} for emulator processes on this host.
func PsScan() map[string][2]int64 {
	out := map[string][2]int64{}
	r := Run([]string{"ps", "-eo", "pid=,rss=,args="}, Quiet)
	for _, l := range strings.Split(r.Out, "\n") {
		lm := psLineRE.FindStringSubmatch(l)
		if lm == nil {
			continue
		}
		if m := psRE.FindStringSubmatch(lm[3]); m != nil {
			e := out[m[1]]
			e[0]++
			rss, _ := strconv.ParseInt(lm[2], 10, 64)
			e[1] += rss
			out[m[1]] = e
		}
	}
	return out
}

func (s *Scanner) Scan() Scan {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := ListNames()
	nameSet := map[string]bool{}
	for _, n := range names {
		nameSet[n] = true
	}
	devs := ParseDevicesL(Run([]string{"adb", "devices", "-l"}, Opt{Quiet: true, Timeout: 8 * time.Second}).Out)
	live := map[string]Device{}
	for _, d := range devs {
		live[d.Serial] = d
	}
	for k := range s.names {
		if _, ok := live[k]; !ok {
			delete(s.names, k)
		}
	}
	for k := range s.booted {
		if _, ok := live[k]; !ok {
			delete(s.booted, k)
		}
	}
	procs := PsScan()
	avdSerial := map[string]string{}
	var extra []Device
	for _, d := range devs {
		if strings.HasPrefix(d.Serial, "emulator-") {
			n := s.names[d.Serial]
			if n == "" && d.State == "device" {
				first := strings.TrimSpace(strings.SplitN(Adb(d.Serial, Opt{Quiet: true, Timeout: 2 * time.Second}, "emu", "avd", "name").Out, "\n", 2)[0])
				if nameSet[first] {
					n = first
					s.names[d.Serial] = n
				}
			}
			if nameSet[n] {
				avdSerial[n] = d.Serial
				continue
			}
		}
		extra = append(extra, d)
	}
	var sc Scan
	for _, n := range names {
		e := Emu{Name: n, Info: AvdInfo(n)}
		e.Custom = e.Info.TagID != "" && !IsOfficialTag(e.Info.TagID)
		if ser := avdSerial[n]; ser != "" {
			e.Serial = ser
			if st := live[ser].State; st != "device" {
				e.State = st
			} else {
				if !s.booted[ser] && Sh(ser, "getprop sys.boot_completed", Opt{Quiet: true, Timeout: 2 * time.Second}).Text() == "1" {
					s.booted[ser] = true
				}
				e.State = map[bool]string{true: "running", false: "booting"}[s.booted[ser]]
			}
		} else if _, ok := procs[n]; ok {
			e.State = "booting"
		} else {
			e.State = "stopped"
		}
		e.RSS = procs[n][1]
		sc.Emus = append(sc.Emus, e)
	}
	have := map[string]bool{}
	for _, d := range extra {
		sc.Conns = append(sc.Conns, Conn{Serial: d.Serial, State: d.State, Model: d.Model, Transport: d.Transport, TID: d.TID})
		have[d.Serial] = true
	}
	for _, c := range LoadConns() {
		if !have[c.Addr] {
			sc.Conns = append(sc.Conns, Conn{Serial: c.Addr, State: "saved", Model: c.Name, Transport: "saved", Addr: c.Addr})
		}
	}
	return sc
}

// Lookup returns the current state of one AVD (fresh scan).
func Lookup(name string) (Emu, bool) {
	for _, e := range Scn.Scan().Emus {
		if e.Name == name {
			return e, true
		}
	}
	return Emu{}, false
}

func ActiveNow(name string) bool { e, ok := Lookup(name); return ok && e.Active() }

// ───────────────────────────── start / stop ─────────────────────────────
