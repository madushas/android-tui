package core

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// rootAVD (a third-party project by "newbit") patches an AVD's ramdisk.img with Magisk. Using it from android-tui is an
// OPTIONAL power-user feature: android-tui neither bundles nor modifies it. You point android-tui at your own copy of
// rootAVD.sh and it
//   - works out which ramdisk.img the AVD boots from,
//   - keeps its OWN one-time backup of the stock ramdisk next to it, so undoing never depends on rootAVD,
//   - runs the script on the real terminal (rootAVD is interactive), and
//   - checks afterwards that the ramdisk really changed.
//
// Caution: the ramdisk belongs to the SYSTEM IMAGE, so every AVD created from the same image is affected.

const ramdiskBackupSuffix = ".android-tui.bak"

// RootAVDScript returns the configured rootAVD script ("" = not configured): ANDROID_TUI_ROOTAVD, then ROOTAVD in the config.
func RootAVDScript() string {
	if p := os.Getenv("ANDROID_TUI_ROOTAVD"); p != "" {
		return p
	}
	return Cfg.RootAVD
}

// RootAVDPlan describes one rootAVD run (or restore) for one AVD.
type RootAVDPlan struct {
	AVD     string
	SDKRoot string
	Ramdisk string   // absolute path of the system image's ramdisk.img
	Rel     string   // the same path relative to SDKRoot (what rootAVD expects as its argument)
	Backup  string   // android-tui's own copy of the stock ramdisk
	Shared  []string // OTHER AVDs that boot from the same image (they are affected too)
	Script  string   // set by UseScript
}

// PlanRootAVD resolves the ramdisk the AVD boots from. It needs no script, so it also serves status and restore.
func PlanRootAVD(avd string) (*RootAVDPlan, error) {
	root := SDK
	if root == "" {
		return nil, errors.New("android SDK not found: set SDK_ROOT in the config")
	}
	sys := strings.Trim(IniGet(CfgPath(avd), "image.sysdir.1"), "/")
	if sys == "" {
		return nil, fmt.Errorf("%s has no system image configured", avd)
	}
	rd := filepath.Join(root, sys, "ramdisk.img")
	rel, err := filepath.Rel(root, rd)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, errors.New("the AVD's system image path points outside the SDK")
	}
	if st, err := os.Stat(rd); err != nil || !st.Mode().IsRegular() {
		return nil, fmt.Errorf("ramdisk not found: %s", rd)
	}
	p := &RootAVDPlan{AVD: avd, SDKRoot: root, Ramdisk: rd, Rel: rel, Backup: rd + ramdiskBackupSuffix}
	for _, n := range ListNames() {
		if n != avd && strings.Trim(IniGet(CfgPath(n), "image.sysdir.1"), "/") == sys {
			p.Shared = append(p.Shared, n)
		}
	}
	return p, nil
}

// UseScript checks the rootAVD script and records its absolute path.
func (p *RootAVDPlan) UseScript(path string) error {
	if strings.HasPrefix(path, "~/") {
		path = filepath.Join(Home, path[2:])
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("rootAVD script not found: %s", abs)
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("%s is not a file", abs)
	}
	p.Script = abs
	return nil
}

// Prepare takes the one-time backup of the stock ramdisk (an existing backup is never overwritten) and returns the
// ramdisk's current hash for Finish. note is non-empty when rootAVD's own backup was used as the stock copy.
func (p *RootAVDPlan) Prepare() (before, note string, err error) {
	f, err := os.OpenFile(p.Ramdisk, os.O_WRONLY, 0)
	if err != nil {
		return "", "", fmt.Errorf("%s is not writable: %w", p.Ramdisk, err)
	}
	_ = f.Close()
	if _, err := os.Stat(p.Backup); err != nil {
		src := p.Ramdisk
		// A leftover rootAVD backup means the ramdisk may already be patched; rootAVD's copy is the stock one.
		if _, e := os.Stat(p.Ramdisk + ".backup"); e == nil {
			src = p.Ramdisk + ".backup"
			note = "found rootAVD's own backup (ramdisk.img.backup) and used it as the stock copy"
		}
		if err := copyFileAtomic(src, p.Backup); err != nil {
			return "", "", fmt.Errorf("cannot back up the stock ramdisk: %w", err)
		}
	}
	before, err = fileSHA256(p.Ramdisk)
	return before, note, err
}

// Command builds the rootAVD invocation. It runs from the SDK root with ANDROID_HOME set and the ramdisk given relative
// to it, so it works whichever way the script resolves its argument. pause keeps the output on screen until Enter.
func (p *RootAVDPlan) Command(extra []string, pause bool) *exec.Cmd {
	argv := append([]string{p.Script, p.Rel}, extra...)
	if st, err := os.Stat(p.Script); err == nil && st.Mode()&0o111 == 0 {
		argv = append([]string{"bash"}, argv...) // not executable: rootAVD is a bash script
	}
	var c *exec.Cmd
	if pause {
		c = exec.Command("sh", append([]string{"-c", `"$@"; rc=$?; printf '\nPress Enter to continue… '; read _x; exit $rc`, "sh"}, argv...)...)
	} else {
		c = exec.Command(argv[0], argv[1:]...)
	}
	c.Dir = p.SDKRoot
	c.Env = append(os.Environ(), "ANDROID_HOME="+p.SDKRoot, "ANDROID_SDK_ROOT="+p.SDKRoot)
	return c
}

// Finish judges the run by what happened to the ramdisk, not by the script's say-so alone.
func (p *RootAVDPlan) Finish(before string, runErr error) (bool, string) {
	after, err := fileSHA256(p.Ramdisk)
	if err != nil {
		return false, "cannot read the ramdisk afterwards: " + err.Error()
	}
	changed := after != before
	switch {
	case runErr != nil && !changed:
		return false, "rootAVD failed (" + runErr.Error() + ") — the ramdisk is unchanged"
	case runErr != nil:
		return false, "rootAVD reported an error but the ramdisk changed — use “Restore stock ramdisk” if the AVD misbehaves"
	case !changed:
		return false, "rootAVD finished but ramdisk.img did not change — nothing was patched"
	}
	return true, "ramdisk patched — cold-boot the AVD to use root (“Restore stock ramdisk” undoes it)"
}

// Restore puts the stock ramdisk back from android-tui's own backup and verifies the result. The AVDs that use this
// image must be stopped first (the caller checks).
func (p *RootAVDPlan) Restore() (bool, string) {
	if _, err := os.Stat(p.Backup); err != nil {
		return false, "there is no android-tui backup of the stock ramdisk for this image"
	}
	if err := copyFileAtomic(p.Backup, p.Ramdisk); err != nil {
		return false, "restore failed: " + err.Error()
	}
	a, e1 := fileSHA256(p.Ramdisk)
	b, e2 := fileSHA256(p.Backup)
	if e1 != nil || e2 != nil || a != b {
		return false, "the restored ramdisk does not match the backup"
	}
	return true, "stock ramdisk restored — cold-boot the AVDs that use " + filepath.Dir(p.Rel)
}

// Status is "no backup yet" (never patched through android-tui), "stock" or "patched".
func (p *RootAVDPlan) Status() string {
	bs, err := os.Stat(p.Backup)
	if err != nil {
		return "no backup yet"
	}
	rs, err := os.Stat(p.Ramdisk)
	if err != nil {
		return "unknown"
	}
	if rs.Size() != bs.Size() {
		return "patched"
	}
	a, e1 := fileSHA256(p.Ramdisk)
	b, e2 := fileSHA256(p.Backup)
	if e1 == nil && e2 == nil && a == b {
		return "stock"
	}
	return "patched"
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// copyFileAtomic copies via a temporary file and renames it into place, so a crash never leaves a half-written ramdisk.
func copyFileAtomic(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, st.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
