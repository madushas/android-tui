package ui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/madushas/android-tui/internal/core"
)

type connRow struct {
	kind string // dev | mdns | saved | act
	dev  core.Device
	md   core.MDNS
	sv   core.SavedConn
	fn   func(m *Model) tea.Cmd
}

type connectTab struct {
	rowsTab
	devs []core.Device
	mdns []core.MDNS
	note string
	t    time.Time
	busy bool
}

func newConnect() Tab {
	c := &connectTab{rowsTab: rowsTab{baseTab: baseTab{ident: "connect", name: "Connect", global: true}}}
	c.build = c.buildRows
	return c
}

func (c *connectTab) Reload() { c.t = time.Time{} }

func (c *connectTab) Tick(m *Model) tea.Cmd {
	if c.busy || time.Since(c.t) < 3*time.Second {
		return nil
	}
	c.t, c.busy = time.Now(), true
	return m.job("adb connections", true, func() cont {
		d := core.ParseDevicesL(core.Run([]string{"adb", "devices", "-l"}, core.Quiet).Out)
		r := core.Run([]string{"adb", "mdns", "services"}, core.Quiet)
		md := core.ParseMDNS(r.Out)
		note := ""
		if low := strings.ToLower(r.Out + r.Err); strings.Contains(low, "unavailable") || strings.Contains(low, "not supported") || strings.Contains(low, "error") {
			note = "mDNS unavailable (adb built without it?)"
		}
		return func(m *Model) tea.Cmd { c.busy, c.devs, c.mdns, c.note = false, d, md, note; return nil }
	})
}

func (c *connectTab) buildRows(m *Model) []row {
	rows := []row{hdrRow(fmt.Sprintf("Connected (%d)", len(c.devs)))}
	for _, d := range c.devs {
		ok := sOK
		if d.State != "device" {
			ok = sErr
		}
		a, b, tr := padR(d.Serial, 30), padR(d.State, 13), padR(d.Transport, 11)
		rows = append(rows, row{text: a + " " + b + " " + tr + " " + d.Model, sty: a + " " + ok.Render(b) + " " + sAcc.Render(tr) + " " + sDim.Render(d.Model), d: connRow{kind: "dev", dev: d}})
	}
	if len(c.devs) == 0 {
		rows = append(rows, row{text: "(no devices — connect one below)", sty: sDim.Render("(no devices — connect one below)")})
	}
	rows = append(rows, hdrRow(fmt.Sprintf("Discovered on the network via mDNS (%d)", len(c.mdns))))
	for _, e := range c.mdns {
		lab := map[string]string{"pair": "pairing  — Enter, then type the 6-digit code", "connect": "connect  — Enter to connect", "plain": "adb service — Enter to connect"}[e.Kind]
		t := fmt.Sprintf("%s %s   %s", padR(e.Addr, 24), lab, e.Name)
		rows = append(rows, row{text: t, sty: t, d: connRow{kind: "mdns", md: e}})
	}
	if len(c.mdns) == 0 {
		t := orStr(c.note, "(nothing advertised — enable Wireless debugging on the phone)")
		rows = append(rows, row{text: t, sty: sDim.Render(t)})
	}
	saved := core.LoadConns()
	rows = append(rows, hdrRow(fmt.Sprintf("Saved connections (%d)", len(saved))))
	for _, s := range saved {
		t := padR(s.Name, 26) + " " + s.Addr
		rows = append(rows, row{text: t, sty: t, d: connRow{kind: "saved", sv: s}})
	}
	rows = append(rows, hdrRow("Connect"))
	for _, a := range []struct {
		l string
		f func(m *Model) tea.Cmd
	}{
		{"Pair with a QR code  (Android 11+ · Wireless debugging)", c.qrPair}, {"Pair with a pairing code…", c.codePair},
		{"Connect to host[:port]…", c.connectPrompt}, {"Scan the LAN for devices listening on :5555", c.scan},
		{"Switch the selected USB device to Wi-Fi (adb tcpip)", func(m *Model) tea.Cmd { return c.toTCPIP(m, nil) }},
		{"Switch back to USB mode (adb usb)", func(m *Model) tea.Cmd { return c.toUSB(m, nil) }},
		{"Use a remote adb server…  (adb -H)", c.remoteServer}, {"Restart the adb server", c.restartServer},
	} {
		rows = append(rows, row{text: a.l, sty: sAcc.Render(a.l), d: connRow{kind: "act", fn: a.f}})
	}
	return rows
}

func (c *connectTab) Hints() []hint {
	return []hint{{"⏎", "open / connect"}, {"a", "save new"}, {"d", "disconnect/remove"}, {"s", "save device"}, {"r", "refresh"}}
}

func (c *connectTab) View(m *Model, w, h int, focused bool) []string {
	c.head = func(m *Model) string {
		srv := os.Getenv("ADB_SERVER_SOCKET")
		s := sDim.Render("adb server ")
		if srv != "" {
			s += sWarn.Render(srv)
		} else {
			s += sAccB.Render("local")
		}
		if c.busy {
			s += sDim.Render("   " + spinner(m.frame))
		}
		return s
	}
	return c.viewRows(m, w, h, focused)
}

func (c *connectTab) curRow() *connRow {
	if r := c.cur(); r != nil && r.d != nil {
		x := r.d.(connRow)
		return &x
	}
	return nil
}

func (c *connectTab) msgJob(m *Model, label string, fn func() (bool, string)) tea.Cmd {
	return m.job(label, false, func() cont {
		ok, msg := fn()
		return func(m *Model) tea.Cmd {
			m.report(ok, label+": "+msg)
			c.t = time.Time{}
			return m.refreshNow()
		}
	})
}

func (c *connectTab) Key(m *Model, k tea.KeyMsg) (bool, tea.Cmd) {
	s := k.String()
	if c.nav(s) {
		return true, nil
	}
	d := c.curRow()
	switch s {
	case "r":
		c.t = time.Time{}
	case "a":
		m.ask("Name", "My phone", func(m *Model, n string) tea.Cmd {
			m.ask("host:port", "192.0.2.", func(m *Model, a string) tea.Cmd {
				if n != "" && a != "" {
					core.AddSaved(n, a)
				}
				return nil
			})
			return nil
		})
	case "s":
		if d != nil && d.kind == "dev" {
			c.saveDev(m, d.dev)
		}
	case "d":
		if d == nil {
			break
		}
		switch d.kind {
		case "saved":
			core.RemoveSaved(d.sv.Addr)
		case "dev":
			if strings.HasPrefix(d.dev.Transport, "wifi") {
				ser := d.dev.Serial
				return true, c.msgJob(m, "disconnect", func() (bool, string) {
					r := core.Run([]string{"adb", "disconnect", ser}, core.Opt{})
					return r.OK(), core.AdbMsg(r)
				})
			}
		}
	case "enter":
		if d == nil {
			return true, nil
		}
		switch d.kind {
		case "act":
			return true, d.fn(m)
		case "dev":
			c.devMenu(m, d.dev)
		case "mdns":
			e := d.md
			if e.Kind == "pair" {
				m.ask("Pairing code shown on the phone for "+e.Addr, "", func(m *Model, code string) tea.Cmd {
					if code == "" {
						return nil
					}
					return c.msgJob(m, "pair", func() (bool, string) { return core.AdbPair(e.Addr, code) })
				})
			} else {
				return true, c.msgJob(m, "connect", func() (bool, string) { return core.AdbConnect(e.Addr) })
			}
		case "saved":
			a := d.sv.Addr
			return true, c.msgJob(m, "connect", func() (bool, string) { return core.AdbConnect(a) })
		}
	default:
		return false, nil
	}
	return true, nil
}

func (c *connectTab) devMenu(m *Model, d core.Device) {
	type opt struct {
		t string
		f func() tea.Cmd
	}
	opts := []opt{{"Select in the connections panel", func() tea.Cmd {
		for i, x := range m.sc.Conns {
			if x.Serial == d.Serial {
				m.connSel, m.grp, m.focus = i, gConn, fConn
				m.checkSelect()
			}
		}
		for i, x := range m.sc.Emus {
			if x.Serial == d.Serial {
				m.emuSel, m.grp, m.focus = i, gEmu, fEmu
				m.checkSelect()
			}
		}
		return nil
	}}}
	if d.State == "device" {
		opts = append(opts, opt{"Interactive adb shell", func() tea.Cmd { return m.ext(false, "adb", "-s", d.Serial, "shell") }},
			opt{"Reboot", func() tea.Cmd {
				return c.msgJob(m, "reboot", func() (bool, string) { r := core.Adb(d.Serial, core.Opt{}, "reboot"); return r.OK(), core.AdbMsg(r) })
			}})
	}
	if strings.HasPrefix(d.Transport, "wifi") {
		opts = append(opts, opt{"Disconnect", func() tea.Cmd {
			return c.msgJob(m, "disconnect", func() (bool, string) {
				r := core.Run([]string{"adb", "disconnect", d.Serial}, core.Opt{})
				return r.OK(), core.AdbMsg(r)
			})
		}}, opt{"Save as a connection", func() tea.Cmd { c.saveDev(m, d); return nil }},
			opt{"Switch back to USB mode", func() tea.Cmd { return c.toUSB(m, &d) }})
	} else if d.Transport == "usb" {
		opts = append(opts, opt{"Switch to Wi-Fi (adb tcpip 5555)", func() tea.Cmd { return c.toTCPIP(m, &d) }})
	}
	var names []string
	for _, o := range opts {
		names = append(names, o.t)
	}
	m.choose(d.Serial+" · "+d.Model, names, func(m *Model, i int) tea.Cmd { return opts[i].f() })
}

func (c *connectTab) saveDev(m *Model, d core.Device) {
	if strings.HasPrefix(d.Transport, "wifi") && strings.Contains(d.Serial, ":") {
		core.AddSaved(orStr(d.Model, d.Serial), d.Serial)
		m.report(true, "saved "+d.Serial)
	} else {
		m.say("only host:port Wi-Fi devices can be saved")
	}
}

func (c *connectTab) qrPair(m *Model) tea.Cmd {
	q, err := newQRModal(func(m *Model, ok bool) tea.Cmd {
		if ok {
			m.report(true, "paired and connected ✓")
		}
		c.t = time.Time{}
		return m.refreshNow()
	})
	if err != nil {
		m.report(false, "cannot start QR pairing: "+err.Error())
		return nil
	}
	m.push(q)
	return nil
}

func (c *connectTab) codePair(m *Model) tea.Cmd {
	m.ask("Pairing address  host:port  (from 'Pair device with pairing code')", "", func(m *Model, a string) tea.Cmd {
		if a == "" {
			return nil
		}
		m.ask("Six-digit pairing code", "", func(m *Model, code string) tea.Cmd {
			if code == "" {
				return nil
			}
			return c.msgJob(m, "pair", func() (bool, string) { return core.AdbPair(a, code) })
		})
		return nil
	})
	return nil
}

func (c *connectTab) connectPrompt(m *Model) tea.Cmd {
	m.ask("Connect to  host[:port]  (default port 5555)", "", func(m *Model, a string) tea.Cmd {
		if a == "" {
			return nil
		}
		return c.msgJob(m, "connect", func() (bool, string) {
			ok, msg := core.AdbConnect(a)
			if ok {
				core.Remember(strings.TrimSpace(map[bool]string{true: a, false: a + ":5555"}[strings.Contains(a, ":")]))
			}
			return ok, msg
		})
	})
	return nil
}

func (c *connectTab) scan(m *Model) tea.Cmd {
	ip := core.LocalIP()
	if ip == "" {
		m.report(false, "no network detected")
		return nil
	}
	return m.job("LAN scan", false, func() cont {
		found := core.LanScan(5555, 400*time.Millisecond, nil)
		return func(m *Model) tea.Cmd {
			if len(found) == 0 {
				m.say("nothing is listening on :5555 — start it with 'adb tcpip 5555' over USB first")
				return nil
			}
			m.choose("Hosts with adb on :5555", found, func(m *Model, i int) tea.Cmd {
				h := found[i]
				return c.msgJob(m, "connect", func() (bool, string) {
					ok, msg := core.AdbConnect(h)
					if ok {
						core.Remember(h + ":5555")
					}
					return ok, msg
				})
			})
			return nil
		}
	})
}

func (c *connectTab) pickDev(m *Model, d *core.Device, usb bool) *core.Device {
	if d != nil {
		return d
	}
	if r := c.curRow(); r != nil && r.kind == "dev" {
		if (usb && r.dev.Transport == "usb") || (!usb && strings.HasPrefix(r.dev.Transport, "wifi")) {
			return &r.dev
		}
	}
	return nil
}

func (c *connectTab) toTCPIP(m *Model, d *core.Device) tea.Cmd {
	d = c.pickDev(m, d, true)
	if d == nil || d.State != "device" {
		m.say("select a USB device in the Connected list first")
		return nil
	}
	s := d.Serial
	return c.msgJob(m, "tcpip", func() (bool, string) { return core.ToTCPIP(s) })
}

func (c *connectTab) toUSB(m *Model, d *core.Device) tea.Cmd {
	d = c.pickDev(m, d, false)
	if d == nil {
		m.say("select a Wi-Fi device in the Connected list first")
		return nil
	}
	s := d.Serial
	return c.msgJob(m, "usb", func() (bool, string) {
		r := core.Adb(s, core.Opt{Timeout: 15 * time.Second}, "usb")
		return r.OK(), core.AdbMsg(r)
	})
}

func (c *connectTab) remoteServer(m *Model) tea.Cmd {
	m.ask("Remote adb server  host[:port]  (empty = local)", "", func(m *Model, v string) tea.Cmd {
		ok, msg := core.UseAdbServer(v)
		m.report(ok, msg)
		c.t = time.Time{}
		return m.refreshNow()
	})
	return nil
}

func (c *connectTab) restartServer(m *Model) tea.Cmd {
	return c.msgJob(m, "adb server", func() (bool, string) {
		core.Run([]string{"adb", "kill-server"}, core.Opt{Timeout: 10 * time.Second})
		r := core.Run([]string{"adb", "start-server"}, core.Opt{Timeout: 30 * time.Second})
		return r.OK(), "adb server restarted"
	})
}
