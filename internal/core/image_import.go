package core

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

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
	prefix, api, tag, abi := readZipImageID(zr, meta)
	if api == "" || tag == "" || abi == "" {
		return false, fmt.Sprintf("can't tell API/tag/ABI from the zip (found: api=%s tag=%s abi=%s)", orQ(api), orQ(tag), orQ(abi)), ""
	}
	safe := regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	if !safe.MatchString(tag) || !safe.MatchString(api) || !safe.MatchString(abi) {
		return false, "unsafe characters in API/tag/ABI", ""
	}
	warnIfForeignABI(abi, emit)
	dest := filepath.Join(SDK, "system-images", "android-"+api, tag, abi)
	rel := filepath.ToSlash(filepath.Join("system-images", "android-"+api, tag, abi))
	members, need := zipMembers(zr, prefix)
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
	if err := extractZipMembers(members, prefix, dest, need, emit); err != nil {
		pruneEmpty(dest)
		return false, err.Error(), ""
	}
	finishImageDir(dest, api, tag, abi, emit)
	return true, "installed " + rel, rel
}

// readZipImageID finds the image's source.properties (the shallowest one) and returns the zip's path prefix plus the
// API level, tag and ABI, with explicit metadata taking precedence and the ABI falling back to the folder name.
func readZipImageID(zr *zip.ReadCloser, meta *ImgMeta) (prefix, api, tag, abi string) {
	var sp *zip.File
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "source.properties") && (sp == nil || strings.Count(f.Name, "/") < strings.Count(sp.Name, "/")) {
			sp = f
		}
	}
	props := map[string]string{}
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
	api, tag, abi = props["AndroidVersion.ApiLevel"], props["SystemImage.TagId"], props["SystemImage.Abi"]
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
	return prefix, api, tag, abi
}

func warnIfForeignABI(abi string, emit Emit) {
	for _, a := range HostABIs() {
		if a == abi {
			return
		}
	}
	emit(fmt.Sprintf("  ! warning: ABI %s doesn't match this host (%s) — it will be slow or won't boot", abi, strings.Join(HostABIs(), ", ")), false)
}

// zipMembers lists the files under the image prefix and their total uncompressed size.
func zipMembers(zr *zip.ReadCloser, prefix string) (members []*zip.File, need uint64) {
	for _, f := range zr.File {
		if !f.FileInfo().IsDir() && strings.HasPrefix(f.Name, prefix) && f.Name != prefix {
			members = append(members, f)
			need += f.UncompressedSize64
		}
	}
	return members, need
}

// extractZipMembers writes the members under dest, refusing any path that would escape it (zip-slip) and checking
// every write and close so a full disk can never leave a silently truncated file.
func extractZipMembers(members []*zip.File, prefix, dest string, need uint64, emit Emit) error {
	base, _ := filepath.Abs(dest)
	var done uint64
	for _, f := range members {
		target := filepath.Join(base, filepath.FromSlash(f.Name[len(prefix):]))
		if !strings.HasPrefix(target, base+string(filepath.Separator)) {
			return errors.New("unsafe path in zip: " + f.Name)
		}
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			_ = rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		if cerr := out.Close(); err == nil {
			err = cerr // a failed close can hide a short write
		}
		_ = rc.Close()
		if err != nil {
			return errors.New("extract failed: " + err.Error())
		}
		done += f.UncompressedSize64
		emit(fmt.Sprintf("  %5.1f%%  %s", 100*float64(done)/float64(max64(1, need)), f.Name[len(prefix):]), true)
	}
	return nil
}

// finishImageDir makes sure the extracted image has the metadata the SDK tools expect.
func finishImageDir(dest, api, tag, abi string, emit Emit) {
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
