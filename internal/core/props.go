package core

import (
	"regexp"
	"strings"
)

type PropChange struct{ Key, Cur, New string }

var truthy = map[string]string{"1": "0", "true": "false", "on": "off", "yes": "no", "enabled": "disabled", "enable": "disable", "y": "n"}

func globRE(g string) *regexp.Regexp {
	return regexp.MustCompile("^" + strings.ReplaceAll(regexp.QuoteMeta(g), `\*`, ".*") + "$")
}

// GlobPlan lists the properties matching any glob whose value looks "enabled", with the value that would disable them.
func GlobPlan(serial string, globs []string) []PropChange {
	var res []regexp.Regexp
	for _, g := range globs {
		res = append(res, *globRE(strings.TrimSpace(g)))
	}
	var out []PropChange
	for _, kv := range ParseProps(Exec(serial, "getprop", false).Out) {
		for i := range res {
			if res[i].MatchString(kv.K) {
				if nv, ok := truthy[strings.ToLower(kv.V)]; ok {
					out = append(out, PropChange{kv.K, kv.V, nv})
				}
				break
			}
		}
	}
	return out
}

// ───────────────────────────── launcher / Shizuku ─────────────────────────────

// HomeActivities lists components that can act as the Home launcher, plus the current one.
func HomeActivities(serial string) (cands []string, current string) {
	r := Exec(serial, "cmd package query-activities --brief -a android.intent.action.MAIN -c android.intent.category.HOME", false)
	seen := map[string]bool{}
	for _, l := range strings.Split(r.Out, "\n") {
		l = strings.TrimSpace(l)
		if strings.Contains(l, "/") && !strings.Contains(l, " ") && !seen[l] {
			seen[l] = true
			cands = append(cands, l)
		}
	}
	cur := Exec(serial, "cmd package resolve-activity --brief -a android.intent.action.MAIN -c android.intent.category.HOME | tail -n1", false)
	return cands, strings.TrimSpace(cur.Out)
}
