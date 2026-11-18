package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/madushas/android-tui/internal/qr"
)

// The QR specification asks for a 4-module quiet zone around the symbol; in half-block text that is two blank text rows
// above and below and four blank cells on each side. Scanners fail more often without it.
func TestQRRenderHasQuietZone(t *testing.T) {
	mat, err := qr.Encode("WIFI:T:ADB;S:studio-000000;P:00000000;;")
	if err != nil {
		t.Fatal(err)
	}
	lines := qrRender(mat)
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = ansi.Strip(l)
	}
	for _, i := range []int{0, 1, len(plain) - 2, len(plain) - 1} {
		if strings.Trim(plain[i], " ") != "" {
			t.Fatalf("quiet-zone row %d is not blank: %q", i, plain[i])
		}
	}
	if !strings.HasPrefix(plain[2], "    ") && strings.TrimLeft(plain[2], " ") == "" {
		t.Fatalf("expected the symbol to start after the quiet zone: %q", plain[2])
	}
	if len(lines) != (len(mat)+8+1)/2 {
		t.Fatalf("%d text rows for a %d-module symbol, want %d", len(lines), len(mat), (len(mat)+8+1)/2)
	}
}
