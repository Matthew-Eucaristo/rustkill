package format

import (
	"testing"
	"time"
)

func TestBytes(t *testing.T) {
	for n, want := range map[int64]string{-1: "…", 0: "0 B", 999: "999 B", 1000: "1.0 kB", 1_500_000: "1.5 MB", 4_800_000_000: "4.8 GB"} {
		if got := Bytes(n); got != want {
			t.Errorf("Bytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestParseAge(t *testing.T) {
	day := 24 * time.Hour
	for in, want := range map[string]time.Duration{"90d": 90 * day, "6w": 42 * day, "3mo": 90 * day, "1y": 365 * day, "36h": 36 * time.Hour} {
		got, err := ParseAge(in)
		if err != nil || got != want {
			t.Errorf("ParseAge(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "d", "-3d", "soon"} {
		if _, err := ParseAge(in); err == nil {
			t.Errorf("ParseAge(%q) succeeded", in)
		}
	}
}

func TestAgo(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	for d, want := range map[time.Duration]string{time.Hour: "today", 3 * day: "3d ago", 21 * day: "3w ago", 120 * day: "4mo ago", 730 * day: "2.0y ago"} {
		if got := Ago(now.Add(-d), now); got != want {
			t.Errorf("Ago(-%v) = %q, want %q", d, got, want)
		}
	}
	if Ago(time.Time{}, now) != "?" {
		t.Error("zero time")
	}
}
