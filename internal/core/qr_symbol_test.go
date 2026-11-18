package core

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/madushas/android-tui/internal/qr"
)

// Used by `make qr-check`: writes the symbol for a SYNTHETIC pairing payload (never a real one) so an independent
// checker can validate it against ISO/IEC 18004. Skipped unless QR_DUMP names an output file.
func TestDumpSyntheticPairingSymbol(t *testing.T) {
	out := os.Getenv("QR_DUMP")
	if out == "" {
		t.Skip("set QR_DUMP=<file> (see make qr-check)")
	}
	const payload = "WIFI:T:ADB;S:studio-000000;P:00000000;;"
	m, err := qr.Encode(payload)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString(payload + "\n")
	for _, row := range m {
		for _, c := range row {
			if c {
				b.WriteByte('1')
			} else {
				b.WriteByte('0')
			}
		}
		b.WriteByte('\n')
	}
	if err := os.WriteFile(out, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(os.Stderr, "wrote", out)
}
