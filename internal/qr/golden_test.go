package qr

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

// goldenPayloads are the inputs whose symbols are pinned below.
func goldenPayloads() []string {
	p := []string{"", "A", "HELLO WORLD", "WIFI:T:ADB;S:studio-0a1b2c;P:3f9d2c1e;;", strings.Repeat("x", 17), strings.Repeat("ab", 40),
		strings.Repeat("0123456789", 12), "https://example.com/" + strings.Repeat("q", 60)}
	for n := 1; n <= 120; n += 7 {
		p = append(p, strings.Repeat("Z", n))
	}
	return p
}

// golden is "payload length|rows x cols|first 8 bytes of the SHA-256 of the module matrix", recorded from the encoder when it
// was split into stages. It is a CHARACTERIZATION: it stops accidental changes, it does not prove the symbols scan.
// (Scanning by a real phone is checked by hand; if you change the encoder on purpose, regenerate this table and say so.)
var golden = []string{
	"0|21x21|8a4da9fd3d6f43de",
	"1|21x21|8a59586d0e953fdd",
	"11|21x21|740367f782acd805",
	"39|29x29|db17ef70501285d9",
	"17|25x25|2e4c7b3160ea2e14",
	"80|37x37|81bc9bcaac67bf9b",
	"120|ERR payload too long for the built-in QR encoder",
	"80|37x37|e16e99cfe1c98bee",
	"1|21x21|0ccd037073d59266",
	"8|21x21|63fb6437e9eda71c",
	"15|25x25|d831e44b2d56ff33",
	"22|25x25|634cc017d5e832fa",
	"29|29x29|8e57db94cc461b14",
	"36|29x29|c70bf94891cc9bd0",
	"43|33x33|0a1948affb802fe4",
	"50|33x33|9824b29c7e7ffbed",
	"57|33x33|7d55affcdf5aa9b9",
	"64|37x37|5eb84f68c056edf2",
	"71|37x37|a4365b2c57384138",
	"78|37x37|728dfe0e05c8e8a7",
	"85|41x41|2614009b76fdd7c0",
	"92|41x41|22e8c781d431bbf4",
	"99|41x41|a9f1083522dd1281",
	"106|41x41|ed185e0fb6b7571e",
	"113|ERR payload too long for the built-in QR encoder",
	"120|ERR payload too long for the built-in QR encoder",
}

func TestEncodeGolden(t *testing.T) {
	payloads := goldenPayloads()
	if len(payloads) != len(golden) {
		t.Fatalf("%d payloads but %d golden rows", len(payloads), len(golden))
	}
	for i, p := range payloads {
		var got string
		m, err := Encode(p)
		if err != nil {
			got = fmt.Sprintf("%d|ERR %v", len(p), err)
		} else {
			h := sha256.New()
			for _, row := range m {
				for _, c := range row {
					if c {
						h.Write([]byte{1})
					} else {
						h.Write([]byte{0})
					}
				}
				h.Write([]byte{9})
			}
			got = fmt.Sprintf("%d|%dx%d|%x", len(p), len(m), len(m[0]), h.Sum(nil)[:8])
		}
		if got != golden[i] {
			t.Errorf("payload %d (%d bytes): symbol changed\n got  %s\n want %s", i, len(p), got, golden[i])
		}
	}
}
