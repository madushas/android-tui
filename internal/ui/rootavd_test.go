package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/madushas/android-tui/internal/core"
)

// rootavdModel returns a Model with one emulator "A" (plus "B" on the same image) inside a throw-away SDK.
func rootavdModel(t *testing.T, state string) (*Model, *core.RootAVDPlan) {
	t.Helper()
	sdk, home := t.TempDir(), t.TempDir()
	oldSDK, oldHome, oldCfg := core.SDK, core.AVDHome, core.Cfg.RootAVD
	core.SDK, core.AVDHome = sdk, home
	t.Cleanup(func() { core.SDK, core.AVDHome, core.Cfg.RootAVD = oldSDK, oldHome, oldCfg })
	t.Setenv("ANDROID_TUI_ROOTAVD", "")
	img := "system-images/android-36/google_apis/x86_64"
	for _, n := range []string{"A", "B"} {
		dir := filepath.Join(home, n+".avd")
		for p, c := range map[string]string{
			filepath.Join(home, n+".ini"):    "path=" + dir + "\n",
			filepath.Join(dir, "config.ini"): "image.sysdir.1=" + img + "/\n",
		} {
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	rd := filepath.Join(sdk, img, "ramdisk.img")
	if err := os.MkdirAll(filepath.Dir(rd), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rd, []byte("stock"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	m.w, m.h = 120, 40
	e := core.Emu{Name: "A", State: state}
	if state == "running" {
		e.Serial = "emulator-5554"
	}
	m.sc.Emus = []core.Emu{e, {Name: "B", State: "stopped"}}
	m.focus = fEmu
	plan, err := core.PlanRootAVD("A")
	if err != nil {
		t.Fatal(err)
	}
	return m, plan
}

func TestRootAVDGuards(t *testing.T) {
	// not configured: explain how to set it up instead of failing
	m, _ := rootavdModel(t, "running")
	m.rootAVD()
	if len(m.modals) != 1 {
		t.Fatalf("an unconfigured rootAVD must show the setup help, got %d modals", len(m.modals))
	}
	v, ok := m.modals[0].(*viewerModal)
	if !ok || !strings.Contains(v.title, "rootAVD") {
		t.Fatalf("expected the help viewer, got %T", m.modals[0])
	}

	// configured but the emulator is stopped: refuse, no dialog
	m, _ = rootavdModel(t, "stopped")
	script := filepath.Join(t.TempDir(), "rootAVD.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	core.Cfg.RootAVD = script
	m.rootAVD()
	if len(m.modals) != 0 {
		t.Fatal("a stopped emulator must be refused before any dialog")
	}

	// configured and running: a TYPED confirmation that names the other AVD sharing the image
	m, _ = rootavdModel(t, "running")
	core.Cfg.RootAVD = script
	m.rootAVD()
	if len(m.modals) != 1 {
		t.Fatalf("want one confirmation, got %d modals", len(m.modals))
	}
	tc, ok := m.modals[0].(*typedConfirmModal)
	if !ok {
		t.Fatalf("rewriting a shared system image must need a typed confirmation, got %T", m.modals[0])
	}
	if tc.word != "root" || !strings.Contains(tc.msg, "Also affected") || !strings.Contains(tc.msg, "B") {
		t.Fatalf("confirmation text/word wrong: %q / %q", tc.word, tc.msg)
	}
}

func TestRestoreRamdiskGuards(t *testing.T) {
	m, plan := rootavdModel(t, "stopped")
	m.restoreRamdisk()
	if len(m.modals) != 0 {
		t.Fatal("with no backup there is nothing to restore")
	}
	// make it look patched, as after a rootAVD run
	if _, _, err := plan.Prepare(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.Ramdisk, []byte("patched!"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.restoreRamdisk()
	if len(m.modals) != 1 {
		t.Fatalf("a patched image with every AVD stopped must ask to restore, got %d modals", len(m.modals))
	}
	if _, ok := m.modals[0].(*confirmModal); !ok {
		t.Fatalf("got %T", m.modals[0])
	}

	// the same image used by a RUNNING AVD: refuse
	m, plan = rootavdModel(t, "stopped")
	if _, _, err := plan.Prepare(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.Ramdisk, []byte("patched!"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.sc.Emus[1].State = "running" // B shares the image with A
	m.restoreRamdisk()
	if len(m.modals) != 0 {
		t.Fatal("restoring under a running AVD that shares the image must be refused")
	}
}
