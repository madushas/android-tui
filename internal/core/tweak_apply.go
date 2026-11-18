package core

import (
	"fmt"
	"strings"
	"time"
)

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
