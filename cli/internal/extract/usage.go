package extract

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// Usage is the token accounting reported by one CLI call. Providers differ:
// Claude reports input/output plus cache read/write tokens, while Codex may
// report a total or input/output with cached input as a subset of input.
// Total, when set, is authoritative; otherwise TotalTokens sums the parts.
type Usage struct {
	InputTokens              int     `json:"input_tokens,omitempty"`
	OutputTokens             int     `json:"output_tokens,omitempty"`
	CacheCreationInputTokens int     `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int     `json:"cache_read_input_tokens,omitempty"`
	Total                    int     `json:"total_tokens,omitempty"`
	CostUSD                  float64 `json:"cost_usd,omitempty"`
	Known                    bool    `json:"known"`
}

// TotalTokens returns the best available total. An explicit provider total wins
// because some providers count cached input as a subset of input tokens.
func (u Usage) TotalTokens() int {
	if u.Total > 0 {
		return u.Total
	}
	return u.InputTokens + u.OutputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
}

// Add accumulates another call's usage. Known stays false only when neither
// side reported anything, so a single unknown call does not erase known usage.
func (u *Usage) Add(other Usage) {
	if u == nil {
		return
	}
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	u.CacheCreationInputTokens += other.CacheCreationInputTokens
	u.CacheReadInputTokens += other.CacheReadInputTokens
	u.Total += other.Total
	u.CostUSD += other.CostUSD
	u.Known = u.Known || other.Known
}

type claudeUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

type claudeModelUsage struct {
	InputTokens              int     `json:"inputTokens"`
	OutputTokens             int     `json:"outputTokens"`
	CacheCreationInputTokens int     `json:"cacheCreationInputTokens"`
	CacheReadInputTokens     int     `json:"cacheReadInputTokens"`
	CostUSD                  float64 `json:"costUSD"`
}

func usageFromClaude(env claudeEnvelope) Usage {
	usage := Usage{
		InputTokens:              env.Usage.InputTokens,
		OutputTokens:             env.Usage.OutputTokens,
		CacheCreationInputTokens: env.Usage.CacheCreationInputTokens,
		CacheReadInputTokens:     env.Usage.CacheReadInputTokens,
		CostUSD:                  env.TotalCostUSD,
	}
	if usage.TotalTokens() > 0 {
		usage.Known = true
		return usage
	}
	// Some CLI versions only populate modelUsage. Sum the per-model entries.
	for _, item := range env.ModelUsage {
		usage.InputTokens += item.InputTokens
		usage.OutputTokens += item.OutputTokens
		usage.CacheCreationInputTokens += item.CacheCreationInputTokens
		usage.CacheReadInputTokens += item.CacheReadInputTokens
		usage.CostUSD += item.CostUSD
	}
	usage.Known = usage.TotalTokens() > 0 || usage.CostUSD > 0
	return usage
}

type codexUsage struct {
	InputTokens           int `json:"input_tokens"`
	CachedInputTokens     int `json:"cached_input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	ReasoningOutputTokens int `json:"reasoning_output_tokens"`
}

type codexUsageEvent struct {
	Type  string      `json:"type"`
	Usage *codexUsage `json:"usage"`
}

var tokensUsedPattern = regexp.MustCompile(`(?i)tokens\s+used[^0-9]*([0-9][0-9,]*)`)

// parseCodexUsage is best-effort. Current Codex invocations do not request
// --json, so usage may only appear as a "tokens used: N" line; when --json is
// used by a wrapper, turn.completed events carry a structured usage object.
func parseCodexUsage(stdout, stderr string) Usage {
	var usage Usage
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var event codexUsageEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil || event.Usage == nil {
			continue
		}
		if !strings.Contains(event.Type, "turn.completed") && !strings.Contains(event.Type, "token_count") {
			continue
		}
		call := Usage{
			InputTokens:          event.Usage.InputTokens,
			OutputTokens:         event.Usage.OutputTokens,
			CacheReadInputTokens: event.Usage.CachedInputTokens,
		}
		// OpenAI counts cached input inside input_tokens, so the explicit total
		// must not add it again.
		call.Total = event.Usage.InputTokens + event.Usage.OutputTokens
		if call.Total == 0 {
			call.Known = false
		} else {
			call.Known = true
		}
		usage.Add(call)
	}
	if usage.Known {
		return usage
	}

	for _, text := range []string{stderr, stdout} {
		for _, match := range tokensUsedPattern.FindAllStringSubmatch(text, -1) {
			total, err := strconv.Atoi(strings.ReplaceAll(match[1], ",", ""))
			if err != nil || total <= 0 {
				continue
			}
			usage.Add(Usage{Total: total, Known: true})
		}
	}
	return usage
}
