package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type SettingsSnap struct {
	TS     time.Time         `json:"ts"`
	Serial string            `json:"serial"`
	Model  string            `json:"model"`
	Label  string            `json:"label"`
	Data   map[string]string `json:"data"`
	File   string            `json:"-"`
}

const snapScript = `for ns in system secure global; do echo "@@settings/$ns"; settings list $ns; done; echo "@@device_config"; device_config list 2>/dev/null`

func parseSnap(out string, props bool, propOut string) map[string]string {
	m := map[string]string{}
	pre := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "@@") {
			pre = l[2:]
			continue
		}
		k, v, ok := strings.Cut(l, "=")
		if !ok || pre == "" {
			continue
		}
		if pre == "device_config" {
			m["device_config/"+k] = v
		} else {
			m[pre+"/"+k] = v
		}
	}
	if props {
		for _, kv := range ParseProps(propOut) {
			m["prop/"+kv.K] = kv.V
		}
	}
	return m
}

func snapDir() string { return filepath.Join(DataDir, "snapshots") }

var (
	baseMu    sync.Mutex
	baselined = map[string]bool{}
)

// EnsureBaseline takes one "auto" settings snapshot per device per process, before the first change.
// Skipped when a fresh (<1h) "auto" snapshot already exists, so repeated launches don't pile up files.
func EnsureBaseline(p *Profile) {
	if p == nil {
		return
	}
	baseMu.Lock()
	done := baselined[p.Serial]
	baselined[p.Serial] = true
	baseMu.Unlock()
	if done {
		return
	}
	for _, s := range ListSnapshots(p.Serial) {
		if s.Label == "auto" && time.Since(s.TS) < time.Hour {
			return
		}
	}
	if _, err := TakeSnapshot(p, "auto", false); err != nil {
		Logf("err", "auto baseline: %v", err)
	}
}

// TakeSnapshot captures settings (+device_config, optionally props) and stores it on disk.
func TakeSnapshot(p *Profile, label string, props bool) (*SettingsSnap, error) {
	r := Exec(p.Serial, snapScript, false)
	if !r.OK() && r.Out == "" {
		return nil, fmt.Errorf("%s", r.Why())
	}
	propOut := ""
	if props {
		propOut = Exec(p.Serial, "getprop", false).Out
	}
	s := &SettingsSnap{TS: time.Now(), Serial: p.Serial, Model: p.Model, Label: label, Data: parseSnap(r.Out, props, propOut)}
	MkdirP(snapDir())
	s.File = filepath.Join(snapDir(), fmt.Sprintf("%s-%s.json", safeName(p.Serial), s.TS.Format("20060102-150405")))
	b, _ := json.MarshalIndent(s, "", " ")
	return s, WritePrivate(s.File, b)
}

func safeName(s string) string { return regexp.MustCompile(`[^A-Za-z0-9._-]`).ReplaceAllString(s, "_") }

func ListSnapshots(serial string) []*SettingsSnap {
	m, _ := filepath.Glob(filepath.Join(snapDir(), safeName(serial)+"-*.json"))
	sort.Sort(sort.Reverse(sort.StringSlice(m)))
	var out []*SettingsSnap
	for _, f := range m {
		var s SettingsSnap
		if b, err := os.ReadFile(f); err == nil && json.Unmarshal(b, &s) == nil {
			s.File = f
			out = append(out, &s)
		}
	}
	return out
}

// CurrentSnapshot reads the device now without saving.
func CurrentSnapshot(p *Profile, props bool) *SettingsSnap {
	r := Exec(p.Serial, snapScript, false)
	po := ""
	if props {
		po = Exec(p.Serial, "getprop", false).Out
	}
	return &SettingsSnap{TS: time.Now(), Serial: p.Serial, Model: p.Model, Label: "now", Data: parseSnap(r.Out, props, po)}
}

type DiffLine struct {
	Op       string // + - ~
	Key      string
	Old, New string
}

func DiffSnaps(a, b *SettingsSnap) []DiffLine {
	var out []DiffLine
	for k, v := range b.Data {
		if ov, ok := a.Data[k]; !ok {
			out = append(out, DiffLine{"+", k, "", v})
		} else if ov != v {
			out = append(out, DiffLine{"~", k, ov, v})
		}
	}
	for k, v := range a.Data {
		if _, ok := b.Data[k]; !ok {
			out = append(out, DiffLine{"-", k, v, ""})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// ───────────────────────────── debloat snapshots ─────────────────────────────

type DebloatSnap struct {
	TS       time.Time         `json:"ts"`
	Serial   string            `json:"serial"`
	Model    string            `json:"model"`
	Reason   string            `json:"reason"`
	Packages map[string]string `json:"packages"` // pkg -> installed | disabled | removed (state BEFORE the change)
	File     string            `json:"-"`
}

func debloatDir() string { return filepath.Join(DataDir, "debloat") }

func SaveDebloatSnapshot(p *Profile, pkgs []string, reason string) (*DebloatSnap, error) {
	s := &DebloatSnap{TS: time.Now(), Serial: p.Serial, Model: p.Model, Reason: reason, Packages: map[string]string{}}
	for _, k := range pkgs {
		st := "removed"
		if p.Installed[k] {
			st = "installed"
			if p.Disabled[k] {
				st = "disabled"
			}
		}
		s.Packages[k] = st
	}
	MkdirP(debloatDir())
	s.File = filepath.Join(debloatDir(), fmt.Sprintf("%s-%s.json", safeName(p.Serial), s.TS.Format("20060102-150405")))
	b, _ := json.MarshalIndent(s, "", " ")
	return s, WritePrivate(s.File, b)
}

func ListDebloatSnapshots(serial string) []*DebloatSnap {
	m, _ := filepath.Glob(filepath.Join(debloatDir(), safeName(serial)+"-*.json"))
	sort.Sort(sort.Reverse(sort.StringSlice(m)))
	var out []*DebloatSnap
	for _, f := range m {
		var s DebloatSnap
		if b, err := os.ReadFile(f); err == nil && json.Unmarshal(b, &s) == nil {
			s.File = f
			out = append(out, &s)
		}
	}
	return out
}

// RestoreDebloat puts every package back to the state recorded in the snapshot (journalled).
func RestoreDebloat(p *Profile, s *DebloatSnap) (done int, errs []string) {
	for _, k := range SortedKeys(s.Packages) {
		want := s.Packages[k]
		o := Op{Kind: "pkg", Pkg: k}
		cur, _, _ := o.Read(p.Serial)
		if cur == want {
			continue
		}
		e := ApplyOp(p, "restore@"+s.TS.Format("150405"), "Restore "+k, o, want, false)
		if e.Err != "" {
			errs = append(errs, k+": "+e.Err)
		} else {
			done++
		}
	}
	return
}

// ───────────────────────────── UI dump ─────────────────────────────
