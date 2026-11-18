package ui

import (
	"testing"
)

// Host sampling runs on a poll goroutine while the UI goroutine applies scan results and renders the host panel.
// Run with -race (make race): sharing m.host between the two used to be a data race, and a torn read of its string
// field is memory-unsafe, not just cosmetic.
func TestHostSamplingDoesNotRaceWithRendering(t *testing.T) {
	m := New()
	m.w, m.h = 100, 40
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 300; i++ {
			_ = m.sampleHost() // the poll goroutine's part
		}
	}()
	for i := 0; i < 300; i++ {
		m.applyScan(scanMsg{host: m.sampleHost()}) // the UI goroutine: apply the result …
		_ = m.panelHost(rect{0, 0, 40, 8})         // … and render it
	}
	<-done
}
