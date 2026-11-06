// Package format holds small human-readable formatting helpers.
package format

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Bytes formats n with decimal units like Finder (1 GB = 1000^3 bytes).
func Bytes(n int64) string {
	if n < 0 {
		return "…"
	}
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "kMGTPE"[exp])
}

// Ago formats how long ago t was, coarsely ("today", "3d", "5w", "4mo", "2y").
func Ago(t, now time.Time) string {
	if t.IsZero() {
		return "?"
	}
	d := now.Sub(t)
	days := int(d.Hours() / 24)
	switch {
	case days < 1:
		return "today"
	case days < 14:
		return fmt.Sprintf("%dd ago", days)
	case days < 60:
		return fmt.Sprintf("%dw ago", days/7)
	case days < 365:
		return fmt.Sprintf("%dmo ago", days/30)
	default:
		return fmt.Sprintf("%.1fy ago", float64(days)/365)
	}
}

// ParseAge parses ages like "90d", "6w", "3mo", "1y", or any Go duration.
func ParseAge(s string) (time.Duration, error) {
	units := []struct {
		suffix string
		d      time.Duration
	}{{"mo", 30 * 24 * time.Hour}, {"d", 24 * time.Hour}, {"w", 7 * 24 * time.Hour}, {"y", 365 * 24 * time.Hour}}
	for _, u := range units {
		if v, ok := strings.CutSuffix(s, u.suffix); ok {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return 0, fmt.Errorf("invalid age %q", s)
			}
			return time.Duration(n) * u.d, nil
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid age %q (try 90d, 6w, 3mo or 1y)", s)
	}
	return d, nil
}

// Tilde shortens home to "~" for display.
func Tilde(path, home string) string {
	if home != "" && (path == home || strings.HasPrefix(path, home+string(filepath.Separator))) {
		return "~" + path[len(home):]
	}
	return path
}

// Rel shows path relative to root, or path itself when it is root.
func Rel(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return filepath.Base(path)
}
