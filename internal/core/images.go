package core

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var OfficialTags = map[string]string{
	"default": "AOSP (no Google)", "google_apis": "Google APIs", "google_apis_playstore": "Google Play",
	"google_apis_tablet": "Google APIs Tablet", "google_apis_playstore_tablet": "Google Play Tablet",
	"google_atd": "Google ATD (test)", "aosp_atd": "AOSP ATD (test)", "android-tv": "Android TV", "google-tv": "Google TV",
	"android-wear": "Wear OS", "android-wear-cn": "Wear OS (CN)", "android-automotive": "Automotive",
	"android-automotive-playstore": "Automotive + Play", "android-desktop": "Desktop", "android-xr": "XR",
}

func IsOfficialTag(t string) bool { _, ok := OfficialTags[t]; return ok }

func TagLabel(t string) string {
	if v, ok := OfficialTags[t]; ok {
		return v
	}
	return "custom · " + t
}

var apiNames = map[int]string{21: "5.0", 22: "5.1", 23: "6.0", 24: "7.0", 25: "7.1", 26: "8.0", 27: "8.1", 28: "9", 29: "10", 30: "11",
	31: "12", 32: "12L", 33: "13", 34: "14", 35: "15", 36: "16", 37: "17"}

func APILabel(api string) string {
	if n, err := strconv.Atoi(strings.SplitN(api, ".", 2)[0]); err == nil {
		if v, ok := apiNames[n]; ok {
			return fmt.Sprintf("%s (Android %s)", api, v)
		}
		return fmt.Sprintf("%s (Android ?)", api)
	}
	return api
}

// Image is an installed system image.
type Image struct {
	Rel, API, Tag, ABI, Name string
	Official                 bool
	Used                     []string
	Size                     int64
}

func ReadProps(path string) map[string]string {
	m := map[string]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	for _, l := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(strings.TrimRight(l, "\r"), "="); ok {
			m[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return m
}

func ScanImages() []Image {
	if SDK == "" {
		return nil
	}
	used := map[string][]string{}
	for _, n := range ListNames() {
		k := strings.Trim(IniGet(CfgPath(n), "image.sysdir.1"), "/")
		used[k] = append(used[k], n)
	}
	m, _ := filepath.Glob(filepath.Join(SDK, "system-images", "*", "*", "*"))
	var out []Image
	for _, d := range m {
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			continue
		}
		rel, _ := filepath.Rel(SDK, d)
		p := strings.Split(rel, string(filepath.Separator))
		props := ReadProps(filepath.Join(d, "source.properties"))
		name := props["Pkg.Desc"]
		if name == "" {
			name = props["SystemImage.TagDisplay"]
		}
		out = append(out, Image{Rel: filepath.ToSlash(rel), API: strings.TrimPrefix(p[1], "android-"), Tag: p[2], ABI: p[3], Name: name,
			Official: IsOfficialTag(p[2]), Used: used[filepath.ToSlash(rel)]})
	}
	key := func(i Image) float64 {
		f, err := strconv.ParseFloat(i.API, 64)
		if err != nil {
			return -999
		}
		return -f
	}
	sort.Slice(out, func(i, j int) bool {
		if key(out[i]) != key(out[j]) {
			return key(out[i]) < key(out[j])
		}
		if out[i].Tag != out[j].Tag {
			return out[i].Tag < out[j].Tag
		}
		return out[i].ABI < out[j].ABI
	})
	return out
}

// InstalledImagesForHost lists "system-images/..." rels usable on this machine.
func InstalledImagesForHost() []string {
	var out []string
	abis := map[string]bool{}
	for _, a := range HostABIs() {
		abis[a] = true
	}
	for _, i := range ScanImages() {
		if abis[i.ABI] {
			out = append(out, i.Rel)
		}
	}
	return out
}

func ImgLabel(rel string) string {
	p := strings.Split(strings.Trim(rel, "/"), "/")
	if len(p) >= 4 {
		return strings.Join(p[1:4], " · ")
	}
	return rel
}

// ───────────────────────────── package.xml ─────────────────────────────

func esc(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func PackageXML(api, tag, tagDisplay, abi, display string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<ns2:repository xmlns:ns2="http://schemas.android.com/repository/android/common/02" xmlns:ns5="http://schemas.android.com/repository/android/generic/02" xmlns:ns12="http://schemas.android.com/sdk/android/repo/sys-img2/03">
  <localPackage path="system-images;android-%[1]s;%[2]s;%[4]s" obsolete="false">
    <type-details xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="ns12:sysImgDetailsType">
      <api-level>%[1]s</api-level>
      <base-extension>true</base-extension>
      <tag>
        <id>%[2]s</id>
        <display>%[3]s</display>
      </tag>
      <abi>%[4]s</abi>
    </type-details>
    <revision>
      <major>1</major>
    </revision>
    <display-name>%[5]s</display-name>
  </localPackage>
</ns2:repository>
`, esc(api), esc(tag), esc(tagDisplay), esc(abi), esc(display))
}

// EnsurePackageXML makes a custom image visible to avdmanager / Android Studio.
func EnsurePackageXML(dir string, force bool) bool {
	px := filepath.Join(dir, "package.xml")
	if exists(px) && !force {
		return false
	}
	p := strings.Split(filepath.ToSlash(dir), "/")
	n := len(p)
	abi, tag, api := p[n-1], p[n-2], strings.TrimPrefix(p[n-3], "android-")
	props := ReadProps(filepath.Join(dir, "source.properties"))
	disp := props["SystemImage.TagDisplay"]
	if disp == "" {
		disp = OfficialTags[tag]
	}
	if disp == "" {
		disp = strings.Title(strings.ReplaceAll(tag, "_", " ")) //nolint:staticcheck
	}
	desc := props["Pkg.Desc"]
	if desc == "" {
		desc = disp + " System Image"
	}
	return os.WriteFile(px, []byte(PackageXML(api, tag, disp, abi, desc)), 0o644) == nil
}

// ───────────────────────────── downloads ─────────────────────────────

type ImgMeta struct{ API, Tag, ABI string }

func orQ(s string) string {
	if s == "" {
		return "?"
	}
	return s
}

func max64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ImageProblems sanity-checks a system-image dir.
func ImageProblems(rel string) []string {
	d := filepath.Join(SDK, rel)
	var probs []string
	var miss []string
	for _, f := range []string{"system.img", "ramdisk.img"} {
		if !exists(filepath.Join(d, f)) {
			miss = append(miss, f)
		}
	}
	if len(miss) > 0 {
		probs = append(probs, "missing "+strings.Join(miss, ", "))
	}
	if !exists(filepath.Join(d, "kernel-ranchu")) && !exists(filepath.Join(d, "kernel-qemu")) {
		probs = append(probs, "no kernel-ranchu (the emulator may fall back to its bundled kernel)")
	}
	if !exists(filepath.Join(d, "source.properties")) {
		probs = append(probs, "no source.properties")
	}
	if !exists(filepath.Join(d, "package.xml")) {
		probs = append(probs, "no package.xml (avdmanager/Android Studio won't list it) — use 'fix package.xml'")
	}
	return probs
}

func DeleteImage(rel string) (bool, string) {
	rel = strings.Trim(rel, "/")
	var used []string
	for _, n := range ListNames() {
		if strings.Trim(IniGet(CfgPath(n), "image.sysdir.1"), "/") == rel {
			used = append(used, n)
		}
	}
	if len(used) > 0 {
		return false, "in use by: " + strings.Join(used, ", ")
	}
	if !strings.HasPrefix(rel, "system-images/") || strings.Count(rel, "/") < 3 {
		return false, "refusing to delete outside system-images"
	}
	pruneEmpty(filepath.Join(SDK, rel))
	return true, "deleted " + rel
}
