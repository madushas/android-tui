package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Emit reports progress; replace=true overwrites the previous replaceable line.
type Emit func(text string, replace bool)

func httpGet(u string, limit int64, timeout time.Duration) ([]byte, error) {
	c := &http.Client{Timeout: timeout}
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "android-tui/"+Version)
	r, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Body.Close() }()
	if r.StatusCode >= 400 {
		return nil, fmt.Errorf("server returned HTTP %d", r.StatusCode)
	}
	return io.ReadAll(io.LimitReader(r.Body, limit))
}

var zipHref = regexp.MustCompile(`(?i)href=["']([^"'?#]+\.zip)["']`)

type Remote struct{ Name, URL string }

// ListRemoteZips scrapes .zip links from a directory-listing page (newest-looking last).
func ListRemoteZips(page string) ([]Remote, error) {
	b, err := httpGet(page, 4<<20, 20*time.Second)
	if err != nil {
		return nil, err
	}
	base, _ := url.Parse(page)
	seen := map[string]bool{}
	var out []Remote
	for _, m := range zipHref.FindAllStringSubmatch(string(b), -1) {
		u, err := base.Parse(m[1])
		if err != nil || seen[u.String()] {
			continue
		}
		seen[u.String()] = true
		name, _ := url.PathUnescape(filepath.Base(u.Path))
		out = append(out, Remote{name, u.String()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Download streams u to dest and returns its sha256 (hex).
func Download(u, dest string, emit Emit, cancelled func() bool) (string, error) {
	c := &http.Client{}
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "android-tui/"+Version)
	r, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = r.Body.Close() }()
	if r.StatusCode >= 400 {
		return "", fmt.Errorf("server returned HTTP %d", r.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	// Any failure (including a full disk or a cancel) must not leave a truncated file that looks like a finished download.
	fail := func(err error) (string, error) {
		_ = f.Close()
		_ = os.Remove(dest)
		return "", err
	}
	h := sha256.New()
	buf := make([]byte, 1<<20)
	var done int64
	t0, last := time.Now(), time.Time{}
	for {
		if cancelled() {
			return fail(fmt.Errorf("cancelled"))
		}
		n, err := r.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return fail(fmt.Errorf("writing %s: %w", dest, werr))
			}
			h.Write(buf[:n]) // hash.Hash.Write never returns an error
			done += int64(n)
			if time.Since(last) > 250*time.Millisecond {
				last = time.Now()
				pct := ""
				if r.ContentLength > 0 {
					pct = fmt.Sprintf("%5.1f%%  ", 100*float64(done)/float64(r.ContentLength))
				}
				emit(fmt.Sprintf("  %s%s   %s/s", pct, Human(float64(done)), Human(float64(done)/time.Since(t0).Seconds())), true)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fail(err)
		}
	}
	if err := f.Close(); err != nil { // a failed close can mean the last buffered data never reached the disk
		_ = os.Remove(dest)
		return "", fmt.Errorf("writing %s: %w", dest, err)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	emit(fmt.Sprintf("  downloaded %s  sha256 %s…", Human(float64(done)), sum[:16]), true)
	return sum, nil
}

var shaRE = regexp.MustCompile(`^\s*([0-9a-fA-F]{64})`)

func RemoteSHA256(u string) string {
	for _, suf := range []string{".sha256sum", ".sha256"} {
		if b, err := httpGet(u+suf, 4096, 10*time.Second); err == nil {
			if m := shaRE.FindStringSubmatch(string(b)); m != nil {
				return strings.ToLower(m[1])
			}
		}
	}
	return ""
}

// ───────────────────────────── import ─────────────────────────────
