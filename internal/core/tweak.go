package core

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed packs/*.json
var builtinPacks embed.FS

// ───────────────────────────── data model (JSON packs) ─────────────────────────────

// Op is one reversible (or one-shot) change on the device.
type Op struct {
	Kind     string `json:"kind"` // setting device_config prop appops bucket pkg wm doze home shell cleanup
	NS       string `json:"ns,omitempty"`
	Key      string `json:"key,omitempty"`
	Pkg      string `json:"pkg,omitempty"`
	Op       string `json:"op,omitempty"` // appops op name | wm "density"/"size"
	On       string `json:"on,omitempty"`
	Off      string `json:"off,omitempty"`
	OffUnset bool   `json:"off_unset,omitempty"` // turning the tweak off deletes the key instead of writing Off
	Cmd      string `json:"cmd,omitempty"`       // shell/cleanup apply command
	Undo     string `json:"undo,omitempty"`      // shell undo command
	Root     bool   `json:"root,omitempty"`
	Fallback string `json:"fallback_ns,omitempty"` // for ns "auto" when the key is not found anywhere
}

type Requires struct {
	MinSDK  int      `json:"min_sdk,omitempty"`
	MaxSDK  int      `json:"max_sdk,omitempty"`
	Form    []string `json:"form,omitempty"`
	SoC     []string `json:"soc,omitempty"`
	Brand   []string `json:"brand,omitempty"`
	Root    bool     `json:"root,omitempty"`
	PkgsAny []string `json:"pkgs_any,omitempty"`
	KeysAny []string `json:"keys_any,omitempty"` // settings/props that must exist on the device
}

type Preset struct{ Label, Value string }

type Tweak struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Category string   `json:"category"`
	Desc     string   `json:"desc"`
	Risk     string   `json:"risk"` // safe | caution | risky
	Reboot   bool     `json:"reboot,omitempty"`
	Action   bool     `json:"action,omitempty"` // one-shot (compile, fstrim…): no on/off state
	Requires Requires `json:"requires,omitempty"`
	Special  string   `json:"special,omitempty"` // home_picker | value | prop_glob
	Cmd      string   `json:"cmd,omitempty"`     // special helpers (globs…)
	Ops      []Op     `json:"ops"`
	Presets  []Preset `json:"presets,omitempty"`
	Pack     string   `json:"-"`
}

type DebloatEntry struct {
	Pkg      string   `json:"pkg"`
	Label    string   `json:"label"`
	Risk     string   `json:"risk"`
	Why      string   `json:"why"`
	Requires Requires `json:"requires,omitempty"`
	Pack     string   `json:"-"`
}

type Pack struct {
	Pack     string         `json:"pack"`
	Title    string         `json:"title"`
	Requires Requires       `json:"requires,omitempty"`
	Tweaks   []Tweak        `json:"tweaks"`
	Debloat  []DebloatEntry `json:"debloat"`
}

// LoadPacks returns the built-in packs plus any *.json in ~/.config/android-tui/packs/ (user packs may override ids).
func LoadPacks() []Pack {
	var out []Pack
	read := func(name string, b []byte, user bool) {
		var p Pack
		if err := json.Unmarshal(b, &p); err != nil {
			Logf("err", "pack %s: %v", name, err)
			return
		}
		for i := range p.Tweaks {
			p.Tweaks[i].Pack = p.Pack
			if p.Tweaks[i].Risk == "" {
				p.Tweaks[i].Risk = "caution"
			}
		}
		for i := range p.Debloat {
			p.Debloat[i].Pack = p.Pack
		}
		out = append(out, p)
	}
	ents, _ := builtinPacks.ReadDir("packs")
	for _, e := range ents {
		if b, err := builtinPacks.ReadFile("packs/" + e.Name()); err == nil {
			read(e.Name(), b, false)
		}
	}
	if m, _ := filepath.Glob(filepath.Join(ConfDir, "packs", "*.json")); len(m) > 0 {
		for _, f := range m {
			if b, err := os.ReadFile(f); err == nil {
				read(f, b, true)
			}
		}
	}
	return out
}

// Applicable reports whether a requirement set is satisfied by the profile; reason explains a "no".
func (r Requires) Applicable(p *Profile, keyExists func(string) bool) (bool, string) {
	if p == nil {
		return false, "device not probed"
	}
	if r.MinSDK > 0 && p.SDK < r.MinSDK {
		return false, fmt.Sprintf("needs Android API %d+ (this is %d)", r.MinSDK, p.SDK)
	}
	if r.MaxSDK > 0 && p.SDK > r.MaxSDK {
		return false, fmt.Sprintf("only for API ≤ %d (this is %d)", r.MaxSDK, p.SDK)
	}
	if len(r.Form) > 0 && !contains(r.Form, p.Form) {
		return false, "for " + strings.Join(r.Form, "/") + " devices (this is " + p.Form + ")"
	}
	if len(r.SoC) > 0 && !contains(r.SoC, p.Vendor) {
		return false, "for " + strings.Join(r.SoC, "/") + " SoCs (this is " + p.Vendor + ")"
	}
	if len(r.Brand) > 0 {
		ok := false
		for _, b := range r.Brand {
			ok = ok || p.HasBrand(b)
		}
		if !ok {
			return false, "for " + strings.Join(r.Brand, "/") + " devices"
		}
	}
	if r.Root && !p.Root {
		return false, "needs root"
	}
	if len(r.PkgsAny) > 0 {
		ok := false
		for _, k := range r.PkgsAny {
			ok = ok || p.Installed[k]
		}
		if !ok {
			return false, "none of its packages are installed here"
		}
	}
	if len(r.KeysAny) > 0 && keyExists != nil {
		ok := false
		for _, k := range r.KeysAny {
			ok = ok || keyExists(k)
		}
		if !ok {
			return false, "its settings/properties don't exist on this device"
		}
	}
	return true, ""
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// ───────────────────────────── device access (swappable for tests) ─────────────────────────────

// Catalog resolves which tweaks/debloat entries apply to a device.
type CatalogTweak struct {
	Tweak
	OK     bool
	Reason string
}

func Catalog(p *Profile) ([]CatalogTweak, []DebloatEntry, []DebloatEntry) {
	exists := func(k string) bool { return KeyExists(p.Serial, p, k) }
	var tw []CatalogTweak
	var deb, hidden []DebloatEntry
	for _, pk := range LoadPacks() {
		packOK, packWhy := pk.Requires.Applicable(p, exists)
		for _, t := range pk.Tweaks {
			ok, why := t.Requires.Applicable(p, exists)
			if !packOK {
				ok, why = false, packWhy
			}
			tw = append(tw, CatalogTweak{t, ok, why})
		}
		for _, d := range pk.Debloat {
			if ok, _ := d.Requires.Applicable(p, exists); ok && packOK && p.Installed[d.Pkg] {
				deb = append(deb, d)
			} else if ok && packOK {
				hidden = append(hidden, d)
			}
		}
	}
	sort.SliceStable(tw, func(i, j int) bool { return tw[i].Category < tw[j].Category })
	seen := map[string]bool{}
	var uniq []DebloatEntry
	for _, d := range deb {
		if !seen[d.Pkg] {
			seen[d.Pkg] = true
			uniq = append(uniq, d)
		}
	}
	sort.SliceStable(uniq, func(i, j int) bool { return uniq[i].Pack+uniq[i].Pkg < uniq[j].Pack+uniq[j].Pkg })
	return tw, uniq, hidden
}
