package ui

import "testing"

func TestFuzzy(t *testing.T) {
	best := func(q string, texts ...string) string {
		bi, bs := -1, -1
		for i, x := range texts {
			if s, ok := fuzzy(q, x); ok && s > bs {
				bi, bs = i, s
			}
		}
		if bi < 0 {
			return ""
		}
		return texts[bi]
	}
	if got := best("shiz", "System  Start Shizuku (adb)", "Host  Show hidden zip images"); got != "System  Start Shizuku (adb)" {
		t.Errorf("shiz → %q", got)
	}
	// a substring match beats a scattered subsequence
	if got := best("scr", "Misc  Set custom rule", "Tools  Screenshot"); got != "Tools  Screenshot" {
		t.Errorf("scr → %q", got)
	}
	// word-start substring beats mid-word substring
	if got := best("log", "Inspect  Catalog", "Inspect  Logcat"); got != "Inspect  Logcat" {
		t.Errorf("log → %q", got)
	}
	if _, ok := fuzzy("zzq", "Start Shizuku"); ok {
		t.Error("non-match accepted")
	}
	if _, ok := fuzzy("start shiz", "Start Shizuku"); !ok {
		t.Error("multi-token query should match")
	}
	if _, ok := fuzzy("start xyz", "Start Shizuku"); ok {
		t.Error("every token must match")
	}
	if s, ok := fuzzy("", "anything"); !ok || s != 0 {
		t.Error("empty query matches everything")
	}
}
