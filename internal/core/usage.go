package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Palette "frecency": how often each action was run, kept in DataDir/usage.json (best effort, per user).
var usageMu sync.Mutex

func usagePath() string { return filepath.Join(DataDir, "usage.json") }

func loadUsage() map[string]int {
	m := map[string]int{}
	if b, err := os.ReadFile(usagePath()); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

// UsageCounts returns the usage count per action key.
func UsageCounts() map[string]int {
	usageMu.Lock()
	defer usageMu.Unlock()
	return loadUsage()
}

// BumpUsage records one use of key. When the total grows past 500 every count is halved (old habits fade) and zeros are dropped.
func BumpUsage(key string) {
	usageMu.Lock()
	defer usageMu.Unlock()
	m := loadUsage()
	m[key]++
	total := 0
	for _, n := range m {
		total += n
	}
	if total > 500 {
		for k, n := range m {
			if n /= 2; n == 0 {
				delete(m, k)
			} else {
				m[k] = n
			}
		}
	}
	MkdirP(DataDir)
	b, _ := json.Marshal(m)
	tmp := usagePath() + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, usagePath())
	}
}
