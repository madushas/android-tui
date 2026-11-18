package core

import (
	"os"
	"path/filepath"
	"testing"
)

func modeOf(t *testing.T, p string) os.FileMode {
	t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return st.Mode().Perm()
}

// State files and device-derived output must not be readable by other users on a shared machine.
func TestWritePrivateMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "state.json")
	if err := WritePrivate(p, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if m := modeOf(t, p); m != 0o600 {
		t.Fatalf("file mode %o, want 600", m)
	}
	if m := modeOf(t, filepath.Dir(p)); m != 0o700 {
		t.Fatalf("directory mode %o, want 700", m)
	}
	if exists(p + ".tmp") {
		t.Fatal("temporary file left behind")
	}
}

// os.WriteFile keeps the mode of an existing file; WritePrivate must still tighten it.
func TestWritePrivateTightensExistingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "old.json")
	if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WritePrivate(p, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if m := modeOf(t, p); m != 0o600 {
		t.Fatalf("an existing 644 file must become 600, got %o", m)
	}
	if b, _ := os.ReadFile(p); string(b) != "new" {
		t.Fatalf("content %q", b)
	}
}

// Directories left by older versions are tightened on start; missing ones are not created by the check.
func TestHardenStateDirs(t *testing.T) {
	oldD, oldC, oldF := DataDir, ConfDir, CacheDir
	defer func() { DataDir, ConfDir, CacheDir = oldD, oldC, oldF }()
	base := t.TempDir()
	DataDir = filepath.Join(base, "data")
	ConfDir = filepath.Join(base, "conf")
	CacheDir = filepath.Join(base, "missing")
	for _, d := range []string{DataDir, ConfDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	hardenStateDirs()
	for _, d := range []string{DataDir, ConfDir} {
		if m := modeOf(t, d); m != 0o700 {
			t.Fatalf("%s: mode %o, want 700", d, m)
		}
	}
	if exists(CacheDir) {
		t.Fatal("hardening must not create directories")
	}
}

// The journal is the most sensitive state (device values before and after each change): it must be written privately.
func TestJournalFileIsPrivate(t *testing.T) {
	old := DataDir
	DataDir = t.TempDir()
	defer func() { DataDir = old }()
	appendJournal(JEntry{ID: 1, Serial: "S", Title: "t"})
	if m := modeOf(t, journalPath()); m != 0o600 {
		t.Fatalf("journal mode %o, want 600", m)
	}
}
