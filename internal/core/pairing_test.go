package core

import "testing"

// A failed pairing must not look like a success because some OTHER wireless device is connected.
func TestPairDone(t *testing.T) {
	ents := []MDNS{
		{Name: "adb-OTHER-1", Kind: "connect", Addr: "192.0.2.50:41235"},
		{Name: "adb-MINE-2", Kind: "connect", Addr: "192.0.2.7:44444"},
	}
	cases := []struct {
		name string
		devs []Device
		want bool
	}{
		{"nothing connected", nil, false},
		{"another wireless phone connected", []Device{{Serial: "192.0.2.50:41235", State: "device"}}, false},
		{"the paired phone connected by ip:port", []Device{{Serial: "192.0.2.7:44444", State: "device"}}, true},
		{"the paired phone listed by its mDNS name", []Device{{Serial: "adb-MINE-2"}}, true},
		{"another phone discovered over mDNS", []Device{{Serial: "adb-OTHER-9._adb-tls-connect._tcp"}}, false},
		{"usb phone only", []Device{{Serial: "TEST-DEVICE-001", Transport: "usb"}}, false},
	}
	for _, c := range cases {
		if got := pairDone(c.devs, ents, "192.0.2.7"); got != c.want {
			t.Errorf("%s: pairDone = %v, want %v", c.name, got, c.want)
		}
	}
	// the IP must match exactly as a host, not as a prefix ("192.0.2.7" must not match "192.0.2.71")
	if pairDone([]Device{{Serial: "192.0.2.71:5555"}}, nil, "192.0.2.7") {
		t.Error("192.0.2.71 must not count as 192.0.2.7")
	}
}

func TestParseAdbVersion(t *testing.T) {
	for out, want := range map[string]int{
		"Android Debug Bridge version 1.0.41\nVersion 34.0.5-10900879\nInstalled as /usr/bin/adb\n": 34,
		"Version 30.0.0-6686991\n": 30,
		"Version 29.0.6\n":         29,
	} {
		if v, ok := parseAdbVersion(out); !ok || v != want {
			t.Errorf("parseAdbVersion(%q) = %d,%v; want %d", out, v, ok, want)
		}
	}
	if _, ok := parseAdbVersion("command not found"); ok {
		t.Error("garbage must not parse")
	}
}
