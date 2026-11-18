package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/madushas/android-tui/internal/core"
	"github.com/madushas/android-tui/internal/qr"
)

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
		dest := filepath.Join(core.CacheDir, "dl", core.SafeName(filepath.Base(strings.SplitN(src, "?", 2)[0])))
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
