package core

import (
	"strings"
	"time"
)

const shizukuStart = "/storage/emulated/0/Android/data/moe.shizuku.privileged.api/start.sh"

// ShizukuBootstrap runs Shizuku's start script (the app must be installed and have been opened once).
func ShizukuBootstrap(serial string) (bool, string) {
	if !strings.Contains(Exec(serial, "pm list packages moe.shizuku.privileged.api", false).Out, "moe.shizuku.privileged.api") {
		return false, "Shizuku (moe.shizuku.privileged.api) is not installed"
	}
	if r := Exec(serial, "ls "+shizukuStart, false); !r.OK() {
		return false, "start.sh not found — open the Shizuku app once so it creates it"
	}
	r := Exec(serial, "sh "+shizukuStart, false)
	out := strings.TrimSpace(r.Out + r.Err)
	if i := strings.LastIndex(out, "\n"); i >= 0 {
		out = out[i+1:]
	}
	time.Sleep(1500 * time.Millisecond)
	running := strings.TrimSpace(Exec(serial, "ps -A 2>/dev/null | grep -c shizuku_server", false).Out)
	if running != "" && running != "0" {
		return true, "Shizuku is running ✓"
	}
	return r.OK(), orDefault(out, "start.sh finished — check the Shizuku app")
}

// ───────────────────────────── settings snapshots & diff ─────────────────────────────
