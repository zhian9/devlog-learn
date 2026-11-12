package config

import (
	"testing"
	"time"
)

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		input   string
		want    time.Time
		wantErr bool
	}{
		{name: "days", input: "7d", want: now.Add(-7 * 24 * time.Hour)},
		{name: "weeks", input: "2w", want: now.Add(-14 * 24 * time.Hour)},
		{name: "hours", input: "36h", want: now.Add(-36 * time.Hour)},
		{name: "minutes", input: "90m", want: now.Add(-90 * time.Minute)},
		{name: "all", input: "all", want: time.Time{}},
		{name: "zero", input: "0", want: time.Time{}},
		{name: "empty", input: "", want: time.Time{}},
		{name: "rfc3339", input: "2026-01-02T03:04:05Z", want: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
		{name: "bad unit", input: "7x", wantErr: true},
		{name: "negative", input: "-7d", wantErr: true},
		{name: "bad duration", input: "abc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSince(tt.input, now)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseSince(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSince(%q) error: %v", tt.input, err)
			}
			if !got.Equal(tt.want) {
				t.Fatalf("ParseSince(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
