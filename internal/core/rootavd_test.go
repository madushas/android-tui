package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const stockBytes = "STOCK-RAMDISK-BYTES"

// rootavdEnv builds a throw-away SDK with one system image and the given AVDs (name → image dir inside the SDK).
func rootavdEnv(t *testing.T, avds map[string]string) (sdk string) {
	t.Helper()
	sdk, home := t.TempDir(), t.TempDir()
	oldSDK, oldHome := SDK, AVDHome
	SDK, AVDHome = sdk, home
	t.Cleanup(func() { SDK, AVDHome = oldSDK, oldHome })
	for name, img := range avds {
		dir := filepath.Join(home, name+".avd")
		must(t, os.MkdirAll(dir, 0o755))
		must(t, os.WriteFile(filepath.Join(home, name+".ini"), []byte("path="+dir+"\n"), 0o644))
		must(t, os.WriteFile(filepath.Join(dir, "config.ini"), []byte("image.sysdir.1="+img+"/\n"), 0o644))
		if rd := filepath.Join(sdk, img, "ramdisk.img"); !exists(rd) {
			must(t, os.MkdirAll(filepath.Dir(rd), 0o755))
			must(t, os.WriteFile(rd, []byte(stockBytes), 0o644))
		}
	}
	return sdk
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// fakeScript stands in for rootAVD.sh: it records how it was invoked and "patches" the ramdisk it is given.
func fakeScript(t *testing.T, body string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "rootAVD.sh")
	must(t, os.WriteFile(p, []byte("#!/bin/bash\n"+body), mode))
	return p
}

func TestPlanRootAVD(t *testing.T) {
	img := "system-images/android-36/google_apis/x86_64"
	sdk := rootavdEnv(t, map[string]string{
		"A": img, "B": img, "C": "system-images/android-35/google_apis/x86_64",
	})
	p, err := PlanRootAVD("A")
	must(t, err)
	if p.Ramdisk != filepath.Join(sdk, img, "ramdisk.img") || p.Rel != filepath.Join(img, "ramdisk.img") {
		t.Fatalf("paths: %+v", p)
	}
	if len(p.Shared) != 1 || p.Shared[0] != "B" {
		t.Fatalf("only B shares the image, got %v", p.Shared)
	}
	if p.Backup != p.Ramdisk+".android-tui.bak" {
		t.Fatalf("backup path: %s", p.Backup)
	}
	if _, err := PlanRootAVD("nosuch"); err == nil {
		t.Fatal("an AVD without a configured image must be an error")
	}
	// a hostile or broken config must not be able to point us outside the SDK
	rootavdEnv(t, map[string]string{"X": "../../etc"})
	if _, err := PlanRootAVD("X"); err == nil {
		t.Fatal("an image path escaping the SDK must be rejected")
	}
	// image listed in the AVD config but not installed
	sdk = rootavdEnv(t, map[string]string{"Y": "system-images/android-30/default/x86"})
	must(t, os.Remove(filepath.Join(sdk, "system-images/android-30/default/x86/ramdisk.img")))
	if _, err := PlanRootAVD("Y"); err == nil || !strings.Contains(err.Error(), "ramdisk not found") {
		t.Fatalf("missing ramdisk: %v", err)
	}
}

func TestRootAVDFullRun(t *testing.T) {
	img := "system-images/android-36/google_apis/x86_64"
	sdk := rootavdEnv(t, map[string]string{"A": img})
	marker := filepath.Join(t.TempDir(), "invocation.txt")
	script := fakeScript(t, `
printf 'cwd=%s\nandroid_home=%s\nsdk_root=%s\nargs=%s\n' "$PWD" "$ANDROID_HOME" "$ANDROID_SDK_ROOT" "$*" > `+marker+`
printf 'PATCHED' >> "$1"      # relative to the cwd: the SDK root
`, 0o755)
	p, err := PlanRootAVD("A")
	must(t, err)
	must(t, p.UseScript(script))

	before, note, err := p.Prepare()
	must(t, err)
	if note != "" || p.Status() != "stock" {
		t.Fatalf("note=%q status=%q", note, p.Status())
	}
	if b, _ := os.ReadFile(p.Backup); string(b) != stockBytes {
		t.Fatalf("backup is %q, want the stock bytes", b)
	}
	must(t, p.Command([]string{"DEBUG"}, false).Run())
	if ok, msg := p.Finish(before, nil); !ok {
		t.Fatal(msg)
	}
	rec, _ := os.ReadFile(marker)
	for _, want := range []string{"cwd=" + sdk, "android_home=" + sdk, "sdk_root=" + sdk, "args=" + p.Rel + " DEBUG"} {
		if !strings.Contains(string(rec), want) {
			t.Fatalf("the script was invoked wrongly; missing %q in\n%s", want, rec)
		}
	}
	if b, _ := os.ReadFile(p.Ramdisk); string(b) != stockBytes+"PATCHED" {
		t.Fatalf("ramdisk now %q", b)
	}
	if p.Status() != "patched" {
		t.Fatalf("status %q", p.Status())
	}

	// a second run must keep the ORIGINAL stock backup, not copy the patched file over it
	before2, _, err := p.Prepare()
	must(t, err)
	if before2 == before {
		t.Fatal("the ramdisk hash should differ after patching")
	}
	if b, _ := os.ReadFile(p.Backup); string(b) != stockBytes {
		t.Fatalf("the stock backup was overwritten: %q", b)
	}

	ok, msg := p.Restore()
	if !ok {
		t.Fatal(msg)
	}
	if b, _ := os.ReadFile(p.Ramdisk); string(b) != stockBytes || p.Status() != "stock" {
		t.Fatalf("restore left %q (status %q)", b, p.Status())
	}
	if exists(p.Ramdisk+".tmp") || exists(p.Backup+".tmp") {
		t.Fatal("temporary files left behind")
	}
}

func TestRootAVDFinishJudgesTheRamdisk(t *testing.T) {
	rootavdEnv(t, map[string]string{"A": "system-images/android-36/google_apis/x86_64"})
	p, _ := PlanRootAVD("A")
	before, _ := fileSHA256(p.Ramdisk)
	if ok, msg := p.Finish(before, nil); ok || !strings.Contains(msg, "did not change") {
		t.Fatalf("a clean exit with an unchanged ramdisk is a failure: %v %q", ok, msg)
	}
	if ok, msg := p.Finish(before, os.ErrInvalid); ok || !strings.Contains(msg, "unchanged") {
		t.Fatalf("failure with no change: %v %q", ok, msg)
	}
	must(t, os.WriteFile(p.Ramdisk, []byte("half-patched"), 0o644))
	if ok, msg := p.Finish(before, os.ErrInvalid); ok || !strings.Contains(msg, "Restore stock ramdisk") {
		t.Fatalf("failure after a change must point to the restore: %v %q", ok, msg)
	}
}

func TestRootAVDNonExecutableScriptRunsViaBash(t *testing.T) {
	rootavdEnv(t, map[string]string{"A": "system-images/android-36/google_apis/x86_64"})
	script := fakeScript(t, `printf 'X' >> "$1"`+"\n", 0o644) // no exec bit
	p, _ := PlanRootAVD("A")
	must(t, p.UseScript(script))
	before, _, err := p.Prepare()
	must(t, err)
	must(t, p.Command(nil, false).Run())
	if ok, msg := p.Finish(before, nil); !ok {
		t.Fatal(msg)
	}
}

func TestRootAVDPauseWrapperPropagatesFailure(t *testing.T) {
	rootavdEnv(t, map[string]string{"A": "system-images/android-36/google_apis/x86_64"})
	p, _ := PlanRootAVD("A")
	must(t, p.UseScript(fakeScript(t, "exit 7\n", 0o755)))
	c := p.Command(nil, true)
	c.Stdin = strings.NewReader("\n") // the "Press Enter" prompt
	err := c.Run()
	if err == nil || !strings.Contains(err.Error(), "exit status 7") {
		t.Fatalf("the script's exit status must survive the pause wrapper, got %v", err)
	}
}

func TestRootAVDPrepareUsesRootAVDsOwnBackup(t *testing.T) {
	rootavdEnv(t, map[string]string{"A": "system-images/android-36/google_apis/x86_64"})
	p, _ := PlanRootAVD("A")
	// the ramdisk is already patched (e.g. by an earlier manual rootAVD run); rootAVD left its stock copy beside it
	must(t, os.WriteFile(p.Ramdisk, []byte("ALREADY-PATCHED"), 0o644))
	must(t, os.WriteFile(p.Ramdisk+".backup", []byte(stockBytes), 0o644))
	_, note, err := p.Prepare()
	must(t, err)
	if !strings.Contains(note, "rootAVD's own backup") {
		t.Fatalf("note %q", note)
	}
	if b, _ := os.ReadFile(p.Backup); string(b) != stockBytes {
		t.Fatalf("our backup must be the stock copy, got %q", b)
	}
}

func TestRootAVDRestoreWithoutBackupAndScriptValidation(t *testing.T) {
	rootavdEnv(t, map[string]string{"A": "system-images/android-36/google_apis/x86_64"})
	p, _ := PlanRootAVD("A")
	if ok, _ := p.Restore(); ok {
		t.Fatal("restore without a backup must fail rather than guess")
	}
	if p.Status() != "no backup yet" {
		t.Fatalf("status %q", p.Status())
	}
	if err := p.UseScript(filepath.Join(t.TempDir(), "missing.sh")); err == nil {
		t.Fatal("a missing script must be an error")
	}
	if err := p.UseScript(t.TempDir()); err == nil {
		t.Fatal("a directory is not a script")
	}
}

func TestRootAVDScriptPrecedence(t *testing.T) {
	old := Cfg.RootAVD
	defer func() { Cfg.RootAVD = old }()
	Cfg.RootAVD = "/from/config.sh"
	t.Setenv("ANDROID_TUI_ROOTAVD", "")
	if RootAVDScript() != "/from/config.sh" {
		t.Fatal("the config value is used when the environment is empty")
	}
	t.Setenv("ANDROID_TUI_ROOTAVD", "/from/env.sh")
	if RootAVDScript() != "/from/env.sh" {
		t.Fatal("the environment overrides the config")
	}
}
