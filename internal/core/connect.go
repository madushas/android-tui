package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// SavedConn is a bookmarked Wi-Fi adb endpoint.
type SavedConn struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
}

func connFile() string { return filepath.Join(ConfDir, "connections.json") }

func LoadConns() []SavedConn {
	b, err := os.ReadFile(connFile())
	if err != nil {
		return nil
	}
	var c []SavedConn
	if json.Unmarshal(b, &c) != nil {
		return nil
	}
	out := c[:0]
	for _, x := range c {
		if x.Addr != "" {
			out = append(out, x)
		}
	}
	return out
}

func SaveConns(c []SavedConn) {
	MkdirP(ConfDir)
	b, _ := json.MarshalIndent(c, "", "  ")
	_ = os.WriteFile(connFile(), b, 0o644)
}

func AddSaved(name, addr string) {
	if !strings.Contains(addr, ":") {
		addr += ":5555"
	}
	var out []SavedConn
	for _, c := range LoadConns() {
		if c.Addr != addr {
			out = append(out, c)
		}
	}
	SaveConns(append(out, SavedConn{name, addr}))
}

func RemoveSaved(addr string) {
	var out []SavedConn
	for _, c := range LoadConns() {
		if c.Addr != addr {
			out = append(out, c)
		}
	}
	SaveConns(out)
}

// Remember saves addr unless it is already known (or an mDNS-style serial).
func Remember(addr string) {
	if !strings.Contains(addr, ":") || strings.HasPrefix(addr, "adb-") {
		return
	}
	for _, c := range LoadConns() {
		if c.Addr == addr {
			return
		}
	}
	AddSaved(addr, addr)
}

func AdbMsg(r Res) string {
	if t := r.Both(); t != "" {
		l := strings.Split(t, "\n")
		return l[len(l)-1]
	}
	if r.OK() {
		return "ok"
	}
	return fmt.Sprintf("exit %d", r.RC)
}

func AdbConnect(addr string) (bool, string) {
	addr = strings.TrimSpace(addr)
	if !strings.Contains(addr, ":") {
		addr += ":5555"
	}
	r := Run([]string{"adb", "connect", addr}, Opt{Timeout: 15 * time.Second})
	o := strings.ToLower(r.Out)
	ok := r.OK() && strings.Contains(o, "connected") && !strings.Contains(o, "cannot") && !strings.Contains(o, "failed")
	return ok, AdbMsg(r)
}

func AdbPair(addr, code string) (bool, string) {
	r := Run([]string{"adb", "pair", strings.TrimSpace(addr), strings.TrimSpace(code)}, Opt{Timeout: 30 * time.Second})
	return r.OK() && strings.Contains(strings.ToLower(r.Out), "success"), AdbMsg(r)
}

// UseAdbServer points every adb call at host[:port], or back to the local server when empty.
func UseAdbServer(spec string) (bool, string) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		if err := os.Unsetenv("ADB_SERVER_SOCKET"); err != nil {
			return false, err.Error()
		}
		return true, "using the local adb server"
	}
	h, p, _ := strings.Cut(spec, ":")
	if p == "" {
		p = "5037"
	}
	if err := os.Setenv("ADB_SERVER_SOCKET", "tcp:"+h+":"+p); err != nil {
		return false, err.Error()
	}
	return true, fmt.Sprintf("adb now talks to the server at %s:%s", h, p)
}

func LocalIP() string {
	c, err := net.Dial("udp", "10.255.255.255:1")
	if err != nil {
		return ""
	}
	defer func() { _ = c.Close() }()
	if a, ok := c.LocalAddr().(*net.UDPAddr); ok {
		return a.IP.String()
	}
	return ""
}

// LanScan probes the local /24 (or hosts) for an open adb port.
func LanScan(port int, timeout time.Duration, hosts []string) []string {
	if hosts == nil {
		ip := LocalIP()
		if ip == "" {
			return nil
		}
		base := ip[:strings.LastIndex(ip, ".")]
		for i := 1; i < 255; i++ {
			if h := fmt.Sprintf("%s.%d", base, i); h != ip {
				hosts = append(hosts, h)
			}
		}
	}
	var mu sync.Mutex
	var found []string
	var wg sync.WaitGroup
	sem := make(chan struct{}, 64)
	for _, h := range hosts {
		wg.Add(1)
		sem <- struct{}{}
		go func(h string) {
			defer wg.Done()
			defer func() { <-sem }()
			if c, err := net.DialTimeout("tcp", net.JoinHostPort(h, fmt.Sprint(port)), timeout); err == nil {
				_ = c.Close()
				mu.Lock()
				found = append(found, h)
				mu.Unlock()
			}
		}(h)
	}
	wg.Wait()
	sort.Strings(found)
	return found
}

var ipRE = regexp.MustCompile(`inet (\d+\.\d+\.\d+\.\d+)`)

// ToTCPIP switches a USB device to Wi-Fi adb and connects to it.
func ToTCPIP(serial string) (bool, string) {
	m := ipRE.FindStringSubmatch(Sh(serial, "ip -f inet addr show wlan0", Opt{Timeout: 8 * time.Second}).Out)
	if m == nil {
		return false, "could not read the phone's Wi-Fi IP — is Wi-Fi on?"
	}
	if r := Adb(serial, Opt{Timeout: 15 * time.Second}, "tcpip", "5555"); !r.OK() {
		return false, AdbMsg(r)
	}
	time.Sleep(2 * time.Second)
	ok, msg := AdbConnect(m[1] + ":5555")
	if ok {
		Remember(m[1] + ":5555")
		msg += "  — you can unplug the cable"
	}
	return ok, msg
}

// ───────────────────────────── QR pairing (Android 11+) ─────────────────────────────

// QRPair drives "Pair device with QR code": show a QR, wait for the phone's mDNS pairing service, pair, connect.
type QRPair struct {
	Name, PW, Payload string
	mu                sync.Mutex
	lines             []string
	fin, ok, cancel   bool
}

// randHex returns n random bytes from the OS CSPRNG as hex. The QR pairing password guards the adb pairing handshake,
// so a failure must be reported and never degrade into a predictable value.
func randHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func NewQRPair() (*QRPair, error) {
	name, err := randHex(3)
	if err != nil {
		return nil, fmt.Errorf("no secure random source: %w", err)
	}
	pw, err := randHex(4)
	if err != nil {
		return nil, fmt.Errorf("no secure random source: %w", err)
	}
	q := &QRPair{Name: "studio-" + name, PW: pw}
	q.Payload = fmt.Sprintf("WIFI:T:ADB;S:%s;P:%s;;", q.Name, q.PW)
	q.lines = []string{"waiting for the phone to scan the code…"}
	return q, nil
}

func (q *QRPair) say(t string) {
	q.mu.Lock()
	q.lines = append(q.lines, t)
	if len(q.lines) > 6 {
		q.lines = q.lines[len(q.lines)-6:]
	}
	q.mu.Unlock()
}

func (q *QRPair) Cancel() { q.mu.Lock(); q.cancel = true; q.mu.Unlock() }

func (q *QRPair) State() (lines []string, fin, ok bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]string(nil), q.lines...), q.fin, q.ok
}

func (q *QRPair) cancelled() bool { q.mu.Lock(); defer q.mu.Unlock(); return q.cancel }

func (q *QRPair) finish(ok bool) { q.mu.Lock(); q.fin, q.ok = true, ok; q.mu.Unlock() }

// Run blocks until paired, cancelled or timed out; call in a goroutine.
func (q *QRPair) Run() {
	t0 := time.Now()
	pairedIP := ""
	for !q.cancelled() && time.Since(t0) < 240*time.Second {
		ents := ParseMDNS(Run([]string{"adb", "mdns", "services"}, Quiet).Out)
		if pairedIP == "" {
			for _, e := range ents {
				if e.Kind == "pair" && e.Name == q.Name {
					q.say("phone found at " + e.Addr + " — pairing…")
					ok, msg := AdbPair(e.Addr, q.PW)
					q.say(msg)
					if !ok {
						q.finish(false)
						return
					}
					pairedIP = e.Addr[:strings.LastIndex(e.Addr, ":")]
					q.say("paired ✓ — waiting for the connect service…")
					t0 = time.Now().Add(-200 * time.Second)
				}
			}
		} else {
			for _, d := range ParseDevicesL(Run([]string{"adb", "devices", "-l"}, Quiet).Out) {
				if strings.HasPrefix(d.Serial, pairedIP) || strings.Contains(d.Serial, "_adb-tls-connect") {
					q.say("connected ✓")
					q.finish(true)
					return
				}
			}
			for _, e := range ents {
				if e.Kind == "connect" && strings.HasPrefix(e.Addr, pairedIP+":") {
					ok, msg := AdbConnect(e.Addr)
					q.say(msg)
					if ok {
						q.finish(true)
						return
					}
				}
			}
		}
		time.Sleep(1200 * time.Millisecond)
	}
	if !q.cancelled() {
		q.say("timed out — is 'Wireless debugging' on and 'Pair with QR code' open on the phone?")
	}
	q.finish(false)
}
