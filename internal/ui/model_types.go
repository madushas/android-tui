package ui

import (
	"os"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

type (
	tickMsg time.Time
	scanMsg struct {
		sc     core.Scan
		host   core.Host
		gen    int
		serial string
		sample *core.Sample
	}
	jobDone struct {
		quiet bool
		k     cont
	}
	hint struct{ k, l string }
)

type focusT int

const (
	fEmu focusT = iota
	fConn
	fSnap
	fMain
)

// modeT groups up to 5 related tabs so the tab strip stays small; m.order[g] is the flattened list.
type modeT struct {
	name string
	ids  []string
}

type groupT int

const (
	gEmu groupT = iota
	gConn
)

type snapEnt struct{ name, size string }

type rect struct{ x, y, w, h int }

func (r rect) in(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

// target is whatever the user has selected: an emulator (AVD) or an adb connection.
type target struct {
	emu  *core.Emu
	conn *core.Conn
}

func (t target) valid() bool { return t.emu != nil || t.conn != nil }

func (t target) isEmu() bool { return t.emu != nil }

func (t target) serial() string {
	switch {
	case t.emu != nil:
		return t.emu.Serial
	case t.conn != nil:
		return t.conn.Serial
	}
	return ""
}

func (t target) online() bool {
	return (t.emu != nil && t.emu.Online()) || (t.conn != nil && t.conn.Online())
}

func (t target) name() string {
	switch {
	case t.emu != nil:
		return t.emu.Name
	case t.conn != nil:
		return t.conn.Name()
	}
	return ""
}

func (t target) state() string {
	switch {
	case t.emu != nil:
		return t.emu.State
	case t.conn != nil:
		return t.conn.State
	}
	return ""
}

func (t target) console() bool { return t.online() && strings.HasPrefix(t.serial(), "emulator-") }

func (t target) key() string {
	switch {
	case t.emu != nil:
		return "e:" + t.emu.Name
	case t.conn != nil:
		return "c:" + t.conn.Serial
	}
	return ""
}

// Model is the root Bubble Tea model.
type Model struct {
	w, h    int
	sc      core.Scan
	host    core.Host // the snapshot the UI displays: read and written on the UI goroutine only
	sampler core.Host // the poll goroutine's own sampler state, guarded by hostMu
	hostMu  sync.Mutex
	scanned bool

	emuSel, emuTop   int
	connSel, connTop int
	snapSel, snapTop int
	focus            focusT
	grp              groupT
	tabIdx           [2]int // index into order[grp] (flat)
	modes            [2][]modeT
	lastTab          [2][]int // last tab used in each mode (flat index)
	modeHits         [][3]int // x0, x1, mode index
	tabs             map[string]Tab
	order            [2][]string

	modals  []modal
	toast   string
	toastK  string
	until   time.Time
	busy    int
	frame   int
	logMode int

	stats       map[string]*core.DevStats
	info        map[string]map[string]string
	profiles    map[string]*core.Profile
	profLoading map[string]bool
	infoLoading map[string]bool
	snaps       []snapEnt
	snapsFor    string
	snapsT      time.Time
	selKey      string
	selChanged  time.Time
	upd         int
	rec         *recording

	pollGen    int
	pollN      int
	pollSerial string
	rects      map[string]rect
	tabHits    [][3]int  // x0, x1, tab index
	lastClick  clickMemo // double-click detection (see input.go)
	topBox     rect      // screen rectangle of the top modal, for its mouse handling
	haveAdb    bool      // adb is on PATH (checked once at start)
	zoom       bool      // main panel fills the screen (Z); any focus change to another pane leaves zoom
	quitting   bool
}

type recording struct {
	serial, remote, name string
	proc                 *os.Process
	t0                   time.Time
}

type execDone struct{ err error }

type execThen struct {
	then func(m *Model, err error) tea.Cmd
	err  error
}
