package core

import (
	"regexp"
	"strconv"
	"strings"
)

type TopProc struct {
	PID      int
	User     string
	CPU      float64
	RSS, PSS int64 // KiB
	Name     string
}

// ParseTop reads `top -b -n 1` output of toybox/procps styles by locating columns from the header.
func ParseTop(out string) []TopProc {
	lines := strings.Split(out, "\n")
	hi := -1
	for i, l := range lines {
		if strings.Contains(l, "PID") && (strings.Contains(l, "ARGS") || strings.Contains(l, "COMMAND") || strings.Contains(l, "Name") || strings.Contains(l, "NAME")) {
			hi = i
			break
		}
	}
	if hi < 0 {
		return nil
	}
	hdr := strings.Fields(strings.ReplaceAll(lines[hi], "S[%CPU]", "S %CPU")) // toybox prints state and %CPU as separate columns
	col := map[string]int{}
	for i, h := range hdr {
		h = strings.ToUpper(strings.Trim(h, "[]"))
		col[h] = i
	}
	cpuI, ok := col["%CPU"]
	if !ok {
		cpuI, ok = col["CPU%"]
	}
	if !ok {
		cpuI = -1
	}
	resI, ok := col["RES"]
	if !ok {
		resI, ok = col["RSS"]
	}
	if !ok {
		resI = -1
	}
	userI, ok := col["USER"]
	if !ok {
		userI = -1
	}
	last := len(hdr) - 1
	var res []TopProc
	for _, l := range lines[hi+1:] {
		f := strings.Fields(l)
		if len(f) <= last {
			continue
		}
		pid, err := strconv.Atoi(f[col["PID"]])
		if err != nil {
			continue
		}
		p := TopProc{PID: pid, Name: strings.Join(f[last:], " ")}
		if userI >= 0 {
			p.User = f[userI]
		}
		// toybox lists S before [%CPU] so the "[%CPU]" header index lines up with fields
		if cpuI >= 0 && cpuI < len(f) {
			p.CPU, _ = strconv.ParseFloat(strings.TrimSuffix(f[cpuI], "%"), 64)
		}
		if resI >= 0 && resI < len(f) {
			p.RSS = parseSize(f[resI])
		}
		res = append(res, p)
	}
	return res
}

// parseSize converts toybox-style sizes (123M, 4.5G, 1234K, 5678) to KiB.
func parseSize(s string) int64 {
	if s == "" {
		return 0
	}
	mult := 1.0
	switch s[len(s)-1] {
	case 'K', 'k':
		s = s[:len(s)-1]
	case 'M', 'm':
		s, mult = s[:len(s)-1], 1024
	case 'G', 'g':
		s, mult = s[:len(s)-1], 1024*1024
	}
	f, _ := strconv.ParseFloat(strings.ReplaceAll(s, ",", ""), 64)
	return int64(f * mult)
}

var pssRE = regexp.MustCompile(`^\s*([\d,]+)K: (\S+) \(pid (\d+)`)

// ParsePSS reads the "Total PSS by process" section of dumpsys meminfo: pid -> KiB.
func ParsePSS(out string) map[int]int64 {
	m := map[int]int64{}
	in := false
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "Total PSS by process") {
			in = true
			continue
		}
		if in {
			if strings.TrimSpace(l) == "" {
				break
			}
			if g := pssRE.FindStringSubmatch(l); g != nil {
				kb, _ := strconv.ParseInt(strings.ReplaceAll(g[1], ",", ""), 10, 64)
				pid, _ := strconv.Atoi(g[3])
				m[pid] = kb
			}
		}
	}
	return m
}

// ───────────────────────────── logcat presets ─────────────────────────────
