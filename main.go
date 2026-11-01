// android-tui — Android device & emulator optimisation, diagnostics and management: Bubble Tea TUI + scriptable CLI.
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
	"github.com/madushas/android-tui/internal/qr"
	"github.com/madushas/android-tui/internal/ui"
)

const usage = `android-tui %s — Android emulators & adb connections

Usage: android-tui [-y] [command] [args]

  (none)                      interactive TUI (lazygit/btop style)
  ls                          emulators and adb connections, listed separately

  start|stop|restart <avd>    stop also accepts 'all'
  cold|headless|wipe <avd>    cold boot · no window · factory reset
  snaps <avd>                 list snapshots        snap <avd> <name>   boot from one
  save <avd> <name>           save a snapshot of a running AVD
  new                         create-AVD wizard (android CLI)
  rm <avd>                    delete an AVD (asks you to type its name)
  image <avd> <image-path>    change the system image (wipes user data)
  raw <avd>                   open config.ini in $EDITOR
  set <avd> key=value ...     set config.ini keys (scripting)
  rename <avd> <new>          clone <avd> <new> [--with-snapshots]

  images                      installed system images (official + custom ROMs)
  import <zip|url>            install a ROM/system image (AOSP, LineageOS emu_img_zip, /e/OS…)
  mkavd <name> <image> [1-4]  create an AVD from an installed image (profile 1-4)
  flags <avd> [flags...]      show / set per-AVD emulator launch flags

  connect <host[:port]>       adb connect
  pair [host:port code]       pair by code, or with no args: QR code pairing (Android 11+)

  ── optimisation suite ──
  profile <dev>               what android-tui knows about the device (form factor, SoC, API, privilege)
  tweaks <dev>                catalog filtered for this device, with current state
  apply <dev> <id> [on|off|value]   apply a tweak (journalled)       revert <dev> all|<id>
  journal <dev>               every change made, with the value it replaced
  uidump <dev> [--md|--json|--full] [-o file]  current screen's view hierarchy as text
  snapshot <dev> [label]      save settings+device_config        diff <dev> [snapshot.json]  compare with now

  install <dev> <apk|dir> [flags]   uninstall <dev> <pkg>   shot <dev> [file|-o file|--stdout]
  props <dev> [filter]        pkgs <dev> [filter]       push|pull <dev> <src> <dst>
  adb <dev> <adb args...>     shell <dev>      logcat <dev>

  update [check|all|emulator|cli]   prune   doctor   config
  -y, --yes                   assume "yes" to confirmations

<dev> is an AVD name, an adb serial, or any unique case-insensitive substring.`

var assumeYes bool

func main() {
	var args []string
	for _, a := range os.Args[1:] {
		if a == "-y" || a == "--yes" {
			assumeYes = true
		} else {
			args = append(args, a)
		}
	}
	os.Exit(run(args))
}

func ask(prompt string) string {
	fmt.Print(prompt)
	s, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(s)
}

func confirm(msg string) bool {
	if assumeYes {
		return true
	}
	return strings.HasPrefix(strings.ToLower(ask(msg+" [y/N] ")), "y")
}

func fail(f string, a ...any) int { fmt.Fprintf(os.Stderr, "android-tui: "+f+"\n", a...); return 1 }

func done(ok bool, msg string) int {
	if msg != "" {
		fmt.Println(msg)
	}
	if ok {
		return 0
	}
	return 1
}

type dev struct {
	emu  *core.Emu
	conn *core.Conn
}

func (d dev) serial() string {
	if d.emu != nil {
		return d.emu.Serial
	}
	return d.conn.Serial
}
func (d dev) online() bool {
	if d.emu != nil {
		return d.emu.Online()
	}
	return d.conn.Online()
}

func resolve(sc core.Scan, q string) *dev {
	ql := strings.ToLower(q)
	var hits []dev
	for i := range sc.Emus {
		if sc.Emus[i].Name == q {
			return &dev{emu: &sc.Emus[i]}
		}
		if strings.Contains(strings.ToLower(sc.Emus[i].Name), ql) {
			hits = append(hits, dev{emu: &sc.Emus[i]})
		}
	}
	for i := range sc.Conns {
		if sc.Conns[i].Serial == q {
			return &dev{conn: &sc.Conns[i]}
		}
	}
	if len(hits) == 0 {
		for i := range sc.Conns {
			if strings.Contains(strings.ToLower(sc.Conns[i].Serial+" "+sc.Conns[i].Model), ql) {
				hits = append(hits, dev{conn: &sc.Conns[i]})
			}
		}
	}
	if len(hits) == 1 {
		return &hits[0]
	}
	if len(hits) == 0 {
		fail("no AVD or device matching '%s'", q)
	} else {
		fail("'%s' is ambiguous (%d matches)", q, len(hits))
	}
	return nil
}

func printList(sc core.Scan) {
	col := func(state string) string {
		if !isTTY() {
			return ""
		}
		return map[string]string{"running": "\x1b[32m", "device": "\x1b[32m", "booting": "\x1b[33m"}[state] + map[bool]string{true: "", false: ""}[false]
	}
	rs := func() string {
		if isTTY() {
			return "\x1b[0m"
		}
		return ""
	}
	fmt.Println("EMULATORS")
	if len(sc.Emus) == 0 {
		fmt.Println("  no AVDs yet — run: android-tui new")
	}
	for _, e := range sc.Emus {
		dot := map[string]string{"running": "●", "booting": "◐"}[e.State]
		if dot == "" {
			dot = "○"
		}
		c := col(e.State)
		if c == "" && isTTY() {
			c = "\x1b[2m"
		}
		star := ""
		if e.Custom {
			star = " ★"
		}
		fmt.Printf("  %s%s %-28s %-9s %-16s %s%s%s\n", c, dot, e.Name, e.State, orDash(e.Serial), e.Info, star, rs())
	}
	fmt.Println("ADB CONNECTIONS")
	if len(sc.Conns) == 0 {
		fmt.Println("  none — android-tui pair / android-tui connect host:port")
	}
	for _, c := range sc.Conns {
		dot := "◆"
		if c.State == "saved" {
			dot = "○"
		}
		fmt.Printf("  %s %-28s %-12s %-11s %s\n", dot, c.Name(), c.State, c.Transport, c.Serial)
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func isTTY() bool {
	st, err := os.Stdout.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func runExt(argv ...string) int {
	c := exec.Command(argv[0], argv[1:]...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		return 127
	}
	return 0
}

func run(args []string) int {
	core.MigrateLegacy()
	core.LoadConfig()
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "-h", "--help", "help":
		fmt.Printf(usage+"\n", core.Version)
		return 0
	case "__show":
		if len(args) < 2 {
			return 1
		}
		return showImage(args[1])
	case "-V", "--version", "version":
		fmt.Println("android-tui", core.Version)
		return 0
	case "_names":
		fmt.Println(strings.Join(core.ListNames(), "\n"))
		return 0
	case "config":
		core.MkdirP(core.ConfDir)
		if _, err := os.Stat(core.ConfFile); err != nil {
			_ = os.WriteFile(core.ConfFile, []byte(core.ConfigTemplate), 0o644)
		}
		ed := core.PickEditor()
		if ed == "" {
			return fail("no editor found — set $EDITOR")
		}
		return runExt(append(core.ShellSplit(ed), core.ConfFile)...)
	}
	if !core.Bootstrap() {
		if cmd == "doctor" {
			return doctor()
		}
		return fail("'adb' not found.\n     " + core.AdbInstallHint + "\n     Run 'android-tui doctor' for details.")
	}
	switch cmd {
	case "":
		return runTUI()
	case "doctor":
		return doctor()
	case "images":
		imgs := core.ScanImages()
		for _, i := range imgs {
			used := ""
			if len(i.Used) > 0 {
				used = "used by " + strings.Join(i.Used, ", ")
			}
			fmt.Printf("%-62s %-26s %7s  %s\n", i.Rel, core.TagLabel(i.Tag), core.Human(float64(core.DirSize(filepath.Join(core.SDK, i.Rel)))), used)
		}
		if len(imgs) == 0 {
			return fail("no system images found (SDK_ROOT set?)")
		}
		return 0
	case "import":
		return cliImport(args[1:])
	case "mkavd":
		if len(args) < 3 {
			return fail("usage: android-tui mkavd <name> <system-images/android-N/tag/abi> [profile 1-4]")
		}
		p := core.HWProfiles[0]
		if len(args) > 3 {
			if n := int(args[3][0] - '0'); n >= 1 && n <= len(core.HWProfiles) {
				p = core.HWProfiles[n-1]
			}
		}
		return done(core.CreateAVDManual(args[1], strings.TrimRight(args[2], "/"), p))
	case "flags":
		if len(args) < 2 {
			return fail("usage: android-tui flags <avd> [flags...]")
		}
		if len(args) == 2 {
			fmt.Println(orDash(core.Cfg.AVDFlags[args[1]]))
			return 0
		}
		return done(core.SetAVDFlags(args[1], strings.Join(args[2:], " ")))
	case "connect":
		if len(args) < 2 {
			return fail("usage: android-tui connect host[:port]")
		}
		return done(core.AdbConnect(args[1]))
	case "pair":
		if len(args) >= 3 {
			return done(core.AdbPair(args[1], args[2]))
		}
		return cliQR()
	case "new", "create":
		if msg := core.NeedAndroid(); msg != "" {
			return fail("%s", msg)
		}
		return runExt("android", "emulator", "create")
	case "prune", "update":
		if msg := core.NeedAndroid(); msg != "" {
			return fail("%s", msg)
		}
		return cliUpdate(args[1:], cmd)
	}
	sc := core.Scn.Scan()
	if cmd == "ls" || cmd == "list" {
		printList(sc)
		return 0
	}
	if cmd == "stop" && len(args) > 1 && args[1] == "all" {
		return done(core.StopAll())
	}
	verbs := map[string]bool{"start": true, "stop": true, "restart": true, "cold": true, "headless": true, "wipe": true, "snaps": true, "snap": true, "save": true,
		"rm": true, "delete": true, "image": true, "raw": true, "set": true, "rename": true, "clone": true, "shot": true, "shell": true, "logcat": true,
		"profile": true, "tweaks": true, "apply": true, "revert": true, "journal": true, "uidump": true, "snapshot": true, "diff": true,
		"install": true, "uninstall": true, "props": true, "pkgs": true, "push": true, "pull": true, "adb": true}
	if !verbs[cmd] {
		// legacy: `emu Pixel_9 [emulator flags]`
		d := resolve(sc, cmd)
		if d == nil || d.emu == nil {
			return 1
		}
		if d.emu.Active() {
			return done(true, d.emu.Name+" is already "+d.emu.State)
		}
		return done(core.DoStart(d.emu.Name, args[1:]...))
	}
	if len(args) < 2 {
		return fail("%s needs an AVD or device name", cmd)
	}
	d := resolve(sc, args[1])
	if d == nil {
		return 1
	}
	rest := args[2:]
	avdOnly := map[string]bool{"start": true, "restart": true, "cold": true, "headless": true, "wipe": true, "snaps": true, "snap": true, "save": true, "rm": true,
		"delete": true, "image": true, "raw": true, "set": true, "rename": true, "clone": true}
	if avdOnly[cmd] && d.emu == nil {
		return fail("%s only works on emulators (AVDs)", cmd)
	}
	if !avdOnly[cmd] && cmd != "stop" && !d.online() {
		return fail("%s is not online", args[1])
	}
	s := d.serial()
	switch cmd {
	case "start":
		if d.emu.Active() {
			return done(true, d.emu.Name+" is already "+d.emu.State)
		}
		return done(core.StartMode(d.emu.Name, "warm", ""))
	case "stop":
		if d.emu == nil {
			return fail("stop only works on emulators")
		}
		return done(core.DoStop(*d.emu))
	case "restart":
		return done(core.StartMode(d.emu.Name, "warm", ""))
	case "cold", "headless", "wipe":
		if cmd == "wipe" && !confirm("Factory-reset "+d.emu.Name+"? All data on it will be erased.") {
			return 1
		}
		return done(core.StartMode(d.emu.Name, cmd, ""))
	case "snaps":
		for _, n := range core.SnapList(d.emu.Name) {
			fmt.Println(n)
		}
		return 0
	case "snap":
		if len(rest) == 0 {
			return fail("snap needs a snapshot name")
		}
		return done(core.StartMode(d.emu.Name, "snap", rest[0]))
	case "save":
		if len(rest) == 0 {
			return fail("save needs a snapshot name")
		}
		return done(core.SaveSnapshot(*d.emu, rest[0]))
	case "rm", "delete":
		if core.ActiveNow(d.emu.Name) {
			core.DoStop(*d.emu)
			core.WaitGone(d.emu.Name)
		}
		if !assumeYes && ask("Type the AVD name ("+d.emu.Name+") to confirm: ") != d.emu.Name {
			return fail("cancelled — nothing was deleted")
		}
		return done(core.DeleteAVD(d.emu.Name))
	case "image":
		if len(rest) == 0 {
			return fail("usage: android-tui image <avd> <system-images/android-N/tag/abi>   (see: android-tui images)")
		}
		if core.ActiveNow(d.emu.Name) {
			return fail("%s is running — stop it first", d.emu.Name)
		}
		if !confirm("This ERASES the user data of " + d.emu.Name + ". Continue?") {
			return 1
		}
		return done(core.SetImage(d.emu.Name, rest[0]))
	case "raw":
		ed := core.PickEditor()
		if ed == "" {
			return fail("no editor found — set $EDITOR")
		}
		c := core.CfgPath(d.emu.Name)
		_ = core.CopyFile(c, c+".emu-bak")
		return runExt(append(core.ShellSplit(ed), c)...)
	case "set":
		if core.ActiveNow(d.emu.Name) {
			return fail("%s is running — stop it first", d.emu.Name)
		}
		for _, kv := range rest {
			k, v, ok := strings.Cut(kv, "=")
			if !ok {
				return fail("expected key=value, got '%s'", kv)
			}
			core.ApplyHW(d.emu.Name, k, v)
			fmt.Printf("  %s=%s\n", k, v)
		}
		return 0
	case "rename":
		if len(rest) == 0 {
			return fail("rename needs a new name")
		}
		return done(core.RenameAVD(d.emu.Name, rest[0]))
	case "clone":
		if len(rest) == 0 {
			return fail("clone needs a new name")
		}
		with := false
		for _, r := range rest[1:] {
			with = with || r == "--with-snapshots"
		}
		return done(core.CloneAVD(d.emu.Name, rest[0], with))
	case "profile", "tweaks", "apply", "revert", "journal", "uidump", "snapshot", "diff":
		return cliSuite(cmd, s, rest)
	case "shell":
		return runExt("adb", "-s", s, "shell")
	case "logcat":
		return runExt("adb", "-s", s, "logcat")
	case "shot":
		f := filepath.Join(core.Cfg.ShotDir, fmt.Sprintf("%s-%s.png", args[1], time.Now().Format("20060102-150405")))
		toStdout := false
		for i := 0; i < len(rest); i++ {
			switch rest[i] {
			case "-", "--stdout":
				toStdout = true // raw PNG on stdout: android-tui shot <dev> --stdout > x.png
			case "-o":
				if i+1 < len(rest) {
					f = rest[i+1]
					i++
				}
			default:
				f = rest[i]
			}
		}
		b, err := core.Binary([]string{"adb", "-s", s, "exec-out", "screencap", "-p"}, 30*time.Second)
		if err != nil || len(b) < 100 {
			return fail("screenshot failed: %v", err)
		}
		if toStdout {
			_, _ = os.Stdout.Write(b)
			return 0
		}
		core.MkdirP(filepath.Dir(f))
		if err := os.WriteFile(f, b, 0o644); err != nil {
			return fail("%v", err)
		}
		fmt.Println("saved", f)
		return 0
	case "install":
		if len(rest) == 0 {
			return fail("install needs an APK path")
		}
		p := core.NormPath(rest[0])
		files := []string{p}
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			files, _ = filepath.Glob(filepath.Join(p, "*.apk"))
		}
		if len(files) == 0 {
			return fail("no APK at %s", p)
		}
		flags := rest[1:]
		if len(flags) == 0 {
			flags = []string{"-r"}
		}
		verb := "install"
		if len(files) > 1 {
			verb = "install-multiple"
		}
		return runExt(append(append([]string{"adb", "-s", s, verb}, flags...), files...)...)
	case "uninstall":
		if len(rest) == 0 {
			return fail("uninstall needs a package")
		}
		return runExt(append([]string{"adb", "-s", s, "uninstall"}, rest...)...)
	case "props":
		for _, kv := range core.ParseProps(core.Sh(s, "getprop", core.Quiet).Out) {
			if len(rest) == 0 || strings.Contains(strings.ToLower(kv.K+"="+kv.V), strings.ToLower(rest[0])) {
				fmt.Printf("%s=%s\n", kv.K, kv.V)
			}
		}
		return 0
	case "pkgs":
		for _, p := range core.ParsePM(core.Sh(s, core.PMScript, core.Opt{Quiet: true, Timeout: 40 * time.Second}).Out) {
			if !p.System && !p.Removed && (len(rest) == 0 || strings.Contains(strings.ToLower(p.Name), strings.ToLower(rest[0]))) {
				fmt.Println(p.Name)
			}
		}
		return 0
	case "push", "pull":
		if len(rest) != 2 {
			return fail("usage: android-tui %s <dev> <src> <dst>", cmd)
		}
		return runExt("adb", "-s", s, cmd, rest[0], rest[1])
	case "adb":
		return runExt(append([]string{"adb", "-s", s}, rest...)...)
	}
	return 0
}

func doctor() int {
	sym := map[string]string{"ok": "\x1b[32m✓\x1b[0m", "warn": "\x1b[33m!\x1b[0m", "bad": "\x1b[31m✗\x1b[0m"}
	for _, l := range core.Doctor() {
		switch l.Kind {
		case "h":
			fmt.Printf("\n\x1b[1m%s\x1b[0m\n", l.Text)
		case "txt":
			fmt.Println(l.Text)
		default:
			fmt.Printf("  %s %s\n", sym[l.Kind], l.Text)
		}
	}
	return 0
}

func cliImport(args []string) int {
	if len(args) < 1 {
		return fail("usage: android-tui import <zip|url>")
	}
	emit := func(t string, rep bool) {
		if rep {
			fmt.Printf("\r%-70s", t)
		} else {
			fmt.Println(t)
		}
	}
	src := args[0]
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		dest := filepath.Join(core.CacheDir, "dl", filepath.Base(strings.SplitN(src, "?", 2)[0]))
		core.MkdirP(filepath.Dir(dest))
		got, err := core.Download(src, dest, emit, func() bool { return false })
		if err != nil {
			return fail("%v", err)
		}
		want := core.RemoteSHA256(src)
		switch {
		case want == "":
			fmt.Println("\nno .sha256sum published — not verified")
		case want != got:
			return fail("sha256 mismatch — file kept at %s but NOT imported", dest)
		default:
			fmt.Println("\nsha256 verified")
		}
		src = dest
	}
	ok, msg, _ := core.ImportZip(core.NormPath(src), emit, nil, assumeYes)
	return done(ok, msg)
}

func cliQR() int {
	q, err := core.NewQRPair()
	if err != nil {
		return fail("%v", err)
	}
	mat, err := qr.Encode(q.Payload)
	if err != nil {
		return fail("%v", err)
	}
	fmt.Print("On the phone: Settings → Developer options → Wireless debugging → Pair device with QR code, then scan:\n\n")
	for _, l := range qr.Lines(mat, 2) {
		fmt.Printf("\x1b[30;47m%s\x1b[0m\n", l)
	}
	go q.Run()
	shown := 0
	for {
		time.Sleep(500 * time.Millisecond)
		lines, fin, ok := q.State()
		for ; shown < len(lines); shown++ {
			fmt.Println("  " + lines[shown])
		}
		if fin {
			return map[bool]int{true: 0, false: 1}[ok]
		}
	}
}

func cliUpdate(args []string, cmd string) int {
	if cmd == "prune" {
		var unused []core.Image
		for _, i := range core.ScanImages() {
			if len(i.Used) == 0 {
				unused = append(unused, i)
			}
		}
		if len(unused) == 0 {
			fmt.Println("no unused system images")
			return 0
		}
		for _, i := range unused {
			if confirm("Remove " + i.Rel + "?") {
				runExt("android", "sdk", "remove", i.Rel)
			}
		}
		return 0
	}
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "check", "":
		u, ok := core.SDKUpdates()
		if !ok {
			return fail("could not check for updates (offline?)")
		}
		core.SaveUpdates(u)
		for _, x := range u {
			fmt.Printf("  ⬆ %-46s %s → %s\n", x.Pkg, x.Old, x.New)
		}
		if len(u) == 0 {
			fmt.Println("  ✓ all SDK packages are up to date")
		}
		return 0
	case "all":
		return runExt("android", "sdk", "update")
	case "emulator":
		return runExt("android", "sdk", "update", "emulator")
	case "cli":
		return runExt("android", "update")
	}
	return fail("usage: android-tui update [check|all|emulator|cli]")
}

func runTUI() int {
	if !isTTY() || fi(os.Stdin) {
		sc := core.Scn.Scan()
		printList(sc)
		return 0
	}
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if core.Cfg.Mouse {
		opts = append(opts, tea.WithMouseCellMotion())
	}
	if _, err := tea.NewProgram(ui.New(), opts...).Run(); err != nil {
		return fail("%v", err)
	}
	return 0
}

func fi(f *os.File) bool {
	st, err := f.Stat()
	return err != nil || st.Mode()&os.ModeCharDevice == 0
}

func termSize() (int, int) {
	c := exec.Command("stty", "size")
	c.Stdin = os.Stdin
	out, err := c.Output()
	var r, cl int
	if err == nil {
		_, _ = fmt.Sscanf(string(out), "%d %d", &r, &cl) // zero values fall through to the default size below
	}
	if r < 5 || cl < 10 {
		return 100, 30
	}
	return cl, r
}

// showImage prints a PNG with the best protocol the terminal supports, then waits for Enter.
func showImage(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return fail("%v", err)
	}
	cols, rows := termSize()
	rows -= 3
	switch core.ImageProtocol() {
	case "kitty":
		fmt.Print("\x1b[2J\x1b[H" + core.KittySeq(b, cols, rows))
	case "chafa":
		runExt("chafa", "-s", fmt.Sprintf("%dx%d", cols, rows), path)
	case "sixel":
		runExt("img2sixel", "-w", fmt.Sprint(cols*8), path)
	default:
		lines, err := core.RenderHalfBlocks(b, cols, rows)
		if err != nil {
			return fail("%v", err)
		}
		fmt.Println(strings.Join(lines, "\n"))
	}
	fmt.Print("\n\n  " + filepath.Base(path) + "  —  press Enter to return… ")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n') // any key (or EOF) returns
	return 0
}

// cliSuite implements the scriptable side of the optimisation suite.
func cliSuite(cmd, serial string, rest []string) int {
	p := core.ProbeDevice(serial)
	switch cmd {
	case "profile":
		fmt.Printf("serial        %s\nmodel         %s (%s / %s)\nandroid       %s (API %d)\nform factor   %s\nsoc           %s  [platform=%s soc=%s hw=%s]\nbrands        %s\nprivilege     %s\npackages      %d installed, %d disabled\n",
			p.Serial, p.Model, p.Brand, p.Manufacturer, p.Release, p.SDK, p.Form, p.Vendor, p.Platform, p.SoC, p.Hardware, strings.Join(p.Brands, ","), p.Privilege(), len(p.Installed), len(p.Disabled))
		return 0
	case "tweaks":
		cat, deb, _ := core.Catalog(p)
		fmt.Printf("%s\n\n", p.Summary())
		for _, c := range cat {
			if !c.OK {
				fmt.Printf("  ✗ %-24s %-46s %s\n", c.ID, c.Title, c.Reason)
				continue
			}
			st, cur := core.TweakState(p, c.Tweak)
			fmt.Printf("  %s %-24s %-46s %-8s %s\n", stateMark(st), c.ID, c.Title, c.Risk, map[bool]string{true: "now: " + cur}[st == "preset"])
		}
		fmt.Printf("\n%d known bloat package(s) installed:\n", len(deb))
		for _, d := range deb {
			fmt.Printf("  %-44s %-8s %s\n", d.Pkg, d.Risk, d.Label)
		}
		return 0
	case "apply":
		if len(rest) == 0 {
			return fail("usage: android-tui apply <dev> <tweak-id> [on|off|value]")
		}
		cat, _, _ := core.Catalog(p)
		for _, c := range cat {
			if c.ID != rest[0] {
				continue
			}
			if !c.OK {
				return fail("%s is not available here: %s", c.ID, c.Reason)
			}
			mode := "on"
			if len(rest) > 1 {
				mode = rest[1]
			}
			var es []core.JEntry
			if c.Special == "prop_glob" {
				for _, pc := range core.GlobPlan(serial, strings.Split(c.Cmd, ",")) {
					es = append(es, core.ApplyOp(p, "cli", c.Title, core.Op{Kind: "prop", Key: pc.Key}, pc.New, false))
				}
			} else {
				es = core.ApplyTweak(p, c.Tweak, mode)
			}
			bad := 0
			for _, e := range es {
				flag := "✓"
				switch {
				case e.Err != "":
					flag, bad = "✗ "+e.Err, bad+1
				case !e.Verified:
					flag, bad = "! did not stick", bad+1
				}
				fmt.Printf("  %s  %s\n", e.Describe(), flag)
			}
			if c.Reboot {
				fmt.Println("  (reboot required)")
			}
			return map[bool]int{true: 0, false: 1}[bad == 0]
		}
		return fail("unknown tweak '%s' (see: android-tui tweaks <dev>)", rest[0])
	case "revert":
		if len(rest) == 0 || rest[0] == "all" {
			if !confirm("Revert ALL journalled changes on this device?") {
				return 1
			}
			d, f, msgs := core.RevertAll(serial)
			fmt.Printf("reverted %d, failed %d %s\n", d, f, strings.Join(msgs, "; "))
			return map[bool]int{true: 0, false: 1}[f == 0]
		}
		cat, _, _ := core.Catalog(p)
		for _, c := range cat {
			if c.ID == rest[0] {
				n, msg := core.RevertTweak(serial, c.Title)
				return done(n > 0, msg)
			}
		}
		return fail("unknown tweak '%s'", rest[0])
	case "journal":
		for _, e := range core.LoadJournal() {
			if e.Serial != serial {
				continue
			}
			st := "applied "
			if e.Reverted {
				st = "reverted"
			} else if e.Irrev {
				st = "one-shot"
			} else if !e.Verified {
				st = "unstuck "
			}
			fmt.Printf("%s  %s  %-30s %s\n", e.TS.Format("2006-01-02 15:04:05"), st, e.Title, e.Describe())
		}
		return 0
	case "uidump":
		n, err := core.UIDump(serial)
		if err != nil {
			return fail("%v", err)
		}
		mode, outFile := "", ""
		for i := 0; i < len(rest); i++ {
			switch rest[i] {
			case "-o":
				if i+1 < len(rest) {
					outFile = rest[i+1]
					i++
				}
			case "--md", "--json", "--full":
				mode = rest[i]
			}
		}
		var text string
		switch mode {
		case "--md":
			text = n.RenderMarkdown(true)
		case "--json":
			text = n.RenderJSON(true) + "\n"
		case "--full":
			text = strings.Join(n.RenderTree(false, true), "\n") + "\n"
		default:
			text = strings.Join(n.RenderTree(true, false), "\n") + "\n"
		}
		if outFile != "" {
			core.MkdirP(filepath.Dir(outFile))
			if err := os.WriteFile(outFile, []byte(text), 0o644); err != nil {
				return fail("%v", err)
			}
			fmt.Println("saved", outFile)
			return 0
		}
		fmt.Print(text)
		return 0
	case "snapshot":
		label := "cli"
		if len(rest) > 0 {
			label = rest[0]
		}
		s, err := core.TakeSnapshot(p, label, len(rest) > 1 && rest[1] == "--props")
		if err != nil {
			return fail("%v", err)
		}
		fmt.Printf("saved %d keys to %s\n", len(s.Data), s.File)
		return 0
	case "diff":
		snaps := core.ListSnapshots(serial)
		if len(snaps) == 0 {
			return fail("no snapshots yet — run: android-tui snapshot <dev>")
		}
		a := snaps[0]
		withProps := false
		for k := range a.Data {
			withProps = withProps || strings.HasPrefix(k, "prop/")
		}
		now := core.CurrentSnapshot(p, withProps)
		ds := core.DiffSnaps(a, now)
		fmt.Printf("%s (%s) → now: %d difference(s)\n", a.Label, a.TS.Format("15:04:05"), len(ds))
		for _, x := range ds {
			switch x.Op {
			case "+":
				fmt.Printf("+ %s = %s\n", x.Key, x.New)
			case "-":
				fmt.Printf("- %s = %s\n", x.Key, x.Old)
			default:
				fmt.Printf("~ %s: %s → %s\n", x.Key, x.Old, x.New)
			}
		}
		return 0
	}
	return 0
}

func stateMark(s string) string {
	return map[string]string{"on": "●", "off": "○", "partial": "◐", "action": "▶", "preset": "◆", "unknown": "?"}[s]
}
