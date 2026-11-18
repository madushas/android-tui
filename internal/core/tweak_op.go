package core

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Exec runs a shell script on the device; root=true wraps it in su.
var Exec = func(serial, script string, root bool) Res {
	if root {
		script = "su -c " + Quote(script)
	}
	return Sh(serial, script, Opt{Timeout: 40 * time.Second})
}

var (
	idxMu  sync.Mutex
	idxMap = map[string]map[string]string{} // serial -> key -> settings namespace
)

// noteSetting updates the cached settings index after a write (new keys become visible without a full refresh).
func noteSetting(serial, ns, key string, present bool) {
	idxMu.Lock()
	defer idxMu.Unlock()
	if m, ok := idxMap[serial]; ok {
		if present {
			m[key] = ns
		} else {
			delete(m, key)
		}
	}
}

// SettingsIndex maps every settings key to its namespace (global/secure/system) — cached per device.
func SettingsIndex(serial string, refresh bool) map[string]string {
	idxMu.Lock()
	defer idxMu.Unlock()
	if m, ok := idxMap[serial]; ok && !refresh {
		return m
	}
	r := Exec(serial, `for ns in system secure global; do echo "@@$ns"; settings list $ns; done`, false)
	m := map[string]string{}
	ns := ""
	for _, l := range strings.Split(r.Out, "\n") {
		if strings.HasPrefix(l, "@@") {
			ns = l[2:]
		} else if k, _, ok := strings.Cut(l, "="); ok && ns != "" {
			m[k] = ns
		}
	}
	idxMap[serial] = m
	return m
}

// resolved turns kind "auto" into a concrete setting/prop op for this device.
func (o Op) resolved(serial string) Op {
	if o.Kind != "auto" {
		return o
	}
	if ns := SettingsIndex(serial, false)[o.Key]; ns != "" {
		o.Kind, o.NS = "setting", ns
		return o
	}
	if strings.TrimSpace(Exec(serial, "getprop "+Quote(o.Key), false).Out) != "" {
		o.Kind = "prop"
		return o
	}
	o.Kind, o.NS = "setting", orDefault(o.Fallback, "global")
	return o
}

func (o Op) ns(serial string) string {
	if o.Kind != "setting" || o.NS != "auto" {
		return o.NS
	}
	if ns := SettingsIndex(serial, false)[o.Key]; ns != "" {
		return ns
	}
	return orDefault(o.Fallback, "global")
}

func orDefault(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// KeyExists answers "does this settings key / system property exist on the device?" (for vendor-specific tweaks).
// kxCache remembers getprop existence checks for 60s: Catalog() asks the same questions on every Tweaks reload.
var (
	kxMu    sync.Mutex
	kxCache = map[string]kxEnt{}
)

type kxEnt struct {
	ok bool
	at time.Time
}

func KeyExists(serial string, p *Profile, key string) bool {
	if _, ok := SettingsIndex(serial, false)[key]; ok {
		return true
	}
	ck := serial + "\x00" + key
	kxMu.Lock()
	e, hit := kxCache[ck]
	kxMu.Unlock()
	if hit && time.Since(e.at) < 60*time.Second {
		return e.ok
	}
	ok := strings.TrimSpace(Exec(serial, "getprop "+Quote(key), false).Out) != ""
	kxMu.Lock()
	kxCache[ck] = kxEnt{ok, time.Now()}
	kxMu.Unlock()
	return ok
}

var bucketNames = map[string]string{"5": "exempted", "10": "active", "20": "working_set", "30": "frequent", "40": "rare", "45": "restricted", "50": "never",
	"ACTIVE": "active", "WORKING_SET": "working_set", "FREQUENT": "frequent", "RARE": "rare", "RESTRICTED": "restricted", "EXEMPTED": "exempted", "NEVER": "never"}

func (o Op) readScript(serial string) string {
	switch o.Kind {
	case "setting":
		return fmt.Sprintf("settings get %s %s", o.ns(serial), Quote(o.Key))
	case "device_config":
		return fmt.Sprintf("device_config get %s %s", Quote(o.NS), Quote(o.Key))
	case "prop":
		return "getprop " + Quote(o.Key)
	case "appops":
		return fmt.Sprintf("cmd appops get %s %s", Quote(o.Pkg), Quote(o.Op))
	case "bucket":
		return "am get-standby-bucket " + Quote(o.Pkg)
	case "pkg":
		q := Quote(o.Pkg)
		return fmt.Sprintf(`i=$(pm list packages %s | grep -x package:%s); d=$(pm list packages -d %s | grep -x package:%s); echo "installed=${i:+1} disabled=${d:+1}"`, q, q, q, q)
	case "wm":
		return "wm " + o.Op
	case "doze":
		return fmt.Sprintf("dumpsys deviceidle whitelist | grep -F %s | head -n1", Quote(","+o.Pkg+","))
	case "home":
		return "cmd package resolve-activity --brief -a android.intent.action.MAIN -c android.intent.category.HOME | tail -n1"
	}
	return ""
}

// Readable reports whether the op's current value can be read (one-shot ops cannot).
func (o Op) Readable() bool { return o.readScript("") != "" }

var kvRE = regexp.MustCompile(`(?:Override|Physical)? ?(?:density|size): ?(\S+)`)

// parseRead normalises command output to (value, exists).
func (o Op) parseRead(out string) (string, bool) {
	out = strings.TrimSpace(out)
	switch o.Kind {
	case "setting", "device_config":
		if out == "null" || out == "" {
			return "", false
		}
		return out, true
	case "prop":
		return out, out != ""
	case "appops":
		if i := strings.Index(out, ": "); i >= 0 {
			v := strings.Fields(strings.SplitN(out[i+2:], ";", 2)[0])
			if len(v) > 0 {
				return v[0], true
			}
		}
		return "default", true
	case "bucket":
		v := strings.TrimSpace(strings.Fields(out + " x")[0])
		if n, ok := bucketNames[strings.ToUpper(v)]; ok {
			return n, true
		}
		return strings.ToLower(v), v != ""
	case "pkg":
		switch {
		case !strings.Contains(out, "installed=1"):
			return "removed", true
		case strings.Contains(out, "disabled=1"):
			return "disabled", true
		}
		return "installed", true
	case "wm":
		val := "reset"
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, "Override") {
				if m := kvRE.FindStringSubmatch(l); m != nil {
					val = m[1]
				}
			}
		}
		return val, true
	case "doze":
		if out != "" {
			return "whitelisted", true
		}
		return "none", true
	case "home":
		return out, out != ""
	}
	return "", false
}

// Read returns the op's current value; exists=false means "unset".
func (o Op) Read(serial string) (string, bool, bool) {
	o = o.resolved(serial)
	s := o.readScript(serial)
	if s == "" {
		return "", false, false
	}
	r := Exec(serial, s, o.Root)
	v, ex := o.parseRead(r.Out)
	return v, ex, true
}

func (o Op) writeScript(serial, v string) string {
	switch o.Kind {
	case "setting":
		return fmt.Sprintf("settings put %s %s %s", o.ns(serial), Quote(o.Key), Quote(v))
	case "device_config":
		return fmt.Sprintf("device_config put %s %s %s", Quote(o.NS), Quote(o.Key), Quote(v))
	case "prop":
		return fmt.Sprintf("setprop %s %s", Quote(o.Key), Quote(v))
	case "appops":
		return fmt.Sprintf("cmd appops set %s %s %s", Quote(o.Pkg), Quote(o.Op), Quote(v))
	case "bucket":
		return fmt.Sprintf("am set-standby-bucket %s %s", Quote(o.Pkg), Quote(v))
	case "pkg":
		q := Quote(o.Pkg)
		switch v {
		case "disabled":
			return "pm disable-user --user 0 " + q
		case "removed":
			return "pm uninstall -k --user 0 " + q
		}
		return fmt.Sprintf("pm enable %s; pm install-existing --user 0 %s", q, q)
	case "wm":
		if v == "reset" || v == "" {
			return "wm " + o.Op + " reset"
		}
		return fmt.Sprintf("wm %s %s", o.Op, Quote(v))
	case "doze":
		if v == "none" {
			return "dumpsys deviceidle whitelist -" + o.Pkg
		}
		return "dumpsys deviceidle whitelist +" + o.Pkg
	case "home":
		return "cmd package set-home-activity " + Quote(v)
	case "shell":
		return strings.ReplaceAll(o.Cmd, "{v}", v)
	case "cleanup":
		return "rm -rf " + Quote(o.Key)
	}
	return "true"
}

func (o Op) unsetScript(serial string) string {
	switch o.Kind {
	case "setting":
		return fmt.Sprintf("settings delete %s %s", o.ns(serial), Quote(o.Key))
	case "device_config":
		return fmt.Sprintf("device_config delete %s %s", Quote(o.NS), Quote(o.Key))
	case "prop":
		return fmt.Sprintf("setprop %s ''", Quote(o.Key))
	}
	return ""
}

// ───────────────────────────── journal ─────────────────────────────
