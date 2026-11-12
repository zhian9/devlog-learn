package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseSince converts a human readable cutoff into an absolute time.
//
// Accepted forms:
//
//	all, 0            -> zero time (no cutoff)
//	36h, 90m          -> Go duration syntax
//	7d, 2w            -> day/week suffixes
//	2026-01-02T03:04:05Z / RFC3339
//
// A zero time means "no cutoff" for every caller.
func ParseSince(value string, now time.Time) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, nil
	}

	// RFC3339 is case-sensitive (T/Z), so try it before lower-casing.
	if t, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return t, nil
	}

	s := strings.ToLower(trimmed)
	if s == "all" || s == "0" {
		return time.Time{}, nil
	}

	if strings.HasSuffix(s, "d") || strings.HasSuffix(s, "w") {
		unit := s[len(s)-1]
		number := s[:len(s)-1]
		n, err := strconv.Atoi(number)
		if err != nil || n < 0 {
			return time.Time{}, fmt.Errorf("invalid --since value %q: expected a non-negative number before %q", value, unit)
		}
		hours := int64(24)
		if unit == 'w' {
			hours = 24 * 7
		}
		return now.Add(-time.Duration(n) * time.Duration(hours) * time.Hour), nil
	}

	if d, err := time.ParseDuration(s); err == nil {
		if d < 0 {
			return time.Time{}, fmt.Errorf("invalid --since value %q: duration must not be negative", value)
		}
		return now.Add(-d), nil
	}

	return time.Time{}, fmt.Errorf("invalid --since value %q: use 7d, 2w, 36h, RFC3339, or all", value)
}
