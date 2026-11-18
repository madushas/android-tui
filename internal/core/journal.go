package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

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
	MkdirPrivate(DataDir)
	b, _ := json.MarshalIndent(es, "", " ")
	_ = WritePrivate(journalPath(), b)
}

func LoadJournal() []JEntry { jMu.Lock(); defer jMu.Unlock(); return loadJournalLocked() }

// lockJournal serialises read-modify-write cycles across processes (TUI + CLI) with an O_EXCL lock file.
// A lock older than 10s is treated as stale (a crashed process) and taken over. If the lock cannot be
// obtained within ~4s we proceed unlocked rather than lose the record of a change already made on the device.
func lockJournal() func() {
	MkdirPrivate(DataDir)
	lp := journalPath() + ".lock"
	for i := 0; i < 200; i++ {
		if f, err := os.OpenFile(lp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600); err == nil {
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
