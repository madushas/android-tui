package qr

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// dump matrices to $QR_DUMP so an external decoder (zxing) can verify them.
func TestDump(t *testing.T) {
	path := os.Getenv("QR_DUMP")
	if path == "" {
		t.Skip("QR_DUMP not set")
	}
	out := map[string][][]bool{}
	for _, s := range []string{"WIFI:T:ADB;S:studio-ab12cd;P:7k2m9x4q;;", "hi", strings.Repeat("A", 14), strings.Repeat("B", 26),
		strings.Repeat("C", 42), strings.Repeat("D", 62), strings.Repeat("E", 84), strings.Repeat("F", 106)} {
		m, err := Encode(s)
		if err != nil {
			t.Fatal(err)
		}
		out[s] = m
	}
	b, _ := json.Marshal(out)
	_ = os.WriteFile(path, b, 0o644)
}

func TestTooLong(t *testing.T) {
	if _, err := Encode(strings.Repeat("x", 107)); err == nil {
		t.Fatal("expected error")
	}
}
