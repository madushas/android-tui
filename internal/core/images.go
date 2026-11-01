package core

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
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

// Emit reports progress; replace=true overwrites the previous replaceable line.
type Emit func(text string, replace bool)

func httpGet(u string, limit int64, timeout time.Duration) ([]byte, error) {
	c := &http.Client{Timeout: timeout}
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "android-tui/"+Version)
	r, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Body.Close() }()
	if r.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", r.StatusCode)
	}
	return io.ReadAll(io.LimitReader(r.Body, limit))
}

var zipHref = regexp.MustCompile(`(?i)href=["']([^"'?#]+\.zip)["']`)

type Remote struct{ Name, URL string }

// ListRemoteZips scrapes .zip links from a directory-listing page (newest-looking last).
func ListRemoteZips(page string) ([]Remote, error) {
	b, err := httpGet(page, 4<<20, 20*time.Second)
	if err != nil {
		return nil, err
	}
	base, _ := url.Parse(page)
	seen := map[string]bool{}
	var out []Remote
	for _, m := range zipHref.FindAllStringSubmatch(string(b), -1) {
		u, err := base.Parse(m[1])
		if err != nil || seen[u.String()] {
			continue
		}
		seen[u.String()] = true
		name, _ := url.PathUnescape(filepath.Base(u.Path))
		out = append(out, Remote{name, u.String()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Download streams u to dest and returns its sha256 (hex).
func Download(u, dest string, emit Emit, cancelled func() bool) (string, error) {
	c := &http.Client{}
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "android-tui/"+Version)
	r, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = r.Body.Close() }()
	if r.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d", r.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	// Any failure (including a full disk or a cancel) must not leave a truncated file that looks like a finished download.
	fail := func(err error) (string, error) {
		_ = f.Close()
		_ = os.Remove(dest)
		return "", err
	}
	h := sha256.New()
	buf := make([]byte, 1<<20)
	var done int64
	t0, last := time.Now(), time.Time{}
	for {
		if cancelled() {
			return fail(fmt.Errorf("cancelled"))
		}
		n, err := r.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return fail(fmt.Errorf("writing %s: %w", dest, werr))
			}
			h.Write(buf[:n]) // hash.Hash.Write never returns an error
			done += int64(n)
			if time.Since(last) > 250*time.Millisecond {
				last = time.Now()
				pct := ""
				if r.ContentLength > 0 {
					pct = fmt.Sprintf("%5.1f%%  ", 100*float64(done)/float64(r.ContentLength))
				}
				emit(fmt.Sprintf("  %s%s   %s/s", pct, Human(float64(done)), Human(float64(done)/time.Since(t0).Seconds())), true)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fail(err)
		}
	}
	if err := f.Close(); err != nil { // a failed close can mean the last buffered data never reached the disk
		_ = os.Remove(dest)
		return "", fmt.Errorf("writing %s: %w", dest, err)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	emit(fmt.Sprintf("  downloaded %s  sha256 %s…", Human(float64(done)), sum[:16]), true)
	return sum, nil
}

var shaRE = regexp.MustCompile(`^\s*([0-9a-fA-F]{64})`)

func RemoteSHA256(u string) string {
	for _, suf := range []string{".sha256sum", ".sha256"} {
		if b, err := httpGet(u+suf, 4096, 10*time.Second); err == nil {
			if m := shaRE.FindStringSubmatch(string(b)); m != nil {
				return strings.ToLower(m[1])
			}
		}
	}
	return ""
}

// ───────────────────────────── import ─────────────────────────────

type ImgMeta struct{ API, Tag, ABI string }

func pruneEmpty(path string) {
	_ = os.RemoveAll(path)
	for up := filepath.Dir(path); filepath.Base(up) != "system-images" && up != "/" && up != "."; up = filepath.Dir(up) {
		if os.Remove(up) != nil {
			break
		}
	}
}

// ImportZip installs an SDK-layout system-image zip (AOSP/LineageOS/e-OS builds). Returns (ok, msg, rel).
func ImportZip(zpath string, emit Emit, meta *ImgMeta, overwrite bool) (bool, string, string) {
	if SDK == "" {
		return false, "Android SDK not found — set SDK_ROOT in ~/.config/android-tui/config", ""
	}
	zr, err := zip.OpenReader(zpath)
	if err != nil {
		return false, "not a valid zip: " + err.Error(), ""
	}
	defer func() { _ = zr.Close() }()
	var sp *zip.File
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "source.properties") && (sp == nil || strings.Count(f.Name, "/") < strings.Count(sp.Name, "/")) {
			sp = f
		}
	}
	prefix, props := "", map[string]string{}
	if sp != nil {
		prefix = strings.TrimSuffix(sp.Name, "source.properties")
		if rc, err := sp.Open(); err == nil {
			b, _ := io.ReadAll(rc)
			_ = rc.Close()
			for _, l := range strings.Split(string(b), "\n") {
				if k, v, ok := strings.Cut(strings.TrimRight(l, "\r"), "="); ok {
					props[strings.TrimSpace(k)] = strings.TrimSpace(v)
				}
			}
		}
	}
	api, tag, abi := props["AndroidVersion.ApiLevel"], props["SystemImage.TagId"], props["SystemImage.Abi"]
	if meta != nil {
		if meta.API != "" {
			api = meta.API
		}
		if meta.Tag != "" {
			tag = meta.Tag
		}
		if meta.ABI != "" {
			abi = meta.ABI
		}
	}
	if abi == "" && prefix != "" {
		abi = filepath.Base(strings.TrimRight(prefix, "/"))
	}
	if api == "" || tag == "" || abi == "" {
		return false, fmt.Sprintf("can't tell API/tag/ABI from the zip (found: api=%s tag=%s abi=%s)", orQ(api), orQ(tag), orQ(abi)), ""
	}
	safe := regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	if !safe.MatchString(tag) || !safe.MatchString(api) || !safe.MatchString(abi) {
		return false, "unsafe characters in API/tag/ABI", ""
	}
	okABI := false
	for _, a := range HostABIs() {
		okABI = okABI || a == abi
	}
	if !okABI {
		emit(fmt.Sprintf("  ! warning: ABI %s doesn't match this host (%s) — it will be slow or won't boot", abi, strings.Join(HostABIs(), ", ")), false)
	}
	dest := filepath.Join(SDK, "system-images", "android-"+api, tag, abi)
	rel := filepath.ToSlash(filepath.Join("system-images", "android-"+api, tag, abi))
	var members []*zip.File
	var need uint64
	for _, f := range zr.File {
		if !f.FileInfo().IsDir() && strings.HasPrefix(f.Name, prefix) && f.Name != prefix {
			members = append(members, f)
			need += f.UncompressedSize64
		}
	}
	if ents, err := os.ReadDir(dest); err == nil && len(ents) > 0 && !overwrite {
		return false, rel + " already exists", rel
	}
	if free := diskFree(SDK); free > 0 && float64(need)*1.1 > float64(free) {
		return false, fmt.Sprintf("not enough disk space: need %s, %s free", Human(float64(need)), Human(float64(free))), ""
	}
	emit(fmt.Sprintf("→ %s   (%d files, %s)", rel, len(members), Human(float64(need))), false)
	_ = os.RemoveAll(dest)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return false, err.Error(), ""
	}
	base, _ := filepath.Abs(dest)
	var done uint64
	for _, f := range members {
		target := filepath.Join(base, filepath.FromSlash(f.Name[len(prefix):]))
		if !strings.HasPrefix(target, base+string(filepath.Separator)) { // zip-slip guard
			pruneEmpty(dest)
			return false, "unsafe path in zip: " + f.Name, ""
		}
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		rc, err := f.Open()
		if err != nil {
			pruneEmpty(dest)
			return false, err.Error(), ""
		}
		out, err := os.Create(target)
		if err != nil {
			_ = rc.Close()
			pruneEmpty(dest)
			return false, err.Error(), ""
		}
		_, err = io.Copy(out, rc)
		if cerr := out.Close(); err == nil {
			err = cerr // a failed close can hide a short write
		}
		_ = rc.Close()
		if err != nil {
			pruneEmpty(dest)
			return false, "extract failed: " + err.Error(), ""
		}
		done += f.UncompressedSize64
		emit(fmt.Sprintf("  %5.1f%%  %s", 100*float64(done)/float64(max64(1, need)), f.Name[len(prefix):]), true)
	}
	if !exists(filepath.Join(dest, "source.properties")) {
		_ = os.WriteFile(filepath.Join(dest, "source.properties"), []byte(fmt.Sprintf(
			"Pkg.Desc=%s System Image\nPkg.Revision=1\nSystemImage.Abi=%s\nSystemImage.TagId=%s\nSystemImage.TagDisplay=%s\nAndroidVersion.ApiLevel=%s\n",
			TagLabel(tag), abi, tag, TagDisplay(tag), api)), 0o644)
	}
	if EnsurePackageXML(dest, false) {
		emit("  wrote package.xml", false)
	} else {
		emit("  package.xml present", false)
	}
	return true, "installed " + rel, rel
}

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

// ImportDir copies an extracted image folder (the <abi> directory) into the SDK.
func ImportDir(src string, emit Emit, meta *ImgMeta) (bool, string, string) {
	sp := filepath.Join(src, "source.properties")
	if !exists(sp) {
		m, _ := filepath.Glob(filepath.Join(src, "*", "source.properties"))
		if len(m) == 0 {
			return false, "no source.properties found there", ""
		}
		sp = m[0]
	}
	root := filepath.Dir(sp)
	props := ReadProps(sp)
	api, tag, abi := props["AndroidVersion.ApiLevel"], props["SystemImage.TagId"], props["SystemImage.Abi"]
	if meta != nil {
		api, tag, abi = firstNonEmpty(meta.API, api), firstNonEmpty(meta.Tag, tag), firstNonEmpty(meta.ABI, abi)
	}
	abi = firstNonEmpty(abi, filepath.Base(root))
	if api == "" || tag == "" {
		return false, "can't tell API/tag/ABI from the folder", ""
	}
	rel := filepath.ToSlash(filepath.Join("system-images", "android-"+api, tag, abi))
	dest := filepath.Join(SDK, rel)
	emit(fmt.Sprintf("copying %s → %s", root, dest), false)
	if r := Run([]string{"cp", "-a", root + "/.", dest}, Opt{Timeout: time.Hour}); !r.OK() {
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return false, err.Error(), ""
		}
		if r := Run([]string{"cp", "-a", root + "/.", dest}, Opt{Timeout: time.Hour}); !r.OK() {
			return false, "copy failed: " + r.Why(), ""
		}
	}
	EnsurePackageXML(dest, false)
	return true, "installed " + rel, rel
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

type Official struct{ Pkg, Label, API string }

// OfficialImages lists downloadable images for this host via the android CLI.
func OfficialImages() ([]Official, bool) {
	r := Run([]string{"android", "sdk", "list", "--all", "system-images/*"}, Opt{Timeout: 2 * time.Minute})
	if !r.OK() {
		return nil, false
	}
	have := map[string]bool{}
	for _, i := range ScanImages() {
		have[i.Rel] = true
	}
	abis := map[string]bool{}
	for _, a := range HostABIs() {
		abis[a] = true
	}
	var out []Official
	for _, l := range strings.Split(r.Out, "\n") {
		f := strings.Fields(l)
		if len(f) == 0 || !strings.HasPrefix(f[0], "system-images/") {
			continue
		}
		p := strings.Split(f[0], "/")
		if len(p) >= 4 && abis[p[3]] && !have[f[0]] {
			out = append(out, Official{f[0], TagLabel(p[2]), strings.TrimPrefix(p[1], "android-")})
		}
	}
	return out, true
}

// ───────────────────────────── AVD creation / flags ─────────────────────────────

type HWProfile struct {
	Name         string
	W, H, D, RAM int
	Cores        int
	Dev          string
}

var HWProfiles = []HWProfile{
	{"Phone  (1080x2400 · 420dpi)", 1080, 2400, 420, 2048, 4, "pixel_6"},
	{"Small phone  (720x1280 · 320dpi)", 720, 1280, 320, 1536, 2, "Nexus 5"},
	{"Tablet  (2560x1600 · 320dpi)", 2560, 1600, 320, 3072, 4, "pixel_c"},
	{"TV 1080p  (1920x1080 · 213dpi)", 1920, 1080, 213, 2048, 4, "tv_1080p"},
}

// CreateAVDManual writes an AVD directly — works for custom ROM tags `android emulator create` doesn't know.
func CreateAVDManual(name, rel string, p HWProfile) (bool, string) {
	if !ValidName(name) {
		return false, "invalid name (letters, digits, . _ - only)"
	}
	if exists(filepath.Join(AVDHome, name+".ini")) || exists(filepath.Join(AVDHome, name+".avd")) {
		return false, fmt.Sprintf("'%s' already exists", name)
	}
	parts := strings.Split(strings.Trim(rel, "/"), "/")
	if len(parts) < 4 || !exists(filepath.Join(SDK, rel)) {
		return false, "image not found: " + rel
	}
	api, tag, abi := strings.TrimPrefix(parts[1], "android-"), parts[2], parts[3]
	d := filepath.Join(AVDHome, name+".avd")
	if err := os.MkdirAll(d, 0o755); err != nil {
		return false, err.Error()
	}
	arch := map[string]string{"arm64-v8a": "arm64", "armeabi-v7a": "arm"}[abi]
	if arch == "" {
		arch = abi
	}
	orient := "portrait"
	if p.W > p.H {
		orient = "landscape"
	}
	b2s := func(b bool) string { return map[bool]string{true: "yes", false: "no"}[b] }
	cfg := [][2]string{
		{"AvdId", name}, {"PlayStore.enabled", fmt.Sprint(strings.Contains(tag, "playstore"))}, {"abi.type", abi},
		{"avd.ini.displayname", name}, {"avd.ini.encoding", "UTF-8"}, {"disk.dataPartition.size", "6G"},
		{"fastboot.chosenSnapshotFile", ""}, {"fastboot.forceChosenSnapshotBoot", "no"}, {"fastboot.forceColdBoot", "no"},
		{"fastboot.forceFastBoot", "yes"}, {"hw.accelerometer", "yes"}, {"hw.arc", "false"}, {"hw.audioInput", "yes"},
		{"hw.battery", "yes"}, {"hw.camera.back", "emulated"}, {"hw.camera.front", "emulated"}, {"hw.cpu.arch", arch},
		{"hw.cpu.ncore", fmt.Sprint(p.Cores)}, {"hw.device.manufacturer", "Generic"}, {"hw.device.name", p.Dev},
		{"hw.dPad", b2s(strings.Contains(tag, "tv"))}, {"hw.gps", "yes"}, {"hw.gpu.enabled", "yes"}, {"hw.gpu.mode", "auto"},
		{"hw.initialOrientation", orient}, {"hw.keyboard", "yes"}, {"hw.lcd.density", fmt.Sprint(p.D)}, {"hw.lcd.height", fmt.Sprint(p.H)},
		{"hw.lcd.width", fmt.Sprint(p.W)}, {"hw.mainKeys", "no"}, {"hw.ramSize", fmt.Sprint(p.RAM)}, {"hw.sdCard", "yes"},
		{"hw.sensors.orientation", "yes"}, {"hw.sensors.proximity", "yes"}, {"hw.trackBall", "no"},
		{"image.sysdir.1", strings.Trim(rel, "/") + "/"}, {"runtime.network.latency", "none"}, {"runtime.network.speed", "full"},
		{"sdcard.size", "512M"}, {"showDeviceFrame", "no"}, {"tag.display", TagDisplay(tag)}, {"tag.id", tag}, {"vm.heapSize", "256"},
	}
	var sb strings.Builder
	for _, kv := range cfg {
		sb.WriteString(kv[0] + "=" + kv[1] + "\n")
	}
	if err := os.WriteFile(filepath.Join(d, "config.ini"), []byte(sb.String()), 0o644); err != nil {
		return false, err.Error()
	}
	ini := fmt.Sprintf("avd.ini.encoding=UTF-8\npath=%s\npath.rel=avd/%s.avd\ntarget=android-%s\n", d, name, api)
	if err := os.WriteFile(filepath.Join(AVDHome, name+".ini"), []byte(ini), 0o644); err != nil {
		return false, err.Error()
	}
	return true, fmt.Sprintf("created AVD %s from %s · API %s · %s", name, TagLabel(tag), api, abi)
}

// FlagPresets are one-tap emulator launch flags.
var FlagPresets = [][2]string{
	{"-gpu host", "Hardware GPU (host) — recommended for custom ROMs"},
	{"-feature -Vulkan", "Disable Vulkan (fixes many graphics crashes)"},
	{"-writable-system", "Writable /system (ROM work; then adb root + remount)"},
	{"-no-audio", "No audio"},
	{"-no-boot-anim", "Skip boot animation"},
	{"-no-snapshot", "Disable snapshots (always cold, never saves)"},
	{"-dns-server 1.1.1.1,8.8.8.8", "Custom DNS servers"},
	{"-netspeed lte", "Throttle network to LTE"},
	{"-camera-back webcam0", "Host webcam as back camera"},
	{"-memory 4096", "4 GB RAM override"},
}

// SetAVDFlags rewrites the AVD_FLAGS[name] line of the config file (backup kept once).
func SetAVDFlags(name, flags string) (bool, string) {
	flags = strings.Join(strings.Fields(flags), " ")
	if strings.ContainsAny(flags, "\"$`\\\n") {
		return false, `flags may not contain " $ ` + "`" + ` or backslashes`
	}
	MkdirP(ConfDir)
	text := ConfigTemplate
	if b, err := os.ReadFile(ConfFile); err == nil {
		text = string(b)
		if !exists(ConfFile + ".emu-bak") {
			_ = os.WriteFile(ConfFile+".emu-bak", b, 0o644)
		}
	}
	pat := regexp.MustCompile(`^\s*AVD_FLAGS\[` + regexp.QuoteMeta(name) + `\]=`)
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if !pat.MatchString(l) {
			lines = append(lines, l)
		}
	}
	if flags != "" {
		lines = append(lines, fmt.Sprintf(`AVD_FLAGS[%s]="%s"`, name, flags))
		Cfg.AVDFlags[name] = flags
	} else {
		delete(Cfg.AVDFlags, name)
	}
	if err := os.WriteFile(ConfFile, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		return false, err.Error()
	}
	if flags == "" {
		flags = "(none)"
	}
	return true, fmt.Sprintf("launch flags for %s: %s", name, flags)
}
