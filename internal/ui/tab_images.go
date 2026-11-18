package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

type romSource struct{ Name, URL string }

var defaultSources = []romSource{{"/e/OS emulator builds (x86_64)", "https://ota.ecloud.global/test/sdk_phone_x86_64/"}}

const romGuide = `Custom / open-source ROMs in the Android Emulator
══════════════════════════════════════════════════
An emulator "ROM" is just an SDK system image:  <sdk>/system-images/android-<API>/<tag>/<abi>/
containing system.img, ramdisk.img, kernel-ranchu, source.properties (+ package.xml).
Anything shipped in that layout works — emu imports it, writes the missing package.xml and
can create an AVD for it, with sane launch flags.

Where to get them
  • Google official (AOSP / Google APIs / Play / ATD / TV / Wear / Auto / Tablet / Desktop):
      Images tab → "Official images".
  • /e/OS: prebuilt emulator images (+ .sha256sum) — Images tab → "/e/OS emulator builds".
  • LineageOS: no official emulator downloads — you build it yourself (wiki.lineageos.org/emulator):
      repo init … -b lineage-21.0 ; repo sync ; source build/envsetup.sh
      breakfast sdk_phone_x86_64 eng ; mka ; mka emu_img_zip
      → out/target/product/<arch>/sdk-repo-linux-system-images.zip   (tag: lineage)
      then Images tab → "Import ROM from a local .zip".
  • Any other build that exports an SDK-repo image zip, or an unpacked directory with source.properties.

Booting custom ROMs
  Recommended launch flags:  -gpu host -feature -Vulkan   (AVD tab → Launch flags).
  The old swiftshader_indirect / guest GPU modes are deprecated and crash on newer emulators.
  For ROM hacking add -writable-system, then:  adb root ; adb remount.
  Custom builds are usually userdebug/eng with test keys — don't sign in with real accounts.`

type imgRow struct {
	kind string // img | act | off | src | remote
	img  core.Image
	pkg  string
	src  romSource
	rem  core.Remote
	fn   func(m *Model) tea.Cmd
}

type imagesTab struct {
	rowsTab
	inst     []core.Image
	loaded   bool
	loading  bool
	official []core.Official
	offOpen  bool
	open     map[string]bool
	remote   map[string][]core.Remote
}

func newImages() Tab {
	i := &imagesTab{rowsTab: rowsTab{baseTab: baseTab{ident: "images", name: "Images", global: true}}, open: map[string]bool{}, remote: map[string][]core.Remote{}}
	i.build = i.buildRows
	return i
}

func (i *imagesTab) Reload() { i.loaded = false }

func (i *imagesTab) Tick(m *Model) tea.Cmd {
	if i.loaded || i.loading || core.SDK == "" {
		return nil
	}
	i.loading = true
	return m.job("scan images", true, func() cont {
		imgs := core.ScanImages()
		for k := range imgs {
			imgs[k].Size = core.DirSize(filepath.Join(core.SDK, imgs[k].Rel))
		}
		return func(m *Model) tea.Cmd { i.inst, i.loaded, i.loading = imgs, true, false; return nil }
	})
}

func (i *imagesTab) sources() []romSource {
	out := append([]romSource(nil), defaultSources...)
	var extra []romSource
	if b, err := os.ReadFile(filepath.Join(core.ConfDir, "rom-sources.json")); err == nil && json.Unmarshal(b, &extra) == nil {
		for _, s := range extra {
			if s.URL != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func (i *imagesTab) buildRows(m *Model) []row {
	if core.SDK == "" {
		return []row{hdrRow("Android SDK not found — set SDK_ROOT in ~/.config/android-tui/config")}
	}
	rows := []row{hdrRow(fmt.Sprintf("Installed system images (%d)", len(i.inst)))}
	for _, im := range i.inst {
		used := ""
		if len(im.Used) > 0 {
			used = "used by " + strings.Join(im.Used, ", ")
		}
		a, tg, ab, sz := padR(core.APILabel(im.API), 17), padR(core.TagLabel(im.Tag), 28), padR(im.ABI, 10), padL(human(float64(im.Size)), 7)
		plain := a + " " + tg + " " + ab + " " + sz + "  " + used
		if !i.match(plain) {
			continue
		}
		tgs := sDim
		if !im.Official {
			tgs = sMag
		}
		rows = append(rows, row{text: plain, sty: a + " " + tgs.Render(tg) + " " + sDim.Render(ab+" "+sz+"  ") + sOK.Render(used), d: imgRow{kind: "img", img: im}})
	}
	rows = append(rows, hdrRow("Get more"))
	arrow := func(o bool) string { return map[bool]string{true: "▾", false: "▸"}[o] }
	act := func(label string, fn func(m *Model) tea.Cmd) {
		rows = append(rows, row{text: label, sty: sAcc.Render(label), d: imgRow{kind: "act", fn: fn}})
	}
	act(arrow(i.offOpen)+" Official Google images (download via the SDK manager)", i.toggleOfficial)
	if i.offOpen {
		for _, o := range i.official {
			t := fmt.Sprintf("      %s %s %s", padR(core.APILabel(o.API), 17), padR(o.Label, 28), o.Pkg)
			if i.match(t) {
				rows = append(rows, row{text: t, sty: t, d: imgRow{kind: "off", pkg: o.Pkg}})
			}
		}
	}
	for _, s := range i.sources() {
		op := i.open[s.URL]
		label := arrow(op) + " " + s.Name
		rows = append(rows, row{text: label, sty: sAcc.Render(label), d: imgRow{kind: "src", src: s}})
		if op {
			rem := i.remote[s.URL]
			for k := len(rem) - 1; k >= 0 && len(rem)-k <= 25; k-- {
				t := "      " + rem[k].Name
				if i.match(t) {
					rows = append(rows, row{text: t, sty: t, d: imgRow{kind: "remote", rem: rem[k]}})
				}
			}
		}
	}
	act("Import a ROM from a local .zip…", i.importLocal)
	act("Import a ROM from a URL…", i.importURL)
	act("Import an already-extracted image folder…", i.importDir)
	act("Add a ROM download source (directory-listing URL)…", i.addSource)
	act("Guide: custom & open-source ROMs in the emulator", func(m *Model) tea.Cmd { m.viewText("Custom ROMs", strings.Split(romGuide, "\n")); return nil })
	return rows
}

func (i *imagesTab) Hints() []hint {
	return []hint{{"⏎", "open / run"}, {"n", "new AVD"}, {"i", "inspect"}, {"d", "delete"}, {"/", "filter"}, {"r", "rescan"}}
}

func (i *imagesTab) View(m *Model, w, h int, focused bool) []string {
	i.head = func(m *Model) string {
		s := sDim.Render("SDK ") + sAccB.Render(orStr(core.SDK, "not found"))
		if i.loading {
			s += sDim.Render("  " + spinner(m.frame) + " scanning")
		}
		if i.flt != "" {
			s += sWarn.Render("   filter: " + i.flt)
		}
		return s
	}
	return i.viewRows(m, w, h, focused)
}

func (i *imagesTab) Key(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	s := k.String()
	if i.nav(s) {
		return true, nil
	}
	var d *imgRow
	if r := i.cur(); r != nil {
		x := r.d.(imgRow)
		d = &x
	}
	switch s {
	case "/":
		i.askFilter(m)
	case "r":
		i.loaded = false
	case "esc":
		if i.flt != "" {
			i.flt = ""
			return true, nil
		}
		return false, nil
	case "enter":
		if d == nil {
			return true, nil
		}
		switch d.kind {
		case "act":
			return true, d.fn(m)
		case "img":
			i.imageMenu(m, d.img)
		case "off":
			return true, i.installOfficial(m, d.pkg)
		case "src":
			return true, i.toggleSource(m, d.src)
		case "remote":
			i.fetchRemote(m, d.rem.Name, d.rem.URL)
		}
	case "n", "i", "d":
		if d == nil || d.kind != "img" {
			return false, nil
		}
		switch s {
		case "n":
			i.createAVD(m, d.img)
		case "i":
			i.inspect(m, d.img)
		case "d":
			i.delete(m, d.img)
		}
	default:
		return false, nil
	}
	return true, nil
}

func (i *imagesTab) toggleOfficial(m *Model) tea.Cmd {
	if i.offOpen {
		i.offOpen = false
		return nil
	}
	if msg := core.NeedAndroid(); msg != "" {
		m.report(false, msg)
		return nil
	}
	return m.job("official images", false, func() cont {
		offs, ok := core.OfficialImages()
		return func(m *Model) tea.Cmd {
			if !ok {
				m.report(false, "could not list SDK images (offline?)")
				return nil
			}
			i.official, i.offOpen = offs, true
			return nil
		}
	})
}

func (i *imagesTab) installOfficial(m *Model, pkg string) tea.Cmd {
	m.confirm("Install "+pkg+"? (downloads via the SDK manager)", false, func(m *Model) tea.Cmd {
		i.loaded, i.offOpen = false, false
		if core.HaveAndroid {
			return m.ext(true, "android", "sdk", "install", pkg)
		}
		if core.Which("sdkmanager") {
			return m.ext(true, "sdkmanager", "--sdk_root="+core.SDK, "--install", strings.ReplaceAll(pkg, "/", ";"))
		}
		m.report(false, "needs the 'android' CLI or sdkmanager")
		return nil
	})
	return nil
}

func (i *imagesTab) toggleSource(m *Model, s romSource) tea.Cmd {
	if i.open[s.URL] {
		delete(i.open, s.URL)
		return nil
	}
	return m.job("read "+s.URL, false, func() cont {
		zips, err := core.ListRemoteZips(s.URL)
		return func(m *Model) tea.Cmd {
			if err != nil || len(zips) == 0 {
				m.report(false, s.Name+": "+orStr(fmt.Sprint(err), "no .zip files found at that URL"))
				return nil
			}
			i.remote[s.URL], i.open[s.URL] = zips, true
			return nil
		}
	})
}

func (i *imagesTab) fetchRemote(m *Model, name, url string) {
	m.confirm("Download "+name+"?\nIt is large (often 1–3 GB) and is checked against a published .sha256sum when one exists.", false, func(m *Model) tea.Cmd {
		dest := filepath.Join(core.CacheDir, "dl", core.SafeName(name)) // name comes from the server's listing
		core.MkdirP(filepath.Dir(dest))
		m.push(newWork("Download — "+name, true, func(emit core.Emit, cancelled func() bool) any {
			emit("GET "+url, false)
			got, err := core.Download(url, dest, emit, cancelled)
			if err != nil {
				emit("✗ "+err.Error(), false)
				return nil
			}
			if want := core.RemoteSHA256(url); want != "" && want != got {
				_ = os.Remove(dest)
				emit(fmt.Sprintf("✗ sha256 mismatch (expected %s…, got %s…) — file removed", want[:16], got[:16]), false)
				return nil
			} else if want != "" {
				emit("✓ sha256 verified", false)
			} else {
				emit("! no .sha256sum published — integrity not verified", false)
			}
			return dest
		}, func(m *Model, v any) tea.Cmd {
			if p, ok := v.(string); ok {
				return i.runImport(m, p, nil, false, true)
			}
			return nil
		}))
		return nil
	})
}

type importRes struct {
	ok       bool
	msg, rel string
}

func (i *imagesTab) runImport(m *Model, path string, meta *core.ImgMeta, overwrite, cleanup bool) tea.Cmd {
	m.push(newWork("Import — "+filepath.Base(path), true, func(emit core.Emit, _ func() bool) any {
		ok, msg, rel := core.ImportZip(path, emit, meta, overwrite)
		return importRes{ok, msg, rel}
	}, func(m *Model, v any) tea.Cmd {
		r, _ := v.(importRes)
		switch {
		case !r.ok && strings.HasPrefix(r.msg, "can't tell"):
			m.ask("api tag abi (e.g. 36 lineage x86_64)", "36 lineage x86_64", func(m *Model, s string) tea.Cmd {
				if f := strings.Fields(s); len(f) == 3 {
					return i.runImport(m, path, &core.ImgMeta{API: f[0], Tag: f[1], ABI: f[2]}, overwrite, cleanup)
				}
				return nil
			})
			return nil
		case !r.ok && strings.Contains(r.msg, "already exists"):
			m.confirm(r.msg+". Replace it?", true, func(m *Model) tea.Cmd { return i.runImport(m, path, meta, true, cleanup) })
			return nil
		}
		m.report(r.ok, r.msg)
		i.loaded = false
		if r.ok {
			if cleanup {
				_ = os.Remove(path)
			}
			p := strings.Split(r.rel, "/")
			if len(p) >= 4 {
				img := core.Image{Rel: r.rel, API: strings.TrimPrefix(p[1], "android-"), Tag: p[2], ABI: p[3], Official: core.IsOfficialTag(p[2])}
				m.confirm("Create an AVD from this image now?", false, func(m *Model) tea.Cmd { i.createAVD(m, img); return nil })
			}
		}
		return nil
	}))
	return nil
}

func (i *imagesTab) importLocal(m *Model) tea.Cmd {
	m.askPath("ROM / system-image .zip", "", "SDK-layout zip (<abi>/source.properties inside) — AOSP, LineageOS emu_img_zip, /e/OS…", func(m *Model, v string) tea.Cmd {
		if v != "" {
			return i.runImport(m, core.NormPath(v), nil, false, false)
		}
		return nil
	})
	return nil
}

func (i *imagesTab) importURL(m *Model) tea.Cmd {
	m.ask("Direct URL of a system-image .zip", "https://", func(m *Model, u string) tea.Cmd {
		if u == "" || u == "https://" {
			return nil
		}
		name := filepath.Base(strings.SplitN(u, "?", 2)[0])
		i.fetchRemote(m, orStr(name, "image.zip"), u)
		return nil
	})
	return nil
}

func (i *imagesTab) importDir(m *Model) tea.Cmd {
	m.askPath("Folder with source.properties (the <abi> directory)", "", "", func(m *Model, v string) tea.Cmd {
		src := core.NormPath(v)
		m.push(newWork("Import folder", true, func(emit core.Emit, _ func() bool) any {
			ok, msg, _ := core.ImportDir(src, emit, nil)
			emit(msg, false)
			if !ok {
				return nil
			}
			return msg
		}, func(m *Model, v any) tea.Cmd {
			if s, ok := v.(string); ok {
				m.report(true, s)
			}
			i.loaded = false
			return nil
		}))
		return nil
	})
	return nil
}

func (i *imagesTab) addSource(m *Model) tea.Cmd {
	m.ask("Source name", "My ROM builds", func(m *Model, n string) tea.Cmd {
		m.ask("URL of a page listing .zip images (and optional .sha256sum files)", "https://", func(m *Model, u string) tea.Cmd {
			if n == "" || u == "" || u == "https://" {
				return nil
			}
			var extra []romSource
			if b, err := os.ReadFile(filepath.Join(core.ConfDir, "rom-sources.json")); err == nil {
				_ = json.Unmarshal(b, &extra)
			}
			extra = append(extra, romSource{n, u})
			core.MkdirPrivate(core.ConfDir)
			b, _ := json.MarshalIndent(extra, "", "  ")
			_ = core.WritePrivate(filepath.Join(core.ConfDir, "rom-sources.json"), b)
			m.report(true, "added source '"+n+"'")
			return nil
		})
		return nil
	})
	return nil
}

func (i *imagesTab) imageMenu(m *Model, img core.Image) {
	type opt struct {
		t string
		f func()
	}
	opts := []opt{
		{"Create an AVD from this image…", func() { i.createAVD(m, img) }},
		{"Inspect (properties, files, problems)", func() { i.inspect(m, img) }},
		{"Fix / regenerate package.xml", func() {
			core.EnsurePackageXML(filepath.Join(core.SDK, img.Rel), true)
			m.report(true, "package.xml regenerated")
		}},
		{"Open folder", func() {
			if !core.OpenPath(filepath.Join(core.SDK, img.Rel)) {
				m.say("xdg-open missing")
			}
		}},
		{"Delete…", func() { i.delete(m, img) }},
	}
	var names []string
	for _, o := range opts {
		names = append(names, o.t)
	}
	m.choose(fmt.Sprintf("%s · %s · %s", core.APILabel(img.API), core.TagLabel(img.Tag), img.ABI), names, func(m *Model, k int) tea.Cmd { opts[k].f(); return nil })
}

func (i *imagesTab) inspect(m *Model, img core.Image) {
	d := filepath.Join(core.SDK, img.Rel)
	kind := "official"
	if !img.Official {
		kind = "custom ROM"
	}
	lines := []string{"path   " + d, "size   " + human(float64(img.Size)), fmt.Sprintf("tag    %s  (%s)", img.Tag, kind), "", "source.properties"}
	props := core.ReadProps(filepath.Join(d, "source.properties"))
	for _, k := range core.SortedKeys(props) {
		lines = append(lines, "  "+k+" = "+props[k])
	}
	lines = append(lines, "", "Problems")
	if probs := core.ImageProblems(img.Rel); len(probs) > 0 {
		for _, p := range probs {
			lines = append(lines, "  ✗ "+p)
		}
	} else {
		lines = append(lines, "  ✓ none found")
	}
	lines = append(lines, "", "Files")
	ents, _ := os.ReadDir(d)
	for _, e := range ents {
		sz := "<dir>"
		if info, err := e.Info(); err == nil && !e.IsDir() {
			sz = human(float64(info.Size()))
		}
		lines = append(lines, fmt.Sprintf("  %-34s %s", e.Name(), sz))
	}
	m.viewText("Inspect — "+img.Rel, lines)
}

func (i *imagesTab) delete(m *Model, img core.Image) {
	m.confirm(fmt.Sprintf("Delete %s (%s)?", img.Rel, human(float64(img.Size))), true, func(m *Model) tea.Cmd {
		ok, msg := core.DeleteImage(img.Rel)
		m.report(ok, msg)
		i.loaded = false
		return nil
	})
}

func (i *imagesTab) createAVD(m *Model, img core.Image) {
	var names []string
	for _, p := range core.HWProfiles {
		names = append(names, p.Name)
	}
	m.choose("Hardware profile", names, func(m *Model, k int) tea.Cmd {
		def := regexp.MustCompile(`[^A-Za-z0-9._-]`).ReplaceAllString(img.Tag+"_"+img.API, "_")
		m.ask("AVD name", def, func(m *Model, name string) tea.Cmd {
			if name == "" {
				return nil
			}
			ok, msg := core.CreateAVDManual(name, img.Rel, core.HWProfiles[k])
			m.report(ok, msg)
			if !ok {
				return nil
			}
			i.loaded = false
			finish := func(m *Model) tea.Cmd {
				m.confirm("Start it now?", false, func(m *Model) tea.Cmd { return m.startEmu(name, "warm") })
				return m.refreshNow()
			}
			if !img.Official {
				m.confirm("Custom ROM — apply recommended launch flags (-gpu host -feature -Vulkan)?", false, func(m *Model) tea.Cmd {
					core.SetAVDFlags(name, "-gpu host -feature -Vulkan")
					return finish(m)
				})
				return m.refreshNow()
			}
			return finish(m)
		})
		return nil
	})
}

var _ = time.Second
