package core

import (
	"os"
	"path/filepath"
)

// Private files: android-tui's state (journal, snapshots, usage, saved connections, shell history, config) and anything
// it writes from device data (screenshots, dumps, logcat, app lists) is owner-readable only. Directories are 0700.
// os.WriteFile does not change the mode of a file that already exists, so every helper chmods explicitly.
const (
	privFileMode = 0o600
	privDirMode  = 0o700
)

// WritePrivate atomically replaces path with data, creating its directory privately. The temporary file is created
// 0600 from the start, so the content is never readable by others even for an instant.
func WritePrivate(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, privDirMode); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, privFileMode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, privFileMode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// MkdirPrivate creates p (and parents) as 0700 and tightens it if it already exists with wider permissions.
func MkdirPrivate(p string) {
	_ = os.MkdirAll(p, privDirMode)
	_ = os.Chmod(p, privDirMode)
}

// hardenStateDirs tightens the directories android-tui owns. Called from LoadConfig, so an upgrade fixes directories
// created by older versions. Missing directories are left alone (they are created privately when first needed).
func hardenStateDirs() {
	for _, d := range []string{DataDir, ConfDir, CacheDir} {
		if st, err := os.Stat(d); err == nil && st.IsDir() && st.Mode().Perm() != privDirMode {
			_ = os.Chmod(d, privDirMode)
		}
	}
}
