package core

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeName(t *testing.T) {
	cases := map[string]string{
		"Pixel 9 Pro":            "Pixel_9_Pro",
		"../../etc/passwd":       "_.._etc_passwd",
		"..":                     "device",
		"":                       "device",
		".hidden":                "hidden",
		"a/b\\c":                 "a_b_c",
		"name\x00null":           "name_null",
		"Samsung SM-S928B":       "Samsung_SM-S928B",
		strings.Repeat("x", 300): strings.Repeat("x", maxNameLen),
	}
	for in, want := range cases {
		got := SafeName(in)
		if got != want {
			t.Errorf("SafeName(%q) = %q, want %q", in, got, want)
		}
		if strings.ContainsAny(got, `/\`+"\x00") || got == "." || got == ".." || strings.HasPrefix(got, ".") {
			t.Errorf("SafeName(%q) = %q is not a single safe component", in, got)
		}
	}
}

func TestIsPackageName(t *testing.T) {
	for _, ok := range []string{"com.example.app", "com.google.android.apps.tips", "android", "a.b", "com.example_app.v2"} {
		if !IsPackageName(ok) {
			t.Errorf("%q should be a package name", ok)
		}
	}
	for _, bad := range []string{"", "../../x", "com/example", "com..example", "com.example.app;rm -rf ~", ".com.example", "com.example.", "1com.example", "com.example app", "com.example\x00"} {
		if IsPackageName(bad) {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func TestJoinWithin(t *testing.T) {
	base := filepath.Join(t.TempDir(), "pull")
	got, err := JoinWithin(base, "com.example.app")
	if err != nil || got != filepath.Join(base, "com.example.app") {
		t.Fatalf("normal join: %q %v", got, err)
	}
	for _, parts := range [][]string{
		{".."}, {"."}, {""}, {"../escape"}, {"a/b"}, {"a\\b"}, {"/abs"}, {"x", ".."}, {"ok", "\x00"},
	} {
		if p, err := JoinWithin(base, parts...); err == nil {
			t.Errorf("JoinWithin%q = %q, want an error", parts, p)
		}
	}
}
