package core

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
)

// Device-supplied text (model names, package names, file names) must never decide where android-tui writes locally.
// A device on the network controls these strings, so every such value passes through one of the helpers below before it
// becomes part of a local path.

const maxNameLen = 64

// SafeName turns a device-supplied string into one harmless path component: characters outside [A-Za-z0-9._-] become
// "_", leading dots are removed (no hidden or ".." names), and the result is short. An empty result becomes "device".
func SafeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.TrimLeft(b.String(), ".")
	if len(out) > maxNameLen {
		out = out[:maxNameLen]
	}
	if out == "" {
		return "device"
	}
	return out
}

// pkgRE is the shape of an Android application id: dot-separated identifiers. One segment is allowed ("android" is the
// framework package). No leading, trailing or doubled dots, so a package name can never be "." or "..".
var pkgRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)*$`)

// IsPackageName reports whether s is a well-formed package name (so it is safe to use as a directory name).
func IsPackageName(s string) bool { return len(s) <= 255 && pkgRE.MatchString(s) }

var errEscape = errors.New("path would leave its folder")

// JoinWithin joins parts under base. Every part must be a single ordinary name (no separators, no "." or "..", no NUL),
// and the result must stay inside base, so a hostile name cannot write outside the folder the user chose.
func JoinWithin(base string, parts ...string) (string, error) {
	for _, p := range parts {
		if p == "" || p == "." || p == ".." || strings.ContainsAny(p, "/\\\x00") {
			return "", errEscape
		}
	}
	joined := filepath.Join(append([]string{base}, parts...)...)
	rel, err := filepath.Rel(base, joined)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errEscape
	}
	return joined, nil
}
