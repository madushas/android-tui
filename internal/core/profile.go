package core

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Profile is what we learned about a device on connection. Tweaks and vendor modules are filtered through it so
// a Unisoc-only property is never offered (let alone applied) on a Snapdragon phone or a Google TV.
type Profile struct {
	Serial, Model, Device, Release, Fingerprint                   string
	Brand, Manufacturer, Platform, SoC, SoCMaker, Hardware, Chars string
	SDK                                                           int
	Form                                                          string   // handheld | tablet | tv | automotive | watch
	Vendor                                                        string   // SoC vendor: unisoc | qualcomm | mediatek | exynos | tensor | unknown
	Brands                                                        []string // lowercase brand family names: zte, nubia, xiaomi, samsung, google, …
	UID                                                           int
	Root                                                          bool // adbd is root, or `su` works
	ShizukuInstalled, ShizukuRunning                              bool
	Features                                                      map[string]bool
	Installed                                                     map[string]bool // every package on the device (including disabled)
	Disabled                                                      map[string]bool
	Probed                                                        time.Time
}

const probeScript = `for k in ro.product.model ro.product.brand ro.product.manufacturer ro.product.device ro.board.platform ro.soc.model ro.soc.manufacturer ro.hardware ro.build.version.release ro.build.version.sdk ro.build.fingerprint ro.build.characteristics ro.miui.ui.version.name ro.build.nothing.version; do echo "$k=$(getprop $k)"; done
echo "uid=$(id -u)"
echo "su=$( (timeout 3 su -c 'id -u' 2>/dev/null || true) | head -n1)"
echo "shizuku_pkg=$(pm list packages moe.shizuku.privileged.api | head -n1)"
echo "shizuku_run=$(ps -A 2>/dev/null | grep -c shizuku_server)"
echo @@features; pm list features
echo @@pkgs; pm list packages
echo @@dis; pm list packages -d`

var (
	reUnisoc = regexp.MustCompile(`unisoc|spreadtrum|\bums\d+|\bsp\d{4}[a-z]?\b|\bsc\d{4}\b|\bud\d{3,4}\b|^t\d{3}$`)
	reQcom   = regexp.MustCompile(`qcom|qualcomm|\bmsm\d+|\bsdm\d+|\bsm\d{4}|kona|lahaina|taro|kalama|bengal|holi|atoll|lito|trinket|parrot|crow|pineapple|sun\b`)
	reMTK    = regexp.MustCompile(`mediatek|\bmt\d{4}`)
	reExynos = regexp.MustCompile(`exynos|universal\d+|s5e\d+`)
	reTensor = regexp.MustCompile(`\bgs\d{3}|zuma|tensor`)
)

// Classify fills Form, Vendor and Brands from the raw properties (exported for tests).
func (p *Profile) Classify(miui bool) {
	soc := strings.ToLower(strings.Join([]string{p.Platform, p.SoC, p.SoCMaker, p.Hardware}, " "))
	switch {
	case reUnisoc.MatchString(soc) || reUnisoc.MatchString(strings.ToLower(p.SoC)):
		p.Vendor = "unisoc"
	case reMTK.MatchString(soc):
		p.Vendor = "mediatek"
	case reExynos.MatchString(soc):
		p.Vendor = "exynos"
	case reTensor.MatchString(soc):
		p.Vendor = "tensor"
	case reQcom.MatchString(soc):
		p.Vendor = "qualcomm"
	default:
		p.Vendor = "unknown"
	}
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" && !seen[s] {
			seen[s] = true
			p.Brands = append(p.Brands, s)
		}
	}
	for _, s := range []string{p.Brand, p.Manufacturer} {
		add(s)
		switch strings.ToLower(s) {
		case "redmi", "poco", "xiaomi":
			add("xiaomi")
		case "nubia", "zte", "redmagic":
			add("zte")
			add("nubia")
		case "vivo", "iqoo":
			add("vivo")
		case "oppo", "realme", "oneplus":
			add("oppo") // ColorOS/OxygenOS family (HeyTap packages)
		}
	}
	if miui {
		add("xiaomi")
	}
	switch {
	case p.Features["android.software.leanback"]:
		p.Form = "tv"
	case p.Features["android.hardware.type.automotive"]:
		p.Form = "automotive"
	case p.Features["android.hardware.type.watch"]:
		p.Form = "watch"
	case strings.Contains(strings.ToLower(p.Chars), "tablet"):
		p.Form = "tablet"
	default:
		p.Form = "handheld"
	}
}

func (p *Profile) HasBrand(b string) bool {
	for _, x := range p.Brands {
		if x == b {
			return true
		}
	}
	return false
}

// Privilege is a short human label: "root", "shell" (+ shizuku note).
func (p *Profile) Privilege() string {
	switch {
	case p.Root:
		return "root"
	case p.ShizukuRunning:
		return "shell · Shizuku running"
	case p.ShizukuInstalled:
		return "shell · Shizuku installed (not running)"
	}
	return "shell"
}

func (p *Profile) Summary() string {
	return strings.TrimSpace(p.Form + " · " + p.Vendor + " · API " + strconv.Itoa(p.SDK) + " · " + p.Privilege())
}

// ProbeDevice runs one adb call and builds the profile.
func ProbeDevice(serial string) *Profile {
	r := Sh(serial, probeScript, Opt{Timeout: 30 * time.Second, Quiet: true})
	return ParseProfile(serial, r.Out)
}

func ParseProfile(serial, out string) *Profile {
	p := &Profile{Serial: serial, Features: map[string]bool{}, Installed: map[string]bool{}, Disabled: map[string]bool{}, Probed: time.Now()}
	sec := ""
	kv := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "@@") {
			sec = l[2:]
			continue
		}
		switch sec {
		case "features":
			if strings.HasPrefix(l, "feature:") {
				p.Features[strings.SplitN(l[8:], "=", 2)[0]] = true
			}
		case "pkgs":
			if strings.HasPrefix(l, "package:") {
				p.Installed[l[8:]] = true
			}
		case "dis":
			if strings.HasPrefix(l, "package:") {
				p.Disabled[l[8:]] = true
			}
		default:
			if k, v, ok := strings.Cut(l, "="); ok {
				kv[k] = v
			}
		}
	}
	p.Model, p.Brand, p.Manufacturer, p.Device = kv["ro.product.model"], kv["ro.product.brand"], kv["ro.product.manufacturer"], kv["ro.product.device"]
	p.Platform, p.SoC, p.SoCMaker, p.Hardware = kv["ro.board.platform"], kv["ro.soc.model"], kv["ro.soc.manufacturer"], kv["ro.hardware"]
	p.Release, p.Fingerprint, p.Chars = kv["ro.build.version.release"], kv["ro.build.fingerprint"], kv["ro.build.characteristics"]
	p.SDK, _ = strconv.Atoi(kv["ro.build.version.sdk"])
	p.UID, _ = strconv.Atoi(kv["uid"])
	p.Root = p.UID == 0 || kv["su"] == "0"
	p.ShizukuInstalled = strings.Contains(kv["shizuku_pkg"], "moe.shizuku.privileged.api")
	p.ShizukuRunning = kv["shizuku_run"] != "" && kv["shizuku_run"] != "0"
	p.Classify(kv["ro.miui.ui.version.name"] != "")
	// Nothing OS sets its own build prop (ro.build.nothing.version), while the product brand/manufacturer strings
	// can name the chipset vendor instead, so detect it by that prop. This also covers CMF devices.
	if kv["ro.build.nothing.version"] != "" && !p.HasBrand("nothing") {
		p.Brands = append(p.Brands, "nothing")
	}
	return p
}
