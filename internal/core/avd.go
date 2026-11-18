package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// ───────────────────────────── SDK discovery ─────────────────────────────

func FindSDK() string {
	cands := []string{Cfg.SDKRoot, os.Getenv("ANDROID_SDK_ROOT"), os.Getenv("ANDROID_HOME")}
	if p, err := exec.LookPath("emulator"); err == nil {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			cands = append(cands, filepath.Dir(filepath.Dir(r)))
		}
	}
	cands = append(cands, filepath.Join(Home, "Android", "Sdk"), "/opt/android-sdk", "/usr/lib/android-sdk")
	for _, c := range cands {
		if c == "" {
			continue
		}
		for _, d := range []string{"emulator", "system-images", "platforms", "platform-tools"} {
			if st, err := os.Stat(filepath.Join(c, d)); err == nil && st.IsDir() {
				return c
			}
		}
	}
	return ""
}

// Bootstrap resolves the SDK, extends PATH and reports whether adb exists.
func Bootstrap() bool {
	SDK = FindSDK()
	if SDK != "" {
		path := os.Getenv("PATH")
		for _, sub := range []string{"emulator", "platform-tools", "cmdline-tools/latest/bin"} {
			p := filepath.Join(SDK, sub)
			if st, err := os.Stat(p); err == nil && st.IsDir() && !strings.Contains(":"+path+":", ":"+p+":") {
				path += ":" + p
			}
		}
		if err := os.Setenv("PATH", path); err != nil {
			Logf("err", "cannot extend PATH with the SDK: %v", err)
		}
	}
	HaveAndroid = Which("android")
	return Which("adb")
}

// ───────────────────────────── AVD data ─────────────────────────────

func AVDDir(name string) string {
	if p := IniGet(filepath.Join(AVDHome, name+".ini"), "path"); p != "" {
		return p
	}
	return filepath.Join(AVDHome, name+".avd")
}

func CfgPath(name string) string { return filepath.Join(AVDDir(name), "config.ini") }

func ShortTag(t string) string {
	switch {
	case strings.HasPrefix(t, "google_apis_playstore"):
		return "play"
	case strings.HasPrefix(t, "google_apis"):
		return "apis"
	case t == "default" || strings.HasPrefix(t, "aosp"):
		return "aosp"
	}
	return t
}

// ImgInfo is what an AVD's config says about its system image.
type ImgInfo struct{ API, Tag, ABI, TagID string }

func (i ImgInfo) String() string {
	if i.API == "?" {
		return "unknown image"
	}
	return fmt.Sprintf("android-%s %s %s", i.API, i.Tag, i.ABI)
}

func (i ImgInfo) Short() string { return i.API + "·" + i.Tag }

func AvdInfo(name string) ImgInfo {
	parts := strings.Split(strings.Trim(IniGet(CfgPath(name), "image.sysdir.1"), "/"), "/")
	if len(parts) < 4 {
		return ImgInfo{"?", "?", "", ""}
	}
	return ImgInfo{strings.TrimPrefix(parts[1], "android-"), ShortTag(parts[2]), parts[3], parts[2]}
}

func ListNames() []string {
	m, _ := filepath.Glob(filepath.Join(AVDHome, "*.ini"))
	var out []string
	for _, f := range m {
		out = append(out, strings.TrimSuffix(filepath.Base(f), ".ini"))
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

func HostABIs() []string {
	switch m := hostMachine(); m {
	case "x86_64":
		return []string{"x86_64", "x86"}
	case "aarch64", "arm64":
		return []string{"arm64-v8a"}
	}
	return []string{"x86_64"}
}

func hostMachine() string {
	r := Run([]string{"uname", "-m"}, Quiet)
	return r.Text()
}

// ───────────────────────────── scanning ─────────────────────────────

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TagDisplay(t string) string {
	if v, ok := OfficialTags[t]; ok {
		return v
	}
	return t
}
