package core

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellSplit(t *testing.T) {
	got := ShellSplit(`-no-metrics "-gpu host" 'a b' c\ d # comment`)
	want := []string{"-no-metrics", "-gpu host", "a b", "c d"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	ConfFile = filepath.Join(dir, "config")
	if err := os.WriteFile(ConfFile, []byte("EMU_FLAGS=(-no-metrics -no-boot-anim)\nDEFAULT_GPU=\"host\"\nAVD_FLAGS[Pixel_9]=\"-memory 4096\"\nUPDATE_CHECK_HOURS=6\n# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	LoadConfig()
	if len(Cfg.EmuFlags) != 2 || Cfg.DefaultGPU != "host" || Cfg.AVDFlags["Pixel_9"] != "-memory 4096" || Cfg.UpdateHours != 6 {
		t.Fatalf("bad config: %+v", Cfg)
	}
}

func TestParseDevicesAndMDNS(t *testing.T) {
	d := ParseDevicesL("List of devices attached\n192.0.2.5:5555 device product:x model:Pixel_7 device:p transport_id:3\nadb-ABC._adb-tls-connect._tcp device model:Pix transport_id:4\nTEST-DEVICE-001 unauthorized usb:1-1 transport_id:5\nemulator-5554 device model:sdk transport_id:1\n")
	tr := []string{}
	for _, x := range d {
		tr = append(tr, x.Transport)
	}
	if strings.Join(tr, ",") != "wifi,wifi (tls),usb,emulator" {
		t.Fatalf("transports: %v", tr)
	}
	m := ParseMDNS("adb-X-y\t_adb-tls-pairing._tcp.\t192.0.2.5:37001\nfoo\t_adb-tls-connect._tcp\t192.0.2.6:4444\n")
	if len(m) != 2 || m[0].Kind != "pair" || m[1].Kind != "connect" || m[0].Addr != "192.0.2.5:37001" {
		t.Fatalf("mdns: %+v", m)
	}
}

func TestParsePM(t *testing.T) {
	out := "package:/data/app/~~x==/com.a-1==/base.apk=com.a  installer=com.android.vending  versionCode:12 uid:10123\n" +
		"package:/system/app/S.apk=com.sys  installer=null  versionCode:3 uid:1000\n@@sys\npackage:com.sys\n@@dis\npackage:com.a\n@@all\npackage:com.a\npackage:com.sys\npackage:com.gone\n"
	p := ParsePM(out)
	if len(p) != 3 || p[0].Name != "com.a" || p[0].Installer != "com.android.vending" || p[0].VC != 12 || !p[0].Disabled || !p[1].System || !p[2].Removed {
		t.Fatalf("pm: %+v", p)
	}
	if !strings.HasSuffix(p[0].Path, "base.apk") || strings.Contains(p[0].Path, "installer") {
		t.Fatalf("path: %q", p[0].Path)
	}
}

func TestParsePermsLsProcs(t *testing.T) {
	pm := ParsePerms("    requested permissions:\n      android.permission.CAMERA\n    runtime permissions:\n      android.permission.CAMERA: granted=true, flags=[ ]\n      android.permission.X: granted=false\n")
	if pm["android.permission.CAMERA"].Kind != "runtime" || !*pm["android.permission.CAMERA"].Granted || *pm["android.permission.X"].Granted {
		t.Fatalf("perms: %+v", pm)
	}
	ls := ParseLs("total 4\ndrwxr-xr-x 2 u0_a1 media_rw 4096 2026-08-24 10:11 Download\n-rw-r--r-- 1 u0_a1 media_rw 12 2026-08-24 10:11 a b.txt\nlrwxrwxrwx 1 root root 21 2026-08-24 10:11 sdcard -> /storage/self/primary\n")
	if len(ls) != 3 || !ls[0].Dir || ls[2].Name != "a b.txt" || ls[1].Link != "/storage/self/primary" {
		t.Fatalf("ls: %+v", ls)
	}
	pr := ParseProcs("   PID USER RSS NAME\n 1 root 100 init\n 2 u0_a1 900 com.x\n")
	if len(pr) != 2 || pr[0].Name != "com.x" {
		t.Fatalf("procs: %+v", pr)
	}
}

func TestParseStats(t *testing.T) {
	s := ParseStats("@@stat\ncpu  100 0 50 800 50 0 0 0 0 0\n@@mem\nMemTotal: 4000 kB\nMemAvailable: 1000 kB\n@@net\nInter-|\n face |\n lo: 10 1 0 0 0 0 0 0 10 1\n eth0: 1000 5 0 0 0 0 0 0 500 3\n@@bat\n AC powered: true\n status: 2\n level: 55\n temperature: 301\n")
	if !s.HasCPU || s.CPUTot != 1000 || s.MemUsed != 3000*1024 || s.RX != 1000 || s.TX != 500 || s.Bat == nil || s.Bat.Level != 55 || !s.Bat.AC {
		t.Fatalf("stats: %+v", s)
	}
}

func makeZip(t *testing.T, files map[string]string) string {
	p := filepath.Join(t.TempDir(), "img.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for n, c := range files {
		w, err := z.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(c)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestImportZip(t *testing.T) {
	SDK = t.TempDir()
	noop := func(string, bool) {}
	z := makeZip(t, map[string]string{
		"x86_64/source.properties": "AndroidVersion.ApiLevel=36\nSystemImage.Abi=x86_64\nSystemImage.TagId=lineage\n",
		"x86_64/system.img":        "data", "x86_64/ramdisk.img": "r", "x86_64/kernel-ranchu": "k"})
	ok, msg, rel := ImportZip(z, noop, nil, false)
	if !ok || rel != "system-images/android-36/lineage/x86_64" {
		t.Fatalf("import: %v %s %s", ok, msg, rel)
	}
	if !exists(filepath.Join(SDK, rel, "package.xml")) {
		t.Fatal("package.xml not written")
	}
	if ok, msg, _ := ImportZip(z, noop, nil, false); ok || !strings.Contains(msg, "already exists") {
		t.Fatalf("second import should refuse: %v %s", ok, msg)
	}
	if probs := ImageProblems(rel); len(probs) != 0 {
		t.Fatalf("unexpected problems: %v", probs)
	}
	evil := makeZip(t, map[string]string{"x86_64/source.properties": "AndroidVersion.ApiLevel=35\nSystemImage.Abi=x86_64\nSystemImage.TagId=evil\n",
		"x86_64/../../../../../PWNED.txt": "x"})
	if ok, msg, _ := ImportZip(evil, noop, nil, false); ok || !strings.Contains(msg, "unsafe path") {
		t.Fatalf("zip-slip not blocked: %v %s", ok, msg)
	}
	if exists(filepath.Join(SDK, "system-images", "android-35")) {
		t.Fatal("partial install left behind")
	}
}
