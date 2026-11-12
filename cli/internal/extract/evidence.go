package extract

import (
	"encoding/json"
	"fmt"
	"strings"

	"devlog-learn/internal/ingest"
	"devlog-learn/internal/segment"
)

// ValidateEvidence enforces the rule that conclusions must be traceable to
// events the model actually saw. included may be nil to allow every event in
// the segment; when provided, omitted events are not valid evidence sources.
func ValidateEvidence(d *Doc, seg segment.Segment, included map[int]bool) {
	if d == nil {
		return
	}
	d.Validation.EvidenceChecked = 0
	d.Validation.EvidenceVerified = 0

	events := make(map[int]ingest.Event, len(seg.Events))
	for _, ev := range seg.Events {
		events[ev.Seq] = ev
	}

	valid := make([]Evidence, 0, len(d.Evidence))
	bySeq := map[int]Evidence{}
	verifiedSeq := map[int]bool{}
	degraded := false
	severe := false
	for _, ev := range d.Evidence {
		source, ok := events[ev.EventSeq]
		if !ok || (included != nil && !included[ev.EventSeq]) {
			d.addValidationWarning("evidence references unknown or omitted event seq %d; dropped", ev.EventSeq)
			degraded = true
			continue
		}
		ev.Quote = strings.TrimSpace(ev.Quote)
		if ev.Quote == "" {
			d.addValidationWarning("evidence seq %d has an empty quote", ev.EventSeq)
			continue
		}
		if quoteMatches(ev.Quote, source) {
			ev.Verified = true
			verifiedSeq[ev.EventSeq] = true
		} else {
			d.addValidationWarning("evidence quote does not match event seq %d", ev.EventSeq)
			degraded = true
		}
		valid = append(valid, ev)
		bySeq[ev.EventSeq] = ev
	}
	d.Evidence = valid

	filter := func(field string, seqs []int) []int {
		out := make([]int, 0, len(seqs))
		seen := map[int]bool{}
		invalid := false
		for _, seq := range seqs {
			if seen[seq] {
				continue
			}
			seen[seq] = true
			if _, ok := bySeq[seq]; !ok {
				invalid = true
				continue
			}
			out = append(out, seq)
		}
		if invalid {
			d.addValidationWarning("%s lost one or more evidence references that were not in the evidence table", field)
			addUncertainty(d, field, "one or more evidence references were invalid")
			degraded = true
		}
		return out
	}

	d.Problem.Evidence = filter("problem", d.Problem.Evidence)
	d.RootCause.Evidence = filter("root_cause", d.RootCause.Evidence)
	for i := range d.Solution {
		d.Solution[i].Evidence = filter("solution", d.Solution[i].Evidence)
	}
	for i := range d.Diagnosis {
		d.Diagnosis[i].Evidence = filter("diagnosis", d.Diagnosis[i].Evidence)
	}
	for i := range d.Commands {
		d.Commands[i].Evidence = filter("commands", d.Commands[i].Evidence)
	}
	for i := range d.Concepts {
		d.Concepts[i].Evidence = filter("concepts", d.Concepts[i].Evidence)
	}
	for i := range d.RejectedHypotheses {
		d.RejectedHypotheses[i].Evidence = filter("rejected_hypotheses", d.RejectedHypotheses[i].Evidence)
	}
	for i := range d.BlindSpots {
		d.BlindSpots[i].Evidence = filter("blind_spots", d.BlindSpots[i].Evidence)
	}
	for i := range d.Insights {
		d.Insights[i].Evidence = filter("insights", d.Insights[i].Evidence)
	}
	for i := range d.Pitfalls {
		d.Pitfalls[i].Evidence = filter("pitfalls", d.Pitfalls[i].Evidence)
	}
	for i := range d.Verification {
		d.Verification[i].Evidence = filter("verification", d.Verification[i].Evidence)
	}

	if len(d.Evidence) == 0 {
		degraded = true
		d.addValidationWarning("document has no evidence entries")
	}
	for i := range d.Solution {
		if len(d.Solution[i].Evidence) == 0 {
			degraded = true
			d.addValidationWarning("solution step %d has no evidence", i+1)
			addUncertainty(d, "solution", fmt.Sprintf("step %d has no supporting evidence", i+1))
		}
	}
	for i := range d.Insights {
		if len(d.Insights[i].Evidence) == 0 {
			degraded = true
			d.addValidationWarning("insight %d has no evidence", i+1)
			addUncertainty(d, "insights", fmt.Sprintf("insight %d has no supporting evidence", i+1))
		}
	}
	for i := range d.Diagnosis {
		if len(d.Diagnosis[i].Evidence) == 0 {
			degraded = true
			d.addValidationWarning("diagnosis step %d has no evidence", i+1)
			addUncertainty(d, "diagnosis", fmt.Sprintf("diagnosis step %d has no supporting evidence", i+1))
		}
	}
	for i := range d.Commands {
		if len(d.Commands[i].Evidence) == 0 {
			degraded = true
			d.addValidationWarning("command %d (%s) has no evidence", i+1, d.Commands[i].Command)
			addUncertainty(d, "commands", fmt.Sprintf("command %d has no supporting evidence", i+1))
		}
	}
	// Learning extras are optional. Missing evidence only degrades the document;
	// it never turns an otherwise valid extraction into a failure.
	for i := range d.Concepts {
		if len(d.Concepts[i].Evidence) == 0 {
			degraded = true
			d.addValidationWarning("concept %d (%s) has no evidence", i+1, d.Concepts[i].Name)
			addUncertainty(d, "concepts", fmt.Sprintf("concept %d has no supporting evidence", i+1))
		}
	}
	for i := range d.RejectedHypotheses {
		if len(d.RejectedHypotheses[i].Evidence) == 0 {
			degraded = true
			d.addValidationWarning("rejected hypothesis %d has no evidence", i+1)
			addUncertainty(d, "rejected_hypotheses", fmt.Sprintf("rejected hypothesis %d has no supporting evidence", i+1))
		}
	}
	for i := range d.BlindSpots {
		if len(d.BlindSpots[i].Evidence) == 0 {
			degraded = true
			d.addValidationWarning("blind spot %d has no evidence", i+1)
			addUncertainty(d, "blind_spots", fmt.Sprintf("blind spot %d has no supporting evidence", i+1))
		}
	}

	if d.RootCause.Known && (len(d.RootCause.Evidence) == 0 || strings.TrimSpace(d.RootCause.Text) == "" || !hasVerifiedRef(d.RootCause.Evidence, verifiedSeq)) {
		d.RootCause.Known = false
		d.addValidationWarning("root cause was marked known without sufficient evidence; downgraded to unknown")
		addUncertainty(d, "root_cause", "root cause was not supported by evidence")
		severe = true
	}

	passed := 0
	for i := range d.Verification {
		v := &d.Verification[i]
		switch v.Status {
		case "passed":
			if !hasVerifiedSuccess(v.Evidence, events, verifiedSeq) {
				v.Status = "unknown"
				d.addValidationWarning("verification %q claimed passed without a proven zero exit; downgraded to unknown", v.Check)
				addUncertainty(d, "verification", "verification success was not proven by tool execution")
				severe = true
			} else {
				passed++
			}
		case "failed":
			if !hasVerifiedFailure(v.Evidence, events, verifiedSeq) {
				v.Status = "unknown"
				d.addValidationWarning("verification %q claimed failed without a proven error outcome; downgraded to unknown", v.Check)
				addUncertainty(d, "verification", "verification failure was not proven by tool execution")
				severe = true
			}
		}
	}

	if d.Status == "resolved" && passed == 0 {
		d.Status = "unknown"
		d.addValidationWarning("document claimed resolved without a passed verification; downgraded to unknown")
		addUncertainty(d, "status", "resolution was not supported by a passed verification")
		severe = true
	}

	d.Validation.EvidenceChecked = len(d.Evidence)
	for _, ev := range d.Evidence {
		if ev.Verified {
			d.Validation.EvidenceVerified++
		}
	}
	if severe && d.Confidence > 0.4 {
		d.Confidence = 0.4
		d.addValidationWarning("confidence capped at 0.4 because strong claims were downgraded")
	} else if degraded && d.Confidence > 0.6 {
		d.Confidence = 0.6
		d.addValidationWarning("confidence capped at 0.6 because evidence was incomplete or unverified")
	}
	if d.Validation.EvidenceChecked > 0 {
		ratio := float64(d.Validation.EvidenceVerified) / float64(d.Validation.EvidenceChecked)
		switch {
		case ratio == 0 && d.Confidence > 0.4:
			d.Confidence = 0.4
			d.addValidationWarning("confidence capped at 0.4 because no evidence quote was verified")
		case ratio < 0.5 && d.Confidence > 0.6:
			d.Confidence = 0.6
			d.addValidationWarning("confidence capped at 0.6 because fewer than half of evidence quotes were verified")
		}
	}

	d.Uncertain = dedupeUncertain(d.Uncertain)
}

func hasVerifiedRef(seqs []int, verified map[int]bool) bool {
	for _, seq := range seqs {
		if verified[seq] {
			return true
		}
	}
	return false
}

func hasVerifiedSuccess(seqs []int, events map[int]ingest.Event, verified map[int]bool) bool {
	for _, seq := range seqs {
		ev, ok := events[seq]
		if !ok || !verified[seq] || ev.Exec == nil {
			continue
		}
		if ev.Exec.Known && ev.Exec.ExitCode != nil && *ev.Exec.ExitCode == 0 &&
			!ev.Exec.TimedOut && !ev.Exec.Canceled {
			return true
		}
	}
	return false
}

func hasVerifiedFailure(seqs []int, events map[int]ingest.Event, verified map[int]bool) bool {
	for _, seq := range seqs {
		ev, ok := events[seq]
		if !ok || !verified[seq] {
			continue
		}
		if ev.IsError {
			return true
		}
		if ev.Exec == nil {
			continue
		}
		if ev.Exec.TimedOut || ev.Exec.Canceled {
			return true
		}
		if ev.Exec.Known && ev.Exec.ExitCode != nil && *ev.Exec.ExitCode != 0 {
			return true
		}
	}
	return false
}

func quoteMatches(quote string, ev ingest.Event) bool {
	needle := normalizeForMatch(quote)
	if needle == "" {
		return false
	}
	for _, text := range eventTexts(ev) {
		if strings.Contains(normalizeForMatch(text), needle) {
			return true
		}
	}
	return false
}

// eventTexts returns the searchable representations of one event. ToolInput is
// raw JSON, where newlines are escaped as literal \n; models quote the decoded
// code, so each JSON string value is also a candidate. Candidates are matched
// separately, never concatenated, so a quote cannot span unrelated fields.
func eventTexts(ev ingest.Event) []string {
	parts := []string{ev.Text, ev.ToolResult, ev.ToolInputText}
	if len(ev.ToolInput) > 0 {
		parts = append(parts, string(ev.ToolInput))
		parts = append(parts, jsonStringValues(ev.ToolInput)...)
	}
	if ev.Exec != nil {
		parts = append(parts, ev.Exec.Stdout, ev.Exec.Stderr)
	}
	out := parts[:0]
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			out = append(out, part)
		}
	}
	return out
}

// jsonStringValues decodes every string value in a JSON document. Order is
// irrelevant for substring matching, so maps may be walked in any order.
func jsonStringValues(raw json.RawMessage) []string {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch typed := v.(type) {
		case string:
			out = append(out, typed)
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(value)
	return out
}

func normalizeForMatch(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

func addUncertainty(d *Doc, field, reason string) {
	d.Uncertain = append(d.Uncertain, Uncertainty{Field: field, Reason: reason, Known: false})
}
