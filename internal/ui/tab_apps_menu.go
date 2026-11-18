package ui

import (
	"fmt"
)

// ContextTitle names the menu after the package under the cursor.
func (a *appsTab) ContextTitle() string {
	if p := a.curPkg(); p != nil {
		return p.Name
	}
	return "Apps"
}

// Context is the right-click / "." menu: actions for the package on a row, list-level actions on empty space.
// Every item sends the same key the keyboard would, so the menu and the shortcuts cannot disagree.
func (a *appsTab) Context(m *Model, onRow bool) []menuItem {
	item := func(label, k, off string, sep bool) menuItem {
		msg, _ := keyMsgFor(k)
		return menuItem{label: label, key: k, run: mainKey(msg), off: off, sep: sep}
	}
	offIf := func(cond bool, why string) string {
		if cond {
			return why
		}
		return ""
	}
	p := a.curPkg()
	if !onRow || p == nil {
		return []menuItem{
			item("Install APK from file…", "i", "", false),
			item("Install APK from URL…", "U", "", false),
			item("Filter…", "/", "", true),
			item("Next scope ("+scopes[(a.scope+1)%len(scopes)]+")", "f", "", false),
			item("Change sort order", "s", "", false),
			item("Mark / unmark all", "a", "", false),
			item("Export package list", "x", "", true),
			item("Refresh", "r", "", false),
		}
	}
	if scopes[a.scope] == "removed" {
		return []menuItem{item("Restore app", "⏎", "", false), item("Mark / unmark", "Space", "", true)}
	}
	uninstall := "Uninstall…"
	if p.System {
		uninstall = "Remove for this user… (restorable)"
	}
	marked := ""
	if n := len(a.marks); n > 0 {
		marked = fmt.Sprintf("  (%d marked)", n)
	}
	return []menuItem{
		item("Launch", "⏎", offIf(p.Disabled, "the app is disabled"), false),
		item("Force stop", "S", "", false),
		item("Clear data…", "C", "", false),
		item("Disable"+marked, "d", offIf(p.Disabled && marked == "", "already disabled"), true),
		item("Enable"+marked, "e", offIf(!p.Disabled && marked == "", "already enabled"), false),
		item(uninstall+marked, "u", "", false),
		item("Permissions…", "p", "", true),
		item("More actions…", "m", "", false),
		item("Pull APK to computer", "P", "", false),
		item("Mark / unmark", "Space", "", true),
		item("Mark / unmark all", "a", "", false),
	}
}
