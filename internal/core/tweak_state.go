package core

import (
	"fmt"
	"strconv"
	"strings"
)

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
