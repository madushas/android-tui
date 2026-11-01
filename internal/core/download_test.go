package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestDownloadHashMatchesFile(t *testing.T) {
	payload := bytes.Repeat([]byte("android-tui test payload\n"), 150000) // ~3.7 MB: several read/write rounds
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(payload) }))
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "img.zip")
	got, err := Download(srv.URL, dest, func(string, bool) {}, func() bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	if got != hex.EncodeToString(sum[:]) || !bytes.Equal(b, payload) {
		t.Fatalf("reported hash %s does not describe the saved file (%d bytes)", got, len(b))
	}
}

// A cancelled or failed download must not leave a truncated file that looks finished.
func TestDownloadCancelRemovesPartial(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 8<<20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(payload) }))
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "img.zip")
	calls := 0
	_, err := Download(srv.URL, dest, func(string, bool) {}, func() bool { calls++; return calls > 1 })
	if err == nil {
		t.Fatal("want a cancellation error")
	}
	if _, serr := os.Stat(dest); serr == nil {
		t.Fatal("partial file was left behind")
	}
}

func TestDownloadHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "x.zip")
	if _, err := Download(srv.URL, dest, func(string, bool) {}, func() bool { return false }); err == nil {
		t.Fatal("HTTP 404 must be an error")
	}
	if _, serr := os.Stat(dest); serr == nil {
		t.Fatal("no file should be created for an HTTP error")
	}
}

// The QR pairing password protects the adb pairing handshake: well-formed and never repeated or zero.
func TestQRPairSecrets(t *testing.T) {
	nameRE, pwRE := regexp.MustCompile(`^studio-[0-9a-f]{6}$`), regexp.MustCompile(`^[0-9a-f]{8}$`)
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		q, err := NewQRPair()
		if err != nil {
			t.Fatal(err)
		}
		if !nameRE.MatchString(q.Name) || !pwRE.MatchString(q.PW) || q.PW == "00000000" {
			t.Fatalf("bad pairing secrets: %q %q", q.Name, q.PW)
		}
		if q.Payload != "WIFI:T:ADB;S:"+q.Name+";P:"+q.PW+";;" {
			t.Fatalf("payload: %q", q.Payload)
		}
		if seen[q.PW] {
			t.Fatalf("pairing password repeated: %s", q.PW)
		}
		seen[q.PW] = true
	}
}
