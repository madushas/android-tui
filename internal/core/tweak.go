package core

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed packs/*.json
var builtinPacks embed.FS

// ───────────────────────────── data model (JSON packs) ─────────────────────────────

// Op is one reversible (or one-shot) change on the device.
type Op struct {
	Kind     string `json:"kind"` // setting device_config prop appops bucket pkg wm doze home shell cleanup
	NS       string `json:"ns,omitempty"`
	Key      string `json:"key,omitempty"`
	Pkg      string `json:"pkg,omitempty"`
	Op       string `json:"op,omitempty"` // appops op name | wm "density"/"size"
	On       string `json:"on,omitempty"`
	Off      string `json:"off,omitempty"`
	OffUnset bool   `json:"off_unset,omitempty"` // turning the tweak off deletes the key instead of writing Off
	Cmd      string `json:"cmd,omitempty"`       // shell/cleanup apply command
	Undo     string `json:"undo,omitempty"`      // shell undo command
	Root     bool   `json:"root,omitempty"`
	Fallback string `json:"fallback_ns,omitempty"` // for ns "auto" when the key is not found anywhere
}

type Requires struct {
	MinSDK  int      `json:"min_sdk,omitempty"`
	MaxSDK  int      `json:"max_sdk,omitempty"`
	Form    []string `json:"form,omitempty"`
	SoC     []string `json:"soc,omitempty"`
	Brand   []string `json:"brand,omitempty"`
	Root    bool     `json:"root,omitempty"`
	PkgsAny []string `json:"pkgs_any,omitempty"`
	KeysAny []string `json:"keys_any,omitempty"` // settings/props that must exist on the device
}

type Preset struct{ Label, Value string }

type Tweak struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Category string   `json:"category"`
	Desc     string   `json:"desc"`
	Risk     string   `json:"risk"` // safe | caution | risky
	Reboot   bool     `json:"reboot,omitempty"`
	Action   bool     `json:"action,omitempty"` // one-shot (compile, fstrim…): no on/off state
	Requires Requires `json:"requires,omitempty"`
	Special  string   `json:"special,omitempty"` // home_picker | value | prop_glob
	Cmd      string   `json:"cmd,omitempty"`     // special helpers (globs…)
	Ops      []Op     `json:"ops"`
	Presets  []Preset `json:"presets,omitempty"`
	Pack     string   `json:"-"`
}

type DebloatEntry struct {
	Pkg      string   `json:"pkg"`
	Label    string   `json:"label"`
	Risk     string   `json:"risk"`
	Why      string   `json:"why"`
	Requires Requires `json:"requires,omitempty"`
	Pack     string   `json:"-"`
}

type Pack struct {
	Pack     string         `json:"pack"`
	Title    string         `json:"title"`
	Requires Requires       `json:"requires,omitempty"`
	Tweaks   []Tweak        `json:"tweaks"`
	Debloat  []DebloatEntry `json:"debloat"`
}

// LoadPacks returns the built-in packs plus any *.json in ~/.config/android-tui/packs/ (user packs may override ids).
func LoadPacks() []Pack {
	var out []Pack
	read := func(name string, b []byte, user bool) {
		var p Pack
		if err := json.Unmarshal(b, &p); err != nil {
			Logf("err", "pack %s: %v", name, err)
			return
		}
		for i := range p.Tweaks {
			p.Tweaks[i].Pack = p.Pack
			if p.Tweaks[i].Risk == "" {
				p.Tweaks[i].Risk = "caution"
			}
		}
		for i := range p.Debloat {
			p.Debloat[i].Pack = p.Pack
		}
		out = append(out, p)
	}
	ents, _ := builtinPacks.ReadDir("packs")
	for _, e := range ents {
		if b, err := builtinPacks.ReadFile("packs/" + e.Name()); err == nil {
			read(e.Name(), b, false)
		}
	}
	if m, _ := filepath.Glob(filepath.Join(ConfDir, "packs", "*.json")); len(m) > 0 {
		for _, f := range m {
			if b, err := os.ReadFile(f); err == nil {
				read(f, b, true)
			}
		}
	}
	return out
}

// Applicable reports whether a requirement set is satisfied by the profile; reason explains a "no".
func (r Requires) Applicable(p *Profile, keyExists func(string) bool) (bool, string) {
	if p == nil {
		return false, "device not probed"
	}
	if r.MinSDK > 0 && p.SDK < r.MinSDK {
		return false, fmt.Sprintf("needs Android API %d+ (this is %d)", r.MinSDK, p.SDK)
	}
	if r.MaxSDK > 0 && p.SDK > r.MaxSDK {
		return false, fmt.Sprintf("only for API ≤ %d (this is %d)", r.MaxSDK, p.SDK)
	}
	if len(r.Form) > 0 && !contains(r.Form, p.Form) {
		return false, "for " + strings.Join(r.Form, "/") + " devices (this is " + p.Form + ")"
	}
	if len(r.SoC) > 0 && !contains(r.SoC, p.Vendor) {
		return false, "for " + strings.Join(r.SoC, "/") + " SoCs (this is " + p.Vendor + ")"
	}
	if len(r.Brand) > 0 {
		ok := false
		for _, b := range r.Brand {
			ok = ok || p.HasBrand(b)
		}
		if !ok {
			return false, "for " + strings.Join(r.Brand, "/") + " devices"
		}
	}
	if r.Root && !p.Root {
		return false, "needs root"
	}
	if len(r.PkgsAny) > 0 {
		ok := false
		for _, k := range r.PkgsAny {
			ok = ok || p.Installed[k]
		}
		if !ok {
			return false, "none of its packages are installed here"
		}
	}
	if len(r.KeysAny) > 0 && keyExists != nil {
		ok := false
		for _, k := range r.KeysAny {
			ok = ok || keyExists(k)
		}
		if !ok {
			return false, "its settings/properties don't exist on this device"
		}
	}
	return true, ""
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// ───────────────────────────── device access (swappable for tests) ─────────────────────────────

// Exec runs a shell script on the device; root=true wraps it in su.
var Exec = func(serial, script string, root bool) Res {
	if root {
		script = "su -c " + Quote(script)
	}
	return Sh(serial, script, Opt{Timeout: 40 * time.Second})
}

var (
	idxMu  sync.Mutex
	idxMap = map[string]map[string]string{} // serial -> key -> settings namespace
)

// noteSetting updates the cached settings index after a write (new keys become visible without a full refresh).
func noteSetting(serial, ns, key string, present bool) {
	idxMu.Lock()
	defer idxMu.Unlock()
	if m, ok := idxMap[serial]; ok {
		if present {
			m[key] = ns
		} else {
			delete(m, key)
		}
	}
}

// SettingsIndex maps every settings key to its namespace (global/secure/system) — cached per device.
func SettingsIndex(serial string, refresh bool) map[string]string {
	idxMu.Lock()
	defer idxMu.Unlock()
	if m, ok := idxMap[serial]; ok && !refresh {
		return m
	}
	r := Exec(serial, `for ns in system secure global; do echo "@@$ns"; settings list $ns; done`, false)
	m := map[string]string{}
	ns := ""
	for _, l := range strings.Split(r.Out, "\n") {
		if strings.HasPrefix(l, "@@") {
			ns = l[2:]
		} else if k, _, ok := strings.Cut(l, "="); ok && ns != "" {
			m[k] = ns
		}
	}
	idxMap[serial] = m
	return m
}

// resolved turns kind "auto" into a concrete setting/prop op for this device.
func (o Op) resolved(serial string) Op {
	if o.Kind != "auto" {
		return o
	}
	if ns := SettingsIndex(serial, false)[o.Key]; ns != "" {
		o.Kind, o.NS = "setting", ns
		return o
	}
	if strings.TrimSpace(Exec(serial, "getprop "+Quote(o.Key), false).Out) != "" {
		o.Kind = "prop"
		return o
	}
	o.Kind, o.NS = "setting", orDefault(o.Fallback, "global")
	return o
}

func (o Op) ns(serial string) string {
	if o.Kind != "setting" || o.NS != "auto" {
		return o.NS
	}
	if ns := SettingsIndex(serial, false)[o.Key]; ns != "" {
		return ns
	}
	return orDefault(o.Fallback, "global")
}

func orDefault(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// KeyExists answers "does this settings key / system property exist on the device?" (for vendor-specific tweaks).
// kxCache remembers getprop existence checks for 60s: Catalog() asks the same questions on every Tweaks reload.
var (
	kxMu    sync.Mutex
	kxCache = map[string]kxEnt{}
)

type kxEnt struct {
	ok bool
	at time.Time
}

func KeyExists(serial string, p *Profile, key string) bool {
	if _, ok := SettingsIndex(serial, false)[key]; ok {
		return true
	}
	ck := serial + "\x00" + key
	kxMu.Lock()
	e, hit := kxCache[ck]
	kxMu.Unlock()
	if hit && time.Since(e.at) < 60*time.Second {
		return e.ok
	}
	ok := strings.TrimSpace(Exec(serial, "getprop "+Quote(key), false).Out) != ""
	kxMu.Lock()
	kxCache[ck] = kxEnt{ok, time.Now()}
	kxMu.Unlock()
	return ok
}

var bucketNames = map[string]string{"5": "exempted", "10": "active", "20": "working_set", "30": "frequent", "40": "rare", "45": "restricted", "50": "never",
	"ACTIVE": "active", "WORKING_SET": "working_set", "FREQUENT": "frequent", "RARE": "rare", "RESTRICTED": "restricted", "EXEMPTED": "exempted", "NEVER": "never"}

func (o Op) readScript(serial string) string {
	switch o.Kind {
	case "setting":
		return fmt.Sprintf("settings get %s %s", o.ns(serial), Quote(o.Key))
	case "device_config":
		return fmt.Sprintf("device_config get %s %s", Quote(o.NS), Quote(o.Key))
	case "prop":
		return "getprop " + Quote(o.Key)
	case "appops":
		return fmt.Sprintf("cmd appops get %s %s", Quote(o.Pkg), Quote(o.Op))
	case "bucket":
		return "am get-standby-bucket " + Quote(o.Pkg)
	case "pkg":
		q := Quote(o.Pkg)
		return fmt.Sprintf(`i=$(pm list packages %s | grep -x package:%s); d=$(pm list packages -d %s | grep -x package:%s); echo "installed=${i:+1} disabled=${d:+1}"`, q, q, q, q)
	case "wm":
		return "wm " + o.Op
	case "doze":
		return fmt.Sprintf("dumpsys deviceidle whitelist | grep -F %s | head -n1", Quote(","+o.Pkg+","))
	case "home":
		return "cmd package resolve-activity --brief -a android.intent.action.MAIN -c android.intent.category.HOME | tail -n1"
	}
	return ""
}

// Readable reports whether the op's current value can be read (one-shot ops cannot).
func (o Op) Readable() bool { return o.readScript("") != "" }

var kvRE = regexp.MustCompile(`(?:Override|Physical)? ?(?:density|size): ?(\S+)`)

// parseRead normalises command output to (value, exists).
func (o Op) parseRead(out string) (string, bool) {
	out = strings.TrimSpace(out)
	switch o.Kind {
	case "setting", "device_config":
		if out == "null" || out == "" {
			return "", false
		}
		return out, true
	case "prop":
		return out, out != ""
	case "appops":
		if i := strings.Index(out, ": "); i >= 0 {
			v := strings.Fields(strings.SplitN(out[i+2:], ";", 2)[0])
			if len(v) > 0 {
				return v[0], true
			}
		}
		return "default", true
	case "bucket":
		v := strings.TrimSpace(strings.Fields(out + " x")[0])
		if n, ok := bucketNames[strings.ToUpper(v)]; ok {
			return n, true
		}
		return strings.ToLower(v), v != ""
	case "pkg":
		switch {
		case !strings.Contains(out, "installed=1"):
			return "removed", true
		case strings.Contains(out, "disabled=1"):
			return "disabled", true
		}
		return "installed", true
	case "wm":
		val := "reset"
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, "Override") {
				if m := kvRE.FindStringSubmatch(l); m != nil {
					val = m[1]
				}
			}
		}
		return val, true
	case "doze":
		if out != "" {
			return "whitelisted", true
		}
		return "none", true
	case "home":
		return out, out != ""
	}
	return "", false
}

// Read returns the op's current value; exists=false means "unset".
func (o Op) Read(serial string) (string, bool, bool) {
	o = o.resolved(serial)
	s := o.readScript(serial)
	if s == "" {
		return "", false, false
	}
	r := Exec(serial, s, o.Root)
	v, ex := o.parseRead(r.Out)
	return v, ex, true
}

func (o Op) writeScript(serial, v string) string {
	switch o.Kind {
	case "setting":
		return fmt.Sprintf("settings put %s %s %s", o.ns(serial), Quote(o.Key), Quote(v))
	case "device_config":
		return fmt.Sprintf("device_config put %s %s %s", Quote(o.NS), Quote(o.Key), Quote(v))
	case "prop":
		return fmt.Sprintf("setprop %s %s", Quote(o.Key), Quote(v))
	case "appops":
		return fmt.Sprintf("cmd appops set %s %s %s", Quote(o.Pkg), Quote(o.Op), Quote(v))
	case "bucket":
		return fmt.Sprintf("am set-standby-bucket %s %s", Quote(o.Pkg), Quote(v))
	case "pkg":
		q := Quote(o.Pkg)
		switch v {
		case "disabled":
			return "pm disable-user --user 0 " + q
		case "removed":
			return "pm uninstall -k --user 0 " + q
		}
		return fmt.Sprintf("pm enable %s; pm install-existing --user 0 %s", q, q)
	case "wm":
		if v == "reset" || v == "" {
			return "wm " + o.Op + " reset"
		}
		return fmt.Sprintf("wm %s %s", o.Op, Quote(v))
	case "doze":
		if v == "none" {
			return "dumpsys deviceidle whitelist -" + o.Pkg
		}
		return "dumpsys deviceidle whitelist +" + o.Pkg
	case "home":
		return "cmd package set-home-activity " + Quote(v)
	case "shell":
		return strings.ReplaceAll(o.Cmd, "{v}", v)
	case "cleanup":
		return "rm -rf " + Quote(o.Key)
	}
	return "true"
}

func (o Op) unsetScript(serial string) string {
	switch o.Kind {
	case "setting":
		return fmt.Sprintf("settings delete %s %s", o.ns(serial), Quote(o.Key))
	case "device_config":
		return fmt.Sprintf("device_config delete %s %s", Quote(o.NS), Quote(o.Key))
	case "prop":
		return fmt.Sprintf("setprop %s ''", Quote(o.Key))
	}
	return ""
}

// ───────────────────────────── journal ─────────────────────────────

// JEntry is one journalled change. Prev/PrevExists are what the device had before.
type JEntry struct {
	ID         int64     `json:"id"`
	TS         time.Time `json:"ts"`
	Serial     string    `json:"serial"`
	Model      string    `json:"model"`
	Group      string    `json:"group"`
	Title      string    `json:"title"`
	Op         Op        `json:"op"`
	NS         string    `json:"ns,omitempty"` // resolved settings namespace
	Prev       string    `json:"prev"`
	PrevExists bool      `json:"prev_exists"`
	New        string    `json:"new"`
	NewUnset   bool      `json:"new_unset,omitempty"`
	Verified   bool      `json:"verified"`
	Reverted   bool      `json:"reverted,omitempty"`
	Irrev      bool      `json:"irreversible,omitempty"`
	Err        string    `json:"err,omitempty"`
}

func (e JEntry) Describe() string {
	o := e.Op
	what := o.Key
	switch o.Kind {
	case "appops":
		what = o.Pkg + " " + o.Op
	case "bucket", "pkg", "doze":
		what = o.Pkg
	case "wm":
		what = "wm " + o.Op
	case "shell", "cleanup":
		what = strings.TrimSpace(orDefault(o.Cmd, o.Key))
	}
	ns := e.NS
	if ns == "" {
		ns = o.NS
	}
	if ns != "" && (o.Kind == "setting" || o.Kind == "device_config") {
		what = ns + "/" + what
	}
	prev := e.Prev
	if !e.PrevExists {
		prev = "∅"
	}
	nv := e.New
	if e.NewUnset {
		nv = "∅"
	}
	if e.Irrev {
		return fmt.Sprintf("%s: %s  (one-shot)", o.Kind, what)
	}
	return fmt.Sprintf("%s: %s  %s → %s", o.Kind, what, prev, nv)
}

var jMu sync.Mutex

func journalPath() string { return filepath.Join(DataDir, "journal.json") }

func loadJournalLocked() []JEntry {
	var es []JEntry
	if b, err := os.ReadFile(journalPath()); err == nil {
		_ = json.Unmarshal(b, &es)
	}
	return es
}

func saveJournalLocked(es []JEntry) {
	MkdirP(DataDir)
	b, _ := json.MarshalIndent(es, "", " ")
	tmp := journalPath() + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, journalPath())
	}
}

func LoadJournal() []JEntry { jMu.Lock(); defer jMu.Unlock(); return loadJournalLocked() }

// lockJournal serialises read-modify-write cycles across processes (TUI + CLI) with an O_EXCL lock file.
// A lock older than 10s is treated as stale (a crashed process) and taken over. If the lock cannot be
// obtained within ~4s we proceed unlocked rather than lose the record of a change already made on the device.
func lockJournal() func() {
	MkdirP(DataDir)
	lp := journalPath() + ".lock"
	for i := 0; i < 200; i++ {
		if f, err := os.OpenFile(lp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644); err == nil {
			_ = f.Close()
			return func() { _ = os.Remove(lp) }
		}
		if st, err := os.Stat(lp); err == nil && time.Since(st.ModTime()) > 10*time.Second {
			_ = os.Remove(lp)
			continue
		}
		time.Sleep(20 * time.Millisecond)
	}
	return func() {}
}

func appendJournal(es ...JEntry) {
	jMu.Lock()
	defer jMu.Unlock()
	defer lockJournal()()
	saveJournalLocked(append(loadJournalLocked(), es...))
}

func markReverted(ids map[int64]bool) {
	jMu.Lock()
	defer jMu.Unlock()
	defer lockJournal()()
	es := loadJournalLocked()
	for i := range es {
		if ids[es[i].ID] {
			es[i].Reverted = true
		}
	}
	saveJournalLocked(es)
}

var idSeq int64

// newID is unique per process even when called from several goroutines (atomic counter + clock).
func newID() int64 {
	return time.Now().UnixNano() + atomic.AddInt64(&idSeq, 1)
}

// ───────────────────────────── apply / revert ─────────────────────────────

// ApplyOp reads the previous value, writes the new one, verifies it stuck and journals the change.
// unset=true deletes the key instead of writing val.
func ApplyOp(p *Profile, group, title string, o Op, val string, unset bool) JEntry {
	serial := p.Serial
	o = o.resolved(serial)
	e := JEntry{ID: newID(), TS: time.Now(), Serial: serial, Model: p.Model, Group: group, Title: title, Op: o, New: val, NewUnset: unset}
	if o.Kind == "setting" {
		e.NS = o.ns(serial)
	}
	prev, ex, readable := o.Read(serial)
	e.Prev, e.PrevExists = prev, ex
	script := o.writeScript(serial, val)
	if unset {
		script = o.unsetScript(serial)
	}
	if script == "" {
		e.Err = "this op can't be unset"
		return e
	}
	r := Exec(serial, script, o.Root)
	out := strings.TrimSpace(r.Out)
	if !r.OK() || strings.HasPrefix(out, "Error") || strings.HasPrefix(out, "Exception") || strings.Contains(out, "Permission denied") ||
		strings.Contains(r.Err, "Permission denied") || strings.Contains(out, "SecurityException") {
		e.Err = orDefault(strings.TrimSpace(r.Err+" "+out), r.Why())
		if idx := strings.Index(e.Err, "\n"); idx > 0 {
			e.Err = e.Err[:idx]
		}
		return e // nothing changed → don't journal
	}
	if o.Kind == "setting" {
		noteSetting(serial, e.NS, o.Key, !unset) // keep the cached key→namespace index current
	}
	switch {
	case !readable || o.Kind == "shell" || o.Kind == "cleanup":
		e.Irrev = o.Kind == "cleanup" || (o.Kind == "shell" && o.Undo == "")
		e.Verified = true
	default:
		nv, nex, _ := o.Read(serial)
		if unset {
			e.Verified = !nex
		} else {
			e.Verified = nex && nv == val
			if o.Kind == "pkg" && val == "installed" {
				e.Verified = nv == "installed"
			}
		}
	}
	appendJournal(e)
	return e
}

// RevertEntry puts the previous value back and marks the entry reverted.
func RevertEntry(e JEntry) (bool, string) {
	if e.Irrev {
		return false, "one-shot action — nothing to revert"
	}
	o := e.Op
	var r Res
	switch {
	case o.Kind == "shell":
		r = Exec(e.Serial, strings.ReplaceAll(o.Undo, "{v}", e.Prev), o.Root)
	case !e.PrevExists && o.unsetScript(e.Serial) != "":
		r = Exec(e.Serial, o.unsetScript(e.Serial), o.Root)
	default:
		r = Exec(e.Serial, o.writeScript(e.Serial, e.Prev), o.Root)
	}
	if !r.OK() {
		return false, r.Why()
	}
	markReverted(map[int64]bool{e.ID: true})
	return true, "reverted " + e.Describe()
}

// LastGroup returns the newest journal group of a device that can still be undone, newest entry first.
// An entry without a group counts as a group of its own.
func LastGroup(serial string) []JEntry {
	es := LoadJournal()
	live := func(e JEntry) bool { return e.Serial == serial && !e.Reverted && !e.Irrev && e.Err == "" }
	for i := len(es) - 1; i >= 0; i-- {
		if !live(es[i]) {
			continue
		}
		if es[i].Group == "" {
			return []JEntry{es[i]}
		}
		var out []JEntry
		for j := len(es) - 1; j >= 0; j-- {
			if live(es[j]) && es[j].Group == es[i].Group {
				out = append(out, es[j])
			}
		}
		return out
	}
	return nil
}

// RevertEntries undoes the given entries in the order given (newest first for correct stacking).
func RevertEntries(es []JEntry) (done, failed int, msgs []string) {
	for _, e := range es {
		if ok, m := RevertEntry(e); ok {
			done++
		} else {
			failed++
			msgs = append(msgs, e.Title+": "+m)
		}
	}
	return
}

// RevertAll undoes every non-reverted entry for a device, newest first.
func RevertAll(serial string) (done, failed int, msgs []string) {
	es := LoadJournal()
	for i := len(es) - 1; i >= 0; i-- {
		e := es[i]
		if e.Serial != serial || e.Reverted || e.Irrev {
			continue
		}
		if ok, m := RevertEntry(e); ok {
			done++
		} else {
			failed++
			msgs = append(msgs, e.Title+": "+m)
		}
	}
	return
}

// ───────────────────────────── tweak state & apply ─────────────────────────────

func sub(s, v string) string { return strings.ReplaceAll(s, "{v}", v) }

// opRead is one op's current value as read from the device.
type opRead struct {
	Val      string
	Exists   bool
	Readable bool
}

// TweakState reads each op and returns on | off | partial | unknown | action, plus the current value of the first op.
func TweakState(p *Profile, t Tweak) (string, string) {
	if t.Action || t.Special == "prop_glob" {
		return "action", ""
	}
	reads := make([]opRead, len(t.Ops))
	for i, o := range t.Ops {
		v, ex, ok := o.Read(p.Serial)
		reads[i] = opRead{v, ex, ok}
	}
	return stateOf(t, reads)
}

// stateOf turns already-read op values into the tweak state (pure: shared by TweakState and TweakStates).
func stateOf(t Tweak, reads []opRead) (string, string) {
	if t.Action || t.Special == "prop_glob" {
		return "action", ""
	}
	on, off, n := 0, 0, 0
	first := ""
	for i, o := range t.Ops {
		if !reads[i].Readable {
			continue
		}
		v, ex := reads[i].Val, reads[i].Exists
		n++
		if i == 0 || first == "" {
			first = v
			if !ex {
				first = "∅"
			}
		}
		if len(t.Presets) == 0 {
			if ex && v == o.On {
				on++
			}
			if !ex || (ex && v == o.Off) { // an absent key means "default", i.e. not On
				off++
			}
		}
	}
	if len(t.Presets) > 0 || t.Special == "value" {
		return "preset", first
	}
	switch {
	case n == 0:
		return "unknown", first
	case on == n:
		return "on", first
	case off == n:
		return "off", first
	}
	return "partial", first
}

// readMany reads many ops with few adb calls (40 per call; root ops are read one by one).
// Each op's output is delimited by an "@@<index>" marker line. Results align with ops.
func readMany(serial string, ops []Op) []opRead {
	res := make([]opRead, len(ops))
	const chunk = 40
	for from := 0; from < len(ops); from += chunk {
		to := min(from+chunk, len(ops))
		batched := map[int]Op{}
		var parts []string
		for i := from; i < to; i++ {
			o := ops[i].resolved(serial)
			s := o.readScript(serial)
			if s == "" {
				continue // one-shot op: not readable
			}
			res[i].Readable = true
			if o.Root {
				res[i].Val, res[i].Exists, _ = o.Read(serial)
				continue
			}
			batched[i] = o
			parts = append(parts, fmt.Sprintf("echo '@@%d'", i), s)
		}
		if len(batched) == 0 {
			continue
		}
		out := Exec(serial, strings.Join(parts, "; "), false).Out
		lines := map[int][]string{}
		cur := -1
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, "@@") {
				if n, err := strconv.Atoi(strings.TrimSpace(l[2:])); err == nil {
					cur = n
					continue
				}
			}
			if cur >= 0 {
				lines[cur] = append(lines[cur], l)
			}
		}
		for i, o := range batched {
			res[i].Val, res[i].Exists = o.parseRead(strings.Join(lines[i], "\n"))
		}
	}
	return res
}

// TweakStates is TweakState for many tweaks at once (one adb call per 40 ops instead of one per op).
// The result aligns with ts: [state, current value of the first op].
func TweakStates(p *Profile, ts []Tweak) [][2]string {
	oneShot := func(t Tweak) bool { return t.Action || t.Special == "prop_glob" }
	var ops []Op
	for _, t := range ts {
		if !oneShot(t) {
			ops = append(ops, t.Ops...)
		}
	}
	reads := readMany(p.Serial, ops)
	out := make([][2]string, len(ts))
	k := 0
	for i, t := range ts {
		if oneShot(t) {
			out[i] = [2]string{"action", ""}
			continue
		}
		st, cur := stateOf(t, reads[k:k+len(t.Ops)])
		out[i] = [2]string{st, cur}
		k += len(t.Ops)
	}
	return out
}

// ApplyTweak applies mode "on", "off" or a preset value across all of the tweak's ops (one journal group).
func ApplyTweak(p *Profile, t Tweak, mode string) []JEntry {
	group := fmt.Sprintf("%s@%d", t.ID, time.Now().UnixNano()) // unique: one group = one undo step
	var out []JEntry
	for _, o := range t.Ops {
		var val string
		unset := false
		switch mode {
		case "off":
			val, unset = o.Off, o.OffUnset
		case "on":
			val = o.On
		default:
			val = mode
		}
		if len(t.Presets) > 0 {
			val = sub(o.On, mode)
		}
		val = sub(val, mode)
		out = append(out, ApplyOp(p, group, t.Title, o, val, unset))
		if out[len(out)-1].Err != "" {
			break
		}
	}
	return out
}

// RevertTweak reverts the most recent non-reverted journal group for a tweak title on this device.
func RevertTweak(serial, title string) (int, string) {
	es := LoadJournal()
	group := ""
	for i := len(es) - 1; i >= 0; i-- {
		if es[i].Serial == serial && es[i].Title == title && !es[i].Reverted && !es[i].Irrev {
			group = es[i].Group
			break
		}
	}
	if group == "" {
		return 0, "nothing to revert for this tweak"
	}
	n := 0
	for i := len(es) - 1; i >= 0; i-- {
		if es[i].Group == group && !es[i].Reverted {
			if ok, m := RevertEntry(es[i]); ok {
				n++
			} else {
				return n, m
			}
		}
	}
	return n, fmt.Sprintf("reverted %d change(s)", n)
}

// Catalog resolves which tweaks/debloat entries apply to a device.
type CatalogTweak struct {
	Tweak
	OK     bool
	Reason string
}

func Catalog(p *Profile) ([]CatalogTweak, []DebloatEntry, []DebloatEntry) {
	exists := func(k string) bool { return KeyExists(p.Serial, p, k) }
	var tw []CatalogTweak
	var deb, hidden []DebloatEntry
	for _, pk := range LoadPacks() {
		packOK, packWhy := pk.Requires.Applicable(p, exists)
		for _, t := range pk.Tweaks {
			ok, why := t.Requires.Applicable(p, exists)
			if !packOK {
				ok, why = false, packWhy
			}
			tw = append(tw, CatalogTweak{t, ok, why})
		}
		for _, d := range pk.Debloat {
			if ok, _ := d.Requires.Applicable(p, exists); ok && packOK && p.Installed[d.Pkg] {
				deb = append(deb, d)
			} else if ok && packOK {
				hidden = append(hidden, d)
			}
		}
	}
	sort.SliceStable(tw, func(i, j int) bool { return tw[i].Category < tw[j].Category })
	seen := map[string]bool{}
	var uniq []DebloatEntry
	for _, d := range deb {
		if !seen[d.Pkg] {
			seen[d.Pkg] = true
			uniq = append(uniq, d)
		}
	}
	sort.SliceStable(uniq, func(i, j int) bool { return uniq[i].Pack+uniq[i].Pkg < uniq[j].Pack+uniq[j].Pkg })
	return tw, uniq, hidden
}
