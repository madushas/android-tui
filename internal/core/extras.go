package core

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ───────────────────────────── property globs (vendor logger purge) ─────────────────────────────

type PropChange struct{ Key, Cur, New string }

var truthy = map[string]string{"1": "0", "true": "false", "on": "off", "yes": "no", "enabled": "disabled", "enable": "disable", "y": "n"}

func globRE(g string) *regexp.Regexp {
	return regexp.MustCompile("^" + strings.ReplaceAll(regexp.QuoteMeta(g), `\*`, ".*") + "$")
}

// GlobPlan lists the properties matching any glob whose value looks "enabled", with the value that would disable them.
func GlobPlan(serial string, globs []string) []PropChange {
	var res []regexp.Regexp
	for _, g := range globs {
		res = append(res, *globRE(strings.TrimSpace(g)))
	}
	var out []PropChange
	for _, kv := range ParseProps(Exec(serial, "getprop", false).Out) {
		for i := range res {
			if res[i].MatchString(kv.K) {
				if nv, ok := truthy[strings.ToLower(kv.V)]; ok {
					out = append(out, PropChange{kv.K, kv.V, nv})
				}
				break
			}
		}
	}
	return out
}

// ───────────────────────────── launcher / Shizuku ─────────────────────────────

// HomeActivities lists components that can act as the Home launcher, plus the current one.
func HomeActivities(serial string) (cands []string, current string) {
	r := Exec(serial, "cmd package query-activities --brief -a android.intent.action.MAIN -c android.intent.category.HOME", false)
	seen := map[string]bool{}
	for _, l := range strings.Split(r.Out, "\n") {
		l = strings.TrimSpace(l)
		if strings.Contains(l, "/") && !strings.Contains(l, " ") && !seen[l] {
			seen[l] = true
			cands = append(cands, l)
		}
	}
	cur := Exec(serial, "cmd package resolve-activity --brief -a android.intent.action.MAIN -c android.intent.category.HOME | tail -n1", false)
	return cands, strings.TrimSpace(cur.Out)
}

const shizukuStart = "/storage/emulated/0/Android/data/moe.shizuku.privileged.api/start.sh"

// ShizukuBootstrap runs Shizuku's start script (the app must be installed and have been opened once).
func ShizukuBootstrap(serial string) (bool, string) {
	if !strings.Contains(Exec(serial, "pm list packages moe.shizuku.privileged.api", false).Out, "moe.shizuku.privileged.api") {
		return false, "Shizuku (moe.shizuku.privileged.api) is not installed"
	}
	if r := Exec(serial, "ls "+shizukuStart, false); !r.OK() {
		return false, "start.sh not found — open the Shizuku app once so it creates it"
	}
	r := Exec(serial, "sh "+shizukuStart, false)
	out := strings.TrimSpace(r.Out + r.Err)
	if i := strings.LastIndex(out, "\n"); i >= 0 {
		out = out[i+1:]
	}
	time.Sleep(1500 * time.Millisecond)
	running := strings.TrimSpace(Exec(serial, "ps -A 2>/dev/null | grep -c shizuku_server", false).Out)
	if running != "" && running != "0" {
		return true, "Shizuku is running ✓"
	}
	return r.OK(), orDefault(out, "start.sh finished — check the Shizuku app")
}

// ───────────────────────────── settings snapshots & diff ─────────────────────────────

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
	return s, os.WriteFile(s.File, b, 0o644)
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
	return s, os.WriteFile(s.File, b, 0o644)
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

type xNode struct {
	Class    string  `xml:"class,attr"`
	Text     string  `xml:"text,attr"`
	ResID    string  `xml:"resource-id,attr"`
	Desc     string  `xml:"content-desc,attr"`
	Pkg      string  `xml:"package,attr"`
	Click    string  `xml:"clickable,attr"`
	Scroll   string  `xml:"scrollable,attr"`
	Checked  string  `xml:"checked,attr"`
	Enabled  string  `xml:"enabled,attr"`
	Bounds   string  `xml:"bounds,attr"`
	Children []xNode `xml:"node"`
}

type xRoot struct {
	Nodes []xNode `xml:"node"`
}

// UINode is a simplified view-hierarchy node.
type UINode struct {
	Class, Text, ID, Desc, Pkg              string
	Clickable, Scrollable, Checked, Enabled bool
	Bounds                                  [4]int
	Children                                []*UINode `json:",omitempty"`
}

var boundsRE = regexp.MustCompile(`\[(\d+),(\d+)\]\[(\d+),(\d+)\]`)

func conv(x xNode) *UINode {
	n := &UINode{Text: x.Text, Desc: x.Desc, Pkg: x.Pkg, Clickable: x.Click == "true", Scrollable: x.Scroll == "true", Checked: x.Checked == "true", Enabled: x.Enabled != "false"}
	n.Class = x.Class
	if i := strings.LastIndex(n.Class, "."); i >= 0 {
		n.Class = n.Class[i+1:]
	}
	if n.Class == "" {
		n.Class = "node"
	}
	n.ID = x.ResID
	if i := strings.Index(n.ID, ":id/"); i >= 0 {
		n.ID = n.ID[i+4:]
	}
	if m := boundsRE.FindStringSubmatch(x.Bounds); m != nil {
		for i := 0; i < 4; i++ {
			n.Bounds[i], _ = strconv.Atoi(m[i+1])
		}
	}
	for _, c := range x.Children {
		n.Children = append(n.Children, conv(c))
	}
	return n
}

// ParseUIDump parses uiautomator XML into a single root node.
func ParseUIDump(xmlText string) (*UINode, error) {
	i := strings.Index(xmlText, "<?xml")
	if i < 0 {
		i = strings.Index(xmlText, "<hierarchy")
	}
	if i < 0 {
		return nil, fmt.Errorf("no UI hierarchy in output: %s", Clip(strings.TrimSpace(xmlText), 120))
	}
	var r xRoot
	if err := xml.Unmarshal([]byte(xmlText[i:]), &r); err != nil {
		return nil, err
	}
	root := &UINode{Class: "hierarchy"}
	for _, n := range r.Nodes {
		root.Children = append(root.Children, conv(n))
	}
	if len(root.Children) == 1 {
		return root.Children[0], nil
	}
	return root, nil
}

func UIDump(serial string) (*UINode, error) {
	r := Sh(serial, "uiautomator dump /data/local/tmp/uidump.xml >/dev/null 2>&1; cat /data/local/tmp/uidump.xml; rm -f /data/local/tmp/uidump.xml", Opt{Timeout: 40 * time.Second})
	return ParseUIDump(r.Out)
}

func (n *UINode) informative() bool { return n.Text != "" || n.Desc != "" || n.ID != "" || n.Clickable }

func (n *UINode) label(bounds bool) string {
	s := "[" + n.Class + "]"
	if n.Text != "" {
		s += fmt.Sprintf(" text=%q", n.Text)
	}
	if n.ID != "" {
		s += fmt.Sprintf(" id=%q", n.ID)
	}
	if n.Desc != "" {
		s += fmt.Sprintf(" desc=%q", n.Desc)
	}
	if n.Clickable {
		s += " (clickable: true)"
	} else if n.Text != "" || n.ID != "" {
		s += " (clickable: false)"
	}
	if n.Checked {
		s += " (checked)"
	}
	if n.Scrollable {
		s += " (scrollable)"
	}
	if bounds && n.Bounds != [4]int{} {
		s += fmt.Sprintf(" bounds=[%d,%d][%d,%d]", n.Bounds[0], n.Bounds[1], n.Bounds[2], n.Bounds[3])
	}
	return s
}

// flatten hoists children of uninformative nodes when compact.
func (n *UINode) kids(compact bool) []*UINode {
	var out []*UINode
	for _, c := range n.Children {
		if compact && !c.informative() {
			out = append(out, c.kids(true)...)
		} else {
			out = append(out, c)
		}
	}
	return out
}

// RenderTree draws the hierarchy with box-drawing connectors.
func (n *UINode) RenderTree(compact, bounds bool) []string {
	out := []string{n.label(bounds)}
	var walk func(x *UINode, prefix string)
	walk = func(x *UINode, prefix string) {
		ks := x.kids(compact)
		for i, c := range ks {
			conn, next := "├── ", "│   "
			if i == len(ks)-1 {
				conn, next = "└── ", "    "
			}
			out = append(out, prefix+conn+c.label(bounds))
			walk(c, prefix+next)
		}
	}
	walk(n, "")
	return out
}

func (n *UINode) RenderMarkdown(compact bool) string {
	var sb strings.Builder
	var walk func(x *UINode, depth int)
	walk = func(x *UINode, depth int) {
		fmt.Fprintf(&sb, "%s- %s\n", strings.Repeat("  ", depth), x.label(true))
		for _, c := range x.kids(compact) {
			walk(c, depth+1)
		}
	}
	walk(n, 0)
	return sb.String()
}

func (n *UINode) RenderJSON(compact bool) string {
	var conv func(x *UINode) map[string]any
	conv = func(x *UINode) map[string]any {
		m := map[string]any{"class": x.Class}
		if x.Text != "" {
			m["text"] = x.Text
		}
		if x.ID != "" {
			m["id"] = x.ID
		}
		if x.Desc != "" {
			m["desc"] = x.Desc
		}
		if x.Clickable {
			m["clickable"] = true
		}
		if x.Bounds != [4]int{} {
			m["bounds"] = x.Bounds
		}
		var ks []map[string]any
		for _, c := range x.kids(compact) {
			ks = append(ks, conv(c))
		}
		if len(ks) > 0 {
			m["children"] = ks
		}
		return m
	}
	b, _ := json.MarshalIndent(conv(n), "", "  ")
	return string(b)
}

// Targets lists tappable/informative leaf-ish nodes (clickable, or with text), for the "tap element" picker.
func (n *UINode) Targets() []*UINode {
	var out []*UINode
	var walk func(x *UINode)
	walk = func(x *UINode) {
		if (x.Clickable || x.Text != "") && x.Bounds != [4]int{} && (x.Text != "" || x.Desc != "" || x.ID != "") {
			out = append(out, x)
		}
		for _, c := range x.Children {
			walk(c)
		}
	}
	walk(n)
	return out
}

func (n *UINode) Center() (int, int) {
	return (n.Bounds[0] + n.Bounds[2]) / 2, (n.Bounds[1] + n.Bounds[3]) / 2
}

// ───────────────────────────── processes (top + meminfo) ─────────────────────────────

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

type LogPreset struct {
	Name, Regex string
	MinLevel    int // index into VDIWEFS
}

var LogPresets = []LogPreset{
	{"Crashes", `FATAL EXCEPTION|AndroidRuntime|Fatal signal|SIGSEGV|SIGABRT|Process: .* has died|tombstone|backtrace:`, 0},
	{"ANRs", `ANR in|am_anr|Input dispatching timed out|not responding|Reason: Input|executing service`, 0},
	{"GC pauses", `GC freed|Background concurrent|concurrent copying|Paused GC|Explicit concurrent|Starting a blocking GC`, 0},
	{"Low memory / process kills", `lmkd|lowmemorykiller|Killing .* \(adj|Phantom|am_kill|am_proc_died|OOM`, 0},
	{"Battery saver / Doze", `BatterySaver|PowerManagerService.*(low power|battery saver)|DeviceIdleController|deep idle|light idle|Doze`, 0},
	{"Wakelocks", `WakeLock|wake_lock|acquire.*wakelock|release.*wakelock`, 0},
	{"Job scheduler", `JobScheduler|JobServiceContext|jobscheduler|StandbyBucket|AppStandby`, 0},
	{"Warnings and errors only", `.`, 3},
}
