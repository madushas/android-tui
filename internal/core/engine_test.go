package core

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeDev is a tiny in-memory Android shell: just enough of settings/device_config/getprop/appops/bucket/pm for the engine.
type fakeDev struct {
	kv   map[string]string // "settings/global/key", "dc/ns/key", "prop/key", "appops/pkg/OP", "bucket/pkg"
	pkgs map[string]string // installed | disabled | removed
}

func newFake() *fakeDev { return &fakeDev{kv: map[string]string{}, pkgs: map[string]string{}} }

func (f *fakeDev) run(serial, script string, root bool) Res {
	cmds := strings.Split(script, "; ")
	var out strings.Builder
	for _, c := range cmds {
		a := ShellSplit(c)
		if len(a) == 0 {
			continue
		}
		switch {
		case a[0] == "settings" && a[1] == "get":
			out.WriteString(f.get("settings/"+a[2]+"/"+a[3], "null") + "\n")
		case a[0] == "settings" && a[1] == "put":
			f.kv["settings/"+a[2]+"/"+a[3]] = a[4]
		case a[0] == "settings" && a[1] == "delete":
			delete(f.kv, "settings/"+a[2]+"/"+a[3])
		case a[0] == "device_config" && a[1] == "get":
			out.WriteString(f.get("dc/"+a[2]+"/"+a[3], "null") + "\n")
		case a[0] == "device_config" && a[1] == "put":
			f.kv["dc/"+a[2]+"/"+a[3]] = a[4]
		case a[0] == "device_config" && a[1] == "delete":
			delete(f.kv, "dc/"+a[2]+"/"+a[3])
		case a[0] == "echo" && len(a) > 1 && strings.HasPrefix(a[1], "@@"):
			out.WriteString(a[1] + "\n") // batch markers from readMany
		case a[0] == "getprop":
			out.WriteString(f.get("prop/"+a[1], "") + "\n")
		case a[0] == "setprop":
			f.kv["prop/"+a[1]] = a[2]
		case a[0] == "cmd" && a[1] == "appops" && a[2] == "get":
			out.WriteString(a[4] + ": " + f.get("appops/"+a[3]+"/"+a[4], "allow") + "\n")
		case a[0] == "cmd" && a[1] == "appops" && a[2] == "set":
			f.kv["appops/"+a[3]+"/"+a[4]] = a[5]
		case a[0] == "am" && a[1] == "get-standby-bucket":
			out.WriteString(f.get("bucket/"+a[2], "10") + "\n")
		case a[0] == "am" && a[1] == "set-standby-bucket":
			f.kv["bucket/"+a[2]] = map[string]string{"active": "10", "rare": "40", "restricted": "45"}[a[3]]
		case a[0] == "pm" && a[1] == "disable-user":
			f.pkgs[a[4]] = "disabled"
		case a[0] == "pm" && a[1] == "enable":
			if f.pkgs[a[2]] == "disabled" {
				f.pkgs[a[2]] = "installed"
			}
		case a[0] == "pm" && a[1] == "install-existing":
			f.pkgs[a[4]] = "installed"
		case a[0] == "pm" && a[1] == "uninstall":
			f.pkgs[a[5]] = "removed"
		case strings.HasPrefix(c, "i=$(pm list packages"):
			pkg := a[0]
			_ = pkg
			for p, st := range f.pkgs {
				if strings.Contains(c, "package:"+p+")") {
					d := ""
					if st == "disabled" {
						d = "1"
					}
					i := "1"
					if st == "removed" {
						i = ""
					}
					fmt.Fprintf(&out, "installed=%s disabled=%s\n", i, d)
				}
			}
		case strings.HasPrefix(c, "for ns in system"):
			out.WriteString("@@global\nfoo=1\n")
		}
	}
	return Res{Out: out.String()}
}

func (f *fakeDev) get(k, def string) string {
	if v, ok := f.kv[k]; ok {
		return v
	}
	return def
}

func setup(t *testing.T) (*fakeDev, *Profile) {
	DataDir = t.TempDir()
	f := newFake()
	Exec = f.run
	t.Cleanup(func() {
		Exec = func(serial, script string, root bool) Res { return Sh(serial, script, Opt{}) }
	})
	return f, &Profile{Serial: "S1", Model: "Test", SDK: 34, Form: "handheld", Vendor: "unisoc", Installed: map[string]bool{}}
}

func findTweak(t *testing.T, id string) Tweak {
	for _, p := range LoadPacks() {
		for _, tw := range p.Tweaks {
			if tw.ID == id {
				return tw
			}
		}
	}
	t.Fatalf("tweak %s not found", id)
	return Tweak{}
}

func TestApplyRevertPreset(t *testing.T) {
	f, p := setup(t)
	f.kv["settings/global/window_animation_scale"] = "1.0"
	tw := findTweak(t, "anim.scale")
	es := ApplyTweak(p, tw, "0.5")
	if len(es) != 3 || es[0].Err != "" || !es[0].Verified || es[0].Prev != "1.0" || !es[0].PrevExists {
		t.Fatalf("entries: %+v", es)
	}
	if f.kv["settings/global/transition_animation_scale"] != "0.5" {
		t.Fatalf("not applied: %v", f.kv)
	}
	if n, msg := RevertTweak("S1", tw.Title); n != 3 {
		t.Fatalf("revert %d %s", n, msg)
	}
	if f.kv["settings/global/window_animation_scale"] != "1.0" {
		t.Fatalf("not restored: %v", f.kv)
	}
	if _, ok := f.kv["settings/global/transition_animation_scale"]; ok {
		t.Fatalf("key that did not exist before must be deleted on revert: %v", f.kv)
	}
}

func TestToggleStateAndUnset(t *testing.T) {
	f, p := setup(t)
	tw := findTweak(t, "phantom.limit")
	if st, _ := TweakState(p, tw); st != "off" {
		t.Fatalf("initial state %s", st)
	}
	ApplyTweak(p, tw, "on")
	if st, _ := TweakState(p, tw); st != "on" || f.kv["dc/activity_manager/max_phantom_processes"] != "2147483647" {
		t.Fatalf("after on: %s %v", st, f.kv)
	}
	ApplyTweak(p, tw, "off")
	if _, ok := f.kv["dc/activity_manager/max_phantom_processes"]; ok {
		t.Fatal("off should delete the device_config key")
	}
}

func TestRevertAllAndPkgOps(t *testing.T) {
	f, p := setup(t)
	f.pkgs["com.x"] = "installed"
	f.kv["bucket/com.x"] = "10"
	ApplyOp(p, "g", "Disable com.x", Op{Kind: "pkg", Pkg: "com.x"}, "disabled", false)
	ApplyOp(p, "g", "Bucket com.x", Op{Kind: "bucket", Pkg: "com.x"}, "restricted", false)
	ApplyOp(p, "g", "Appop", Op{Kind: "appops", Pkg: "com.x", Op: "WAKE_LOCK"}, "ignore", false)
	if f.pkgs["com.x"] != "disabled" || f.kv["bucket/com.x"] != "45" || f.kv["appops/com.x/WAKE_LOCK"] != "ignore" {
		t.Fatalf("not applied: %v %v", f.pkgs, f.kv)
	}
	done, failed, _ := RevertAll("S1")
	if done != 3 || failed != 0 {
		t.Fatalf("revert all: %d %d", done, failed)
	}
	if f.pkgs["com.x"] != "installed" || f.kv["bucket/com.x"] != "10" || f.kv["appops/com.x/WAKE_LOCK"] != "allow" {
		t.Fatalf("not restored: %v %v", f.pkgs, f.kv)
	}
	if n, _, _ := RevertAll("S1"); n != 0 {
		t.Fatal("second revert-all must be a no-op")
	}
}

func TestNotVerifiedWhenOverwritten(t *testing.T) {
	f, p := setup(t)
	old := Exec
	Exec = func(serial, script string, root bool) Res {
		r := old(serial, script, root)
		delete(f.kv, "dc/systemui/android.app.compact_heads_up_notification") // simulate flag sync resetting it
		return r
	}
	es := ApplyTweak(p, findTweak(t, "notif.compact"), "on")
	if es[0].Verified {
		t.Fatal("overwritten value must not count as verified")
	}
}

func TestRequiresAndClassify(t *testing.T) {
	out := "ro.product.model=ZTE Blade\nro.product.brand=ZTE\nro.product.manufacturer=ZTE\nro.board.platform=ums9230\nro.soc.model=T606\nro.build.version.sdk=34\nro.build.version.release=14\nuid=2000\nsu=\nshizuku_pkg=package:moe.shizuku.privileged.api\nshizuku_run=0\n@@features\nfeature:android.hardware.telephony\n@@pkgs\npackage:com.zte.zdm\npackage:com.android.settings\n@@dis\n"
	p := ParseProfile("S", out)
	if p.Vendor != "unisoc" || p.Form != "handheld" || p.SDK != 34 || p.Root || !p.ShizukuInstalled || p.ShizukuRunning || !p.HasBrand("zte") || !p.HasBrand("nubia") {
		t.Fatalf("profile: %+v", p)
	}
	tw, deb, _ := Catalog(p)
	hasDeb := false
	for _, d := range deb {
		hasDeb = hasDeb || d.Pkg == "com.zte.zdm"
	}
	if !hasDeb {
		t.Fatal("ZTE debloat entry for an installed package should be offered")
	}
	for _, c := range tw {
		if c.ID == "tv.framerate" && c.OK {
			t.Fatal("TV tweak must not apply to a handheld")
		}
		if c.ID == "phantom.limit" && !c.OK {
			t.Fatalf("phantom limit should apply on API 34: %s", c.Reason)
		}
	}
	tv := ParseProfile("T", "ro.product.model=Box\nro.board.platform=kalama\nro.build.version.sdk=30\n@@features\nfeature:android.software.leanback\n")
	if tv.Form != "tv" || tv.Vendor != "qualcomm" {
		t.Fatalf("tv: %+v", tv)
	}
	old := ParseProfile("O", "ro.build.version.sdk=29\n")
	if ok, _ := (Requires{MinSDK: 31}).Applicable(old, nil); ok {
		t.Fatal("API gate failed")
	}
}

func TestPacksValid(t *testing.T) {
	ids := map[string]bool{}
	for _, p := range LoadPacks() {
		for _, tw := range p.Tweaks {
			if ids[tw.ID] || tw.Title == "" || len(tw.Ops) == 0 || (tw.Risk != "safe" && tw.Risk != "caution" && tw.Risk != "risky") {
				t.Fatalf("bad tweak %+v", tw)
			}
			ids[tw.ID] = true
		}
		for _, d := range p.Debloat {
			if d.Pkg == "" || d.Risk == "" {
				t.Fatalf("bad debloat %+v", d)
			}
		}
	}
	if len(ids) < 15 {
		t.Fatalf("only %d tweaks", len(ids))
	}
}

func TestUIDump(t *testing.T) {
	x := `<?xml version='1.0'?><hierarchy rotation="0"><node class="android.widget.FrameLayout" text="" resource-id="" clickable="false" bounds="[0,0][1080,2400]"><node class="android.widget.TextView" text="Settings" resource-id="com.android.settings:id/title" clickable="false" bounds="[0,0][500,100]"/><node class="androidx.recyclerview.widget.RecyclerView" text="" clickable="false" bounds="[0,100][1080,2400]"><node class="android.widget.Button" text="Network &amp; Internet" resource-id="com.android.settings:id/settings_network" clickable="true" bounds="[0,100][1080,300]"/></node></node></hierarchy>`
	n, err := ParseUIDump(x)
	if err != nil {
		t.Fatal(err)
	}
	tree := strings.Join(n.RenderTree(true, false), "\n")
	for _, want := range []string{"[FrameLayout]", `├── [TextView] text="Settings" id="title" (clickable: false)`, `└── [Button] text="Network & Internet" id="settings_network" (clickable: true)`} {
		if !strings.Contains(tree, want) {
			t.Fatalf("missing %q in:\n%s", want, tree)
		}
	}
	tg := n.Targets()
	if len(tg) != 2 {
		t.Fatalf("targets %d", len(tg))
	}
	if cx, cy := tg[1].Center(); cx != 540 || cy != 200 {
		t.Fatalf("center %d,%d", cx, cy)
	}
	if !strings.Contains(n.RenderJSON(true), `"clickable": true`) || !strings.Contains(n.RenderMarkdown(true), "- [FrameLayout]") {
		t.Fatal("json/markdown render")
	}
}

func TestDiffAndTop(t *testing.T) {
	a := &SettingsSnap{Data: map[string]string{"settings/global/a": "1", "settings/global/b": "2", "settings/secure/c": "x"}}
	b := &SettingsSnap{Data: map[string]string{"settings/global/a": "1", "settings/global/b": "3", "settings/global/d": "new"}}
	d := DiffSnaps(a, b)
	if len(d) != 3 || d[0].Op != "~" || d[1].Op != "+" || d[2].Op != "-" {
		t.Fatalf("diff: %+v", d)
	}
	top := "Tasks: 5 total\n  PID USER         PR  NI VIRT  RES  SHR S[%CPU] %MEM     TIME+ ARGS\n 1234 u0_a123      10 -10 4.5G 210M  90M S  12.3   2.1   0:01.23 com.pkg.app\n  567 system       20   0 2.1G 150M  80M R   3.0   1.5   1:00.00 system_server\n"
	procs := ParseTop(top)
	if len(procs) != 2 || procs[0].Name != "com.pkg.app" || procs[0].CPU != 12.3 || procs[0].RSS != 210*1024 {
		t.Fatalf("top: %+v", procs)
	}
	pss := ParsePSS("Total PSS by process:\n    123,456K: com.pkg.app (pid 1234 / activity)\n     90,000K: system_server (pid 567)\n\nTotal PSS by OOM adjustment:\n")
	if pss[1234] != 123456 || pss[567] != 90000 {
		t.Fatalf("pss: %v", pss)
	}
	g := GlobPlan("x", []string{"a*"})
	_ = g
}

// Nothing OS is detected from its own build prop, even when the product brand strings name another company.
// (Synthetic values: only the prop name is the real one.)
func TestNothingOSDetect(t *testing.T) {
	out := "ro.product.brand=example\nro.product.manufacturer=EXAMPLE\nro.build.version.sdk=36\nro.build.version.release=16\nro.build.nothing.version=Nothing OS (4)\n@@features\nfeature:com.nothing.feature\n"
	p := ParseProfile("N", out)
	if !p.HasBrand("nothing") || p.SDK != 36 {
		t.Fatalf("nothing os: %+v", p)
	}
	if q := ParseProfile("X", "ro.product.brand=Pixel\nro.build.version.sdk=36\n"); q.HasBrand("nothing") {
		t.Fatalf("false positive: %v", q.Brands)
	}
}

// Brand families: first-party targets are Pixel (google), Samsung, Nothing; then vivo/oppo/xiaomi/zte families.
func TestBrandFamilies(t *testing.T) {
	for _, c := range []struct{ brand, mfr, want string }{
		{"Redmi", "Xiaomi", "xiaomi"}, {"POCO", "Xiaomi", "xiaomi"}, {"iQOO", "vivo", "vivo"},
		{"realme", "realme", "oppo"}, {"OnePlus", "OnePlus", "oppo"}, {"google", "Google", "google"},
		{"Nothing", "Nothing", "nothing"}, {"samsung", "samsung", "samsung"},
	} {
		p := ParseProfile("S", "ro.product.brand="+c.brand+"\nro.product.manufacturer="+c.mfr+"\nro.build.version.sdk=34\n")
		if !p.HasBrand(c.want) {
			t.Errorf("%s/%s: want brand %q, got %v", c.brand, c.mfr, c.want, p.Brands)
		}
	}
}

// Batched reads must give exactly what per-op reads give (incl. presets, toggles and package state).
func TestTweakStatesBatch(t *testing.T) {
	f, p := setup(t)
	f.kv["settings/global/window_animation_scale"] = "1.0"
	f.kv["dc/activity_manager/max_phantom_processes"] = "2147483647"
	f.kv["settings/global/settings_enable_monitor_phantom_procs"] = "false" // phantom.limit has two ops; both must be On
	var ts []Tweak
	for _, id := range []string{"anim.scale", "phantom.limit"} {
		ts = append(ts, findTweak(t, id))
	}
	many := TweakStates(p, ts)
	for i, tw := range ts {
		st, cur := TweakState(p, tw)
		if many[i][0] != st || many[i][1] != cur {
			t.Fatalf("%s: batch %v vs single %s|%s", tw.ID, many[i], st, cur)
		}
	}
	if many[1][0] != "on" {
		t.Fatalf("phantom.limit should read on: %v", many)
	}
}

// Journal read-modify-write is guarded against other processes by a lock file.
func TestJournalLock(t *testing.T) {
	old := DataDir
	DataDir = t.TempDir()
	defer func() { DataDir = old }()
	lock := journalPath() + ".lock"

	// 1. concurrent appends: nothing lost, no lock file left behind
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); appendJournal(JEntry{ID: int64(i + 1), Serial: "S", Title: "t"}) }(i)
	}
	wg.Wait()
	if n := len(LoadJournal()); n != 20 {
		t.Fatalf("lost entries: %d/20", n)
	}
	if _, err := os.Stat(lock); err == nil {
		t.Fatal("lock file left behind")
	}

	// 2. a lock held by another process makes us wait until it is released
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan time.Duration, 1)
	start := time.Now()
	go func() { appendJournal(JEntry{ID: 99, Serial: "S"}); done <- time.Since(start) }()
	time.Sleep(150 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("append did not wait for the held lock")
	default:
	}
	_ = os.Remove(lock)
	if d := <-done; d < 140*time.Millisecond {
		t.Fatalf("append returned too early: %v", d)
	}

	// 3. a stale lock (crashed process) is taken over, not waited on forever
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	old30 := time.Now().Add(-30 * time.Second)
	_ = os.Chtimes(lock, old30, old30)
	t0 := time.Now()
	appendJournal(JEntry{ID: 100, Serial: "S"})
	if time.Since(t0) > 2*time.Second {
		t.Fatal("stale lock not taken over")
	}
	if n := len(LoadJournal()); n != 22 {
		t.Fatalf("entries: %d want 22", n)
	}
}

// Ctrl-Z semantics: the newest still-live group of a device is undone as one step; reverted/other-device entries are skipped.
func TestLastGroupUndo(t *testing.T) {
	f, p := setup(t)
	f.kv["settings/global/window_animation_scale"] = "1.0"
	tw := findTweak(t, "anim.scale")
	ApplyTweak(p, tw, "0.5")                                                                                            // group A (3 ops)
	ApplyOp(p, "g-single", "Auto-rotate", Op{Kind: "setting", NS: "system", Key: "accelerometer_rotation"}, "1", false) // group B
	appendJournal(JEntry{ID: newID(), Serial: "OTHER", Group: "zzz", Title: "other device"})

	last := LastGroup(p.Serial)
	if len(last) != 1 || last[0].Title != "Auto-rotate" {
		t.Fatalf("last group should be the single change, got %+v", last)
	}
	if d, bad, _ := RevertEntries(last); d != 1 || bad != 0 {
		t.Fatalf("revert: %d/%d", d, bad)
	}
	if f.kv["settings/system/accelerometer_rotation"] != "" {
		t.Fatalf("not undone: %q", f.kv["settings/system/accelerometer_rotation"])
	}
	last = LastGroup(p.Serial)
	if len(last) < 2 || last[0].Group != last[1].Group {
		t.Fatalf("next group should be the whole tweak (several ops): %+v", last)
	}
	RevertEntries(last)
	if f.kv["settings/global/window_animation_scale"] != "1.0" {
		t.Fatalf("tweak not restored: %q", f.kv["settings/global/window_animation_scale"])
	}
	if g := LastGroup(p.Serial); g != nil {
		t.Fatalf("nothing should be left, got %+v", g)
	}
}

func TestUsageFrecency(t *testing.T) {
	old := DataDir
	DataDir = t.TempDir()
	defer func() { DataDir = old }()
	for i := 0; i < 3; i++ {
		BumpUsage("Tools/Screenshot")
	}
	BumpUsage("System/Start Shizuku (adb)")
	u := UsageCounts()
	if u["Tools/Screenshot"] != 3 || u["System/Start Shizuku (adb)"] != 1 {
		t.Fatalf("counts: %v", u)
	}
	for i := 0; i < 600; i++ { // pushes the total past 500 → counts are halved, zeros dropped
		BumpUsage("Tools/Screenshot")
	}
	u = UsageCounts()
	if _, ok := u["System/Start Shizuku (adb)"]; ok {
		t.Fatalf("rarely used action should have faded out: %v", u)
	}
	if n := u["Tools/Screenshot"]; n < 1 || n > 500 {
		t.Fatalf("decay failed: %d", n)
	}
}

// Catalog() re-asks the same getprop questions on every Tweaks reload; they must hit the cache.
func TestKeyExistsCached(t *testing.T) {
	f, p := setup(t)
	f.kv["prop/ro.test.cached.key"] = "1"
	calls := 0
	inner := Exec
	Exec = func(serial, script string, root bool) Res {
		if strings.Contains(script, "getprop") {
			calls++
		}
		return inner(serial, script, root)
	}
	defer func() { Exec = inner }()
	for i := 0; i < 3; i++ {
		if !KeyExists(p.Serial, p, "ro.test.cached.key") {
			t.Fatal("key should exist")
		}
	}
	if calls != 1 {
		t.Fatalf("getprop ran %d times, want 1", calls)
	}
}
