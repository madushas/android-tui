package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/madushas/android-tui/internal/core"
)

// cliSuite implements the scriptable side of the optimisation suite.
func cliSuite(cmd, serial string, rest []string) int {
	p := core.ProbeDevice(serial)
	switch cmd {
	case "profile":
		return cliProfile(p, serial, rest)
	case "tweaks":
		return cliTweaks(p, serial, rest)
	case "apply":
		return cliApply(p, serial, rest)
	case "revert":
		return cliRevert(p, serial, rest)
	case "journal":
		return cliJournal(p, serial, rest)
	case "uidump":
		return cliUidump(p, serial, rest)
	case "snapshot":
		return cliSnapshot(p, serial, rest)
	case "diff":
		return cliDiff(p, serial, rest)
	}
	return 0
}

// cliProfile implements `android-tui profile <dev>`.
func cliProfile(p *core.Profile, serial string, rest []string) int {
	fmt.Printf("serial        %s\nmodel         %s (%s / %s)\nandroid       %s (API %d)\nform factor   %s\nsoc           %s  [platform=%s soc=%s hw=%s]\nbrands        %s\nprivilege     %s\npackages      %d installed, %d disabled\n",
		p.Serial, p.Model, p.Brand, p.Manufacturer, p.Release, p.SDK, p.Form, p.Vendor, p.Platform, p.SoC, p.Hardware, strings.Join(p.Brands, ","), p.Privilege(), len(p.Installed), len(p.Disabled))
	return 0
}

// cliTweaks implements `android-tui tweaks <dev>`.
func cliTweaks(p *core.Profile, serial string, rest []string) int {
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
}

// cliApply implements `android-tui apply <dev>`.
func cliApply(p *core.Profile, serial string, rest []string) int {
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
}

// cliRevert implements `android-tui revert <dev>`.
func cliRevert(p *core.Profile, serial string, rest []string) int {
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
}

// cliJournal implements `android-tui journal <dev>`.
func cliJournal(p *core.Profile, serial string, rest []string) int {
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
}

// cliUidump implements `android-tui uidump <dev>`.
func cliUidump(p *core.Profile, serial string, rest []string) int {
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
		if err := core.WritePrivate(outFile, []byte(text)); err != nil {
			return fail("%v", err)
		}
		fmt.Println("saved", outFile)
		return 0
	}
	fmt.Print(text)
	return 0
}

// cliSnapshot implements `android-tui snapshot <dev>`.
func cliSnapshot(p *core.Profile, serial string, rest []string) int {
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
}

// cliDiff implements `android-tui diff <dev>`.
func cliDiff(p *core.Profile, serial string, rest []string) int {
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
