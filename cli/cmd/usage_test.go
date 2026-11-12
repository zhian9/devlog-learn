package main

import (
	"bytes"
	"strings"
	"testing"

	"devlog-learn/internal/extract"
	"devlog-learn/internal/pipeline"
)

func TestFormatUsage(t *testing.T) {
	tests := []struct {
		name  string
		usage extract.Usage
		want  []string
	}{
		{
			name: "claude style",
			usage: extract.Usage{
				InputTokens: 100, OutputTokens: 20,
				CacheCreationInputTokens: 5, CacheReadInputTokens: 1000,
				CostUSD: 0.0123, Known: true,
			},
			want: []string{"in=100", "out=20", "cache_write=5", "cache_read=1000", "total=1125", "cost_usd=0.0123"},
		},
		{
			name:  "total only",
			usage: extract.Usage{Total: 1234, Known: true},
			want:  []string{"total=1234"},
		},
		{
			name:  "unknown",
			usage: extract.Usage{},
			want:  []string{"unknown"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatUsage(tt.usage)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Fatalf("formatUsage = %q, want %q", got, want)
				}
			}
		})
	}
}

func TestSummarizeUsage(t *testing.T) {
	noCalls := &pipeline.RunResult{}
	if got := summarizeUsage(noCalls); !strings.Contains(got, "no model calls") {
		t.Fatalf("no calls = %q", got)
	}

	unknown := &pipeline.RunResult{Extracted: 2}
	if got := summarizeUsage(unknown); !strings.Contains(got, "unknown") {
		t.Fatalf("unknown = %q", got)
	}

	partial := &pipeline.RunResult{
		Extracted:           2,
		Usage:               extract.Usage{InputTokens: 10, OutputTokens: 5, Known: true},
		UsageUnknownReports: 1,
	}
	if got := summarizeUsage(partial); !strings.Contains(got, "total=15") || !strings.Contains(got, "1 extracted segment(s)") {
		t.Fatalf("partial = %q", got)
	}
}

func TestPrintExtractionSummaryIncludesUsage(t *testing.T) {
	var buf bytes.Buffer
	printExtractionSummary(&buf, &pipeline.RunResult{
		Extracted: 1,
		Usage:     extract.Usage{InputTokens: 7, OutputTokens: 3, Known: true},
	})
	out := buf.String()
	if !strings.Contains(out, "summary:") || !strings.Contains(out, "tokens: in=7 out=3 total=10") {
		t.Fatalf("summary = %q", out)
	}
}
