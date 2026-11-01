package core

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ───────────────────────────── host ─────────────────────────────

type Host struct {
	CPU               float64
	MemTotal, MemUsed uint64
	DiskFree          uint64
	KVM               string
	prevTot, prevIdle uint64
}

func (h *Host) Sample() {
	if b, err := os.ReadFile("/proc/stat"); err == nil {
		if f := strings.Fields(strings.SplitN(string(b), "\n", 2)[0]); len(f) > 8 {
			var v [8]uint64
			for i := 0; i < 8; i++ {
				v[i], _ = strconv.ParseUint(f[i+1], 10, 64)
			}
			var tot uint64
			for _, x := range v {
				tot += x
			}
			idle := v[3] + v[4]
			if h.prevTot != 0 && tot > h.prevTot {
				h.CPU = 100 * float64(tot-h.prevTot-(idle-h.prevIdle)) / float64(tot-h.prevTot)
			}
			h.prevTot, h.prevIdle = tot, idle
		}
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		var total, avail uint64
		for _, l := range strings.Split(string(b), "\n") {
			f := strings.Fields(l)
			if len(f) < 2 {
				continue
			}
			n, _ := strconv.ParseUint(f[1], 10, 64)
			switch f[0] {
			case "MemTotal:":
				total = n * 1024
			case "MemAvailable:":
				avail = n * 1024
			}
		}
		h.MemTotal, h.MemUsed = total, total-avail
	}
	p := AVDHome
	if !exists(p) {
		p = Home
	}
	var st syscall.Statfs_t
	if syscall.Statfs(p, &st) == nil {
		h.DiskFree = st.Bavail * uint64(st.Bsize)
	}
	switch {
	case !exists("/dev/kvm"):
		h.KVM = "missing"
	default:
		if f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0); err == nil {
			_ = f.Close()
			h.KVM = "ok"
		} else {
			h.KVM = "no access"
		}
	}
}

// ───────────────────────────── device telemetry ─────────────────────────────

func StatScript(full bool) string {
	s := "echo @@stat; head -n1 /proc/stat; echo @@mem; head -n3 /proc/meminfo; echo @@net; cat /proc/net/dev; echo @@up; cat /proc/uptime; "
	if full {
		s += "echo @@bat; dumpsys battery 2>/dev/null | grep -E 'level:|status:|powered|temperature'; echo @@df; df -k /data 2>/dev/null | tail -n1; "
	}
	return s
}

type Battery struct {
	Level, Status int
	AC, USB       bool
	Temp          float64
}

// Sample is one parsed telemetry reading (zero values = not present).
type Sample struct {
	CPUTot, CPUIdle        uint64
	MemTotal, MemUsed      uint64
	RX, TX                 uint64
	HasCPU, HasMem, HasNet bool
	Up                     float64
	Bat                    *Battery
	DFTotal, DFUsed        uint64
}

func ParseStats(out string) Sample {
	sec := map[string][]string{}
	cur := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "@@") {
			cur = strings.TrimSpace(l[2:])
			sec[cur] = nil
		} else if cur != "" {
			sec[cur] = append(sec[cur], l)
		}
	}
	var s Sample
	if l := sec["stat"]; len(l) > 0 {
		f := strings.Fields(l[0])
		if len(f) > 8 {
			var v [8]uint64
			for i := range v {
				v[i], _ = strconv.ParseUint(f[i+1], 10, 64)
			}
			for _, x := range v {
				s.CPUTot += x
			}
			s.CPUIdle, s.HasCPU = v[3]+v[4], true
		}
	}
	mi := map[string]uint64{}
	for _, l := range sec["mem"] {
		if k, v, ok := strings.Cut(l, ":"); ok {
			if f := strings.Fields(v); len(f) > 0 {
				n, _ := strconv.ParseUint(f[0], 10, 64)
				mi[k] = n * 1024
			}
		}
	}
	if t := mi["MemTotal"]; t > 0 {
		av, ok := mi["MemAvailable"]
		if !ok {
			av = mi["MemFree"]
		}
		s.MemTotal, s.MemUsed, s.HasMem = t, t-av, true
	}
	for _, l := range sec["net"] {
		iface, rest, ok := strings.Cut(l, ":")
		if !ok || strings.TrimSpace(iface) == "lo" {
			continue
		}
		if f := strings.Fields(rest); len(f) > 8 {
			a, _ := strconv.ParseUint(f[0], 10, 64)
			b, _ := strconv.ParseUint(f[8], 10, 64)
			s.RX, s.TX, s.HasNet = s.RX+a, s.TX+b, true
		}
	}
	if l := sec["up"]; len(l) > 0 {
		s.Up, _ = strconv.ParseFloat(strings.Fields(l[0] + " 0")[0], 64)
	}
	if l, ok := sec["bat"]; ok {
		b := map[string]string{}
		for _, x := range l {
			if k, v, ok := strings.Cut(x, ":"); ok {
				b[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
		if lv, err := strconv.Atoi(b["level"]); err == nil {
			st, _ := strconv.Atoi(b["status"])
			t, _ := strconv.Atoi(b["temperature"])
			s.Bat = &Battery{lv, st, b["AC powered"] == "true", b["USB powered"] == "true", float64(t) / 10}
		}
	}
	if l := sec["df"]; len(l) > 0 {
		if f := strings.Fields(l[0]); len(f) > 2 {
			a, _ := strconv.ParseUint(f[1], 10, 64)
			b, _ := strconv.ParseUint(f[2], 10, 64)
			s.DFTotal, s.DFUsed = a*1024, b*1024
		}
	}
	return s
}

// DevStats accumulates history and derived rates for one device.
type DevStats struct {
	CPU, Mem, RX, TX  []float64
	MemTotal, MemUsed uint64
	Bat               *Battery
	DFTotal, DFUsed   uint64
	Up                float64
	prev              Sample
	prevT             time.Time
	Seen              bool
}

const histMax = 240

func push(h []float64, v float64) []float64 {
	h = append(h, v)
	if len(h) > histMax {
		h = h[len(h)-histMax:]
	}
	return h
}

func (d *DevStats) Apply(s Sample) {
	now := time.Now()
	d.Seen = true
	if s.HasCPU {
		if d.prev.HasCPU && s.CPUTot > d.prev.CPUTot {
			dt, di := float64(s.CPUTot-d.prev.CPUTot), float64(s.CPUIdle-d.prev.CPUIdle)
			d.CPU = push(d.CPU, clamp(100*(dt-di)/dt, 0, 100))
		}
		d.prev.CPUTot, d.prev.CPUIdle, d.prev.HasCPU = s.CPUTot, s.CPUIdle, true
	}
	if s.HasMem {
		d.MemTotal, d.MemUsed = s.MemTotal, s.MemUsed
		d.Mem = push(d.Mem, 100*float64(s.MemUsed)/float64(s.MemTotal))
	}
	if s.HasNet {
		if d.prev.HasNet && !d.prevT.IsZero() {
			el := now.Sub(d.prevT).Seconds()
			if el > 0 {
				d.RX = push(d.RX, clamp(float64(int64(s.RX)-int64(d.prev.RX))/el, 0, 1e12))
				d.TX = push(d.TX, clamp(float64(int64(s.TX)-int64(d.prev.TX))/el, 0, 1e12))
			}
		}
		d.prev.RX, d.prev.TX, d.prev.HasNet = s.RX, s.TX, true
		d.prevT = now
	}
	if s.Bat != nil {
		d.Bat = s.Bat
	}
	if s.DFTotal > 0 {
		d.DFTotal, d.DFUsed = s.DFTotal, s.DFUsed
	}
	if s.Up > 0 {
		d.Up = s.Up
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ReadDeviceInfo fetches static facts about a booted device in one adb call.
func ReadDeviceInfo(serial string) map[string]string {
	keys := []string{"ro.product.model", "ro.product.manufacturer", "ro.build.version.release", "ro.build.version.sdk",
		"ro.product.cpu.abi", "ro.build.display.id", "persist.sys.locale", "ro.product.locale", "ro.build.type", "ro.kernel.qemu"}
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf(`echo "%s=$(getprop %s)"`, k, k))
	}
	script := strings.Join(parts, "; ") + `; echo "wm=$(wm size | tail -n1)"; echo "dpi=$(wm density | tail -n1)"`
	r := Sh(serial, script, Opt{Timeout: 8 * time.Second, Quiet: true})
	m := map[string]string{"_": ""}
	for _, l := range strings.Split(r.Out, "\n") {
		if k, v, ok := strings.Cut(l, "="); ok {
			m[k] = strings.TrimSpace(v)
		}
	}
	return m
}
