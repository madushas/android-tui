package ui

import "testing"

// The palette is built from several topic tables; this guards its structure (not its contents, which change as actions are added).
func TestPaletteIntegrity(t *testing.T) {
	m := New()
	seen := map[string]bool{}
	acts := m.actions()
	if len(acts) < 50 {
		t.Fatalf("the palette looks truncated: %d actions", len(acts))
	}
	for _, a := range acts {
		id := a.group + "/" + a.title
		if a.group == "" || a.title == "" || a.run == nil {
			t.Errorf("incomplete action: %+v", a)
		}
		if seen[id] {
			t.Errorf("duplicate action %q", id)
		}
		seen[id] = true
		_ = m.unmet(a.req) // every requirement keyword must be understood (it must not panic)
	}
	for _, want := range []string{"Emulator/Start", "Emulator/Root AVD with rootAVD… (power users)", "Host/Zoom main panel (toggle)"} {
		if !seen[want] {
			t.Errorf("expected action missing: %q", want)
		}
	}
}
