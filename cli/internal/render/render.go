// Package render converts a knowledge Doc into portable Markdown with a YAML
// front matter. It is intentionally independent of the cache so it can be
// tested with plain documents.
package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"devlog-learn/internal/extract"
)

// Plan assigns a deterministic, collision-free relative path to each document.
// Paths always use forward slashes so index JSON is portable across Windows and
// Unix hosts.
type Plan struct {
	Doc  *extract.Doc
	Path string
}

var windowsReserved = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// SafeProject turns a project name into one filesystem-safe path component.
func SafeProject(project string) string {
	return safeComponent(project, "unknown-project")
}

// DocPath returns "<project>/<date>-<slug>.md" with forward slashes.
func DocPath(doc *extract.Doc) string {
	if doc == nil {
		return ""
	}
	slug := strings.TrimSpace(doc.Slug)
	if slug == "" {
		slug = extract.MakeSlug(doc.Title, doc.OccurredAt)
	}
	return SafeProject(doc.Project) + "/" + safeComponent(slug, "untitled") + ".md"
}

// PlanDocs sorts documents by ID and disambiguates duplicate output paths.
// Rendering and indexing must both use this so links always resolve.
func PlanDocs(docs []*extract.Doc) []Plan {
	ordered := make([]*extract.Doc, 0, len(docs))
	for _, doc := range docs {
		if doc != nil {
			ordered = append(ordered, doc)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].ID != ordered[j].ID {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].Title < ordered[j].Title
	})

	seen := make(map[string]bool, len(ordered))
	plans := make([]Plan, 0, len(ordered))
	for _, doc := range ordered {
		rel := DocPath(doc)
		if seen[rel] {
			base := strings.TrimSuffix(rel, ".md")
			suffix := shortID(doc.ID)
			candidate := base + "-" + suffix + ".md"
			for n := 2; seen[candidate]; n++ {
				candidate = fmt.Sprintf("%s-%s-%d.md", base, suffix, n)
			}
			rel = candidate
		}
		seen[rel] = true
		plans = append(plans, Plan{Doc: doc, Path: rel})
	}
	return plans
}

// Markdown renders one document. A nil document renders as an empty string.
func Markdown(doc *extract.Doc) string {
	if doc == nil {
		return ""
	}
	var b strings.Builder
	writeFrontMatter(&b, doc)
	b.WriteString("\n# ")
	b.WriteString(oneLine(doc.Title, "Untitled"))
	b.WriteString("\n\n")
	if summary := strings.TrimSpace(doc.Summary); summary != "" {
		b.WriteString(blockquote(summary))
		b.WriteString("\n")
	}

	writeProblem(&b, doc)
	writeRootCause(&b, doc)
	writeDiagnosis(&b, doc)
	writeCommands(&b, doc)
	writeSolution(&b, doc)
	writeInsights(&b, doc)
	writePitfalls(&b, doc)
	writeVerification(&b, doc)
	writeLessons(&b, doc)
	writeOpenQuestions(&b, doc)
	writeArtifacts(&b, doc)
	writeEvidence(&b, doc)
	writeUncertain(&b, doc)
	writeValidation(&b, doc)
	writeProvenance(&b, doc)
	return b.String()
}

func writeFrontMatter(b *strings.Builder, doc *extract.Doc) {
	b.WriteString("---\n")
	fmt.Fprintf(b, "schema_version: %d\n", doc.SchemaVersion)
	writeYAMLString(b, "id", doc.ID)
	writeYAMLString(b, "slug", doc.Slug)
	writeYAMLString(b, "title", doc.Title)
	writeYAMLString(b, "project", doc.Project)
	writeYAMLString(b, "source", doc.Source)
	writeYAMLString(b, "knowledge_type", doc.KnowledgeType)
	writeYAMLString(b, "status", doc.Status)
	writeYAMLString(b, "review_status", doc.ReviewStatus)
	writeYAMLString(b, "review_note", doc.ReviewNote)
	writeYAMLString(b, "lifecycle", doc.Lifecycle)
	writeYAMLString(b, "stale_reason", doc.StaleReason)
	if doc.StaledAt != nil {
		writeYAMLString(b, "staled_at", doc.StaledAt.UTC().Format(time.RFC3339))
	}
	if !doc.OccurredAt.IsZero() {
		writeYAMLString(b, "occurred_at", doc.OccurredAt.UTC().Format(time.RFC3339))
	}
	if !doc.EndedAt.IsZero() {
		writeYAMLString(b, "ended_at", doc.EndedAt.UTC().Format(time.RFC3339))
	}
	fmt.Fprintf(b, "duration_minutes: %d\n", doc.DurationMin)
	writeYAMLString(b, "difficulty", doc.Difficulty)
	fmt.Fprintf(b, "confidence: %s\n", formatFloat(doc.Confidence))
	writeYAMLList(b, "tags", doc.Tags)
	writeYAMLList(b, "stack", doc.Stack)
	fmt.Fprintf(b, "evidence_checked: %d\n", doc.Validation.EvidenceChecked)
	fmt.Fprintf(b, "evidence_verified: %d\n", doc.Validation.EvidenceVerified)
	b.WriteString("---\n")
}

func writeProblem(b *strings.Builder, doc *extract.Doc) {
	problem := doc.Problem
	if strings.TrimSpace(problem.Statement) == "" && len(problem.Symptoms) == 0 && len(problem.ErrorSignatures) == 0 {
		return
	}
	b.WriteString("\n## Problem\n\n")
	if statement := strings.TrimSpace(problem.Statement); statement != "" {
		b.WriteString(statement)
		b.WriteString("\n\n")
	}
	writeBulletSection(b, "Symptoms", problem.Symptoms)
	if len(problem.ErrorSignatures) > 0 {
		b.WriteString("### Error signatures\n\n")
		b.WriteString(fenced("text", strings.Join(problem.ErrorSignatures, "\n")))
		b.WriteString("\n")
	}
	writeEvidenceRefs(b, problem.Evidence)
}

func writeRootCause(b *strings.Builder, doc *extract.Doc) {
	cause := doc.RootCause
	if strings.TrimSpace(cause.Text) == "" && !cause.Known {
		return
	}
	b.WriteString("\n## Root Cause\n\n")
	text := strings.TrimSpace(cause.Text)
	if text == "" {
		text = "_Not established._"
	}
	b.WriteString(text)
	b.WriteString("\n\n")
	if cause.Known {
		b.WriteString("Known: yes\n\n")
	} else {
		b.WriteString("Known: no\n\n")
	}
	if cause.Confidence != "" && cause.Confidence != "unknown" {
		fmt.Fprintf(b, "Confidence: %s\n\n", cause.Confidence)
	}
	writeEvidenceRefs(b, cause.Evidence)
}

func writeDiagnosis(b *strings.Builder, doc *extract.Doc) {
	if len(doc.Diagnosis) == 0 {
		return
	}
	b.WriteString("\n## Diagnosis Path\n\n")
	for i, step := range doc.Diagnosis {
		order := step.Order
		if order <= 0 {
			order = i + 1
		}
		fmt.Fprintf(b, "%d. **%s**\n", order, oneLine(step.Observation, "Investigation step"))
		if hypothesis := strings.TrimSpace(step.Hypothesis); hypothesis != "" {
			fmt.Fprintf(b, "\n   - Hypothesis: %s\n", oneLine(hypothesis, ""))
		}
		if action := strings.TrimSpace(step.Action); action != "" {
			fmt.Fprintf(b, "   - Action: %s\n", oneLine(action, ""))
		}
		if command := strings.TrimSpace(step.Command); command != "" {
			kind := strings.TrimSpace(step.CommandKind)
			if kind == "" || kind == "unknown" {
				kind = "command"
			}
			fmt.Fprintf(b, "\n   `%s`:\n\n", kind)
			b.WriteString(indent(fenced("bash", command), "   "))
			b.WriteString("\n")
		}
		if signal := strings.TrimSpace(step.ExpectedSignal); signal != "" {
			fmt.Fprintf(b, "\n   - Expected signal: %s\n", oneLine(signal, ""))
		}
		if result := strings.TrimSpace(step.Result); result != "" {
			fmt.Fprintf(b, "   - Result: %s\n", oneLine(result, ""))
		}
		if conclusion := strings.TrimSpace(step.Conclusion); conclusion != "" {
			fmt.Fprintf(b, "   - Conclusion: %s\n", oneLine(conclusion, ""))
		}
		if refs := evidenceRefs(step.Evidence); refs != "" {
			fmt.Fprintf(b, "   - %s\n", refs)
		}
		b.WriteString("\n")
	}
}

func writeCommands(b *strings.Builder, doc *extract.Doc) {
	if len(doc.Commands) == 0 {
		return
	}
	b.WriteString("\n## Effective Commands\n\n")
	b.WriteString("> Only commands that advanced the investigation or are needed to reproduce/verify the fix.\n\n")
	for i, command := range doc.Commands {
		fmt.Fprintf(b, "### C%d. %s\n\n", i+1, oneLine(command.Purpose, "Command"))
		if kind := strings.TrimSpace(command.Kind); kind != "" && kind != "unknown" {
			fmt.Fprintf(b, "- Kind: %s\n", kind)
		}
		if risk := strings.TrimSpace(command.Risk); risk != "" {
			fmt.Fprintf(b, "- Risk: %s\n", oneLine(risk, ""))
		}
		if signal := strings.TrimSpace(command.ExpectedSignal); signal != "" {
			fmt.Fprintf(b, "- Expected signal: %s\n", oneLine(signal, ""))
		}
		if refs := evidenceRefs(command.Evidence); refs != "" {
			fmt.Fprintf(b, "- %s\n", refs)
		}
		b.WriteString("\n")
		b.WriteString(fenced("bash", command.Command))
		b.WriteString("\n")
	}
}

func writeSolution(b *strings.Builder, doc *extract.Doc) {
	if len(doc.Solution) == 0 {
		return
	}
	b.WriteString("\n## Solution\n\n")
	for i, step := range doc.Solution {
		order := step.Order
		if order <= 0 {
			order = i + 1
		}
		fmt.Fprintf(b, "%d. **%s**\n", order, oneLine(step.Action, "Step"))
		if detail := strings.TrimSpace(step.Detail); detail != "" {
			b.WriteString("\n   ")
			b.WriteString(strings.ReplaceAll(detail, "\n", "\n   "))
			b.WriteString("\n")
		}
		if command := strings.TrimSpace(step.Command); command != "" {
			b.WriteString("\n")
			b.WriteString(indent(fenced("bash", command), "   "))
		}
		if refs := evidenceRefs(step.Evidence); refs != "" {
			b.WriteString("\n   " + refs + "\n")
		}
		b.WriteString("\n")
	}
}

func writeInsights(b *strings.Builder, doc *extract.Doc) {
	if len(doc.Insights) == 0 {
		return
	}
	b.WriteString("\n## Insights\n\n")
	for _, insight := range doc.Insights {
		fmt.Fprintf(b, "- %s", oneLine(insight.Text, "(empty)"))
		if insight.Confidence != "" && insight.Confidence != "unknown" {
			fmt.Fprintf(b, " _(confidence: %s)_", insight.Confidence)
		}
		if refs := evidenceRefs(insight.Evidence); refs != "" {
			b.WriteString(" " + refs)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func writePitfalls(b *strings.Builder, doc *extract.Doc) {
	if len(doc.Pitfalls) == 0 {
		return
	}
	b.WriteString("\n## Pitfalls\n\n")
	for _, pitfall := range doc.Pitfalls {
		fmt.Fprintf(b, "- **%s**\n", oneLine(pitfall.Trap, "Pitfall"))
		if why := strings.TrimSpace(pitfall.Why); why != "" {
			fmt.Fprintf(b, "  - Why: %s\n", oneLine(why, ""))
		}
		if avoid := strings.TrimSpace(pitfall.Avoid); avoid != "" {
			fmt.Fprintf(b, "  - Avoid: %s\n", oneLine(avoid, ""))
		}
		if refs := evidenceRefs(pitfall.Evidence); refs != "" {
			fmt.Fprintf(b, "  - %s\n", refs)
		}
	}
	b.WriteString("\n")
}

func writeVerification(b *strings.Builder, doc *extract.Doc) {
	if len(doc.Verification) == 0 {
		return
	}
	b.WriteString("\n## Verification\n\n")
	b.WriteString("| Status | Check | Expected | Observed |\n")
	b.WriteString("|---|---|---|---|\n")
	for _, check := range doc.Verification {
		fmt.Fprintf(b, "| %s | %s | %s | %s |\n",
			tableCell(check.Status), tableCell(check.Check), tableCell(check.Expected), tableCell(check.Observed))
	}
	b.WriteString("\n")
}

func writeLessons(b *strings.Builder, doc *extract.Doc) {
	writeBulletSection(b, "Lessons", doc.Lessons)
}

func writeOpenQuestions(b *strings.Builder, doc *extract.Doc) {
	writeBulletSection(b, "Open Questions", doc.OpenQuestions)
}

func writeArtifacts(b *strings.Builder, doc *extract.Doc) {
	artifacts := doc.Artifacts
	if len(artifacts.Files) == 0 && len(artifacts.Commands) == 0 && len(artifacts.URLs) == 0 {
		return
	}
	b.WriteString("\n## Artifacts\n\n")
	writeBulletSection(b, "Files", artifacts.Files)
	writeBulletSection(b, "Commands", artifacts.Commands)
	writeBulletSection(b, "URLs", artifacts.URLs)
}

func writeEvidence(b *strings.Builder, doc *extract.Doc) {
	if len(doc.Evidence) == 0 {
		return
	}
	b.WriteString("\n## Evidence\n\n")
	b.WriteString("| Seq | Kind | Verified | Quote |\n")
	b.WriteString("|---:|---|---|---|\n")
	for _, ev := range doc.Evidence {
		verified := "no"
		if ev.Verified {
			verified = "yes"
		}
		fmt.Fprintf(b, "| %d | %s | %s | %s |\n", ev.EventSeq, tableCell(ev.Kind), verified, tableCell(ev.Quote))
	}
	b.WriteString("\n")
}

func writeUncertain(b *strings.Builder, doc *extract.Doc) {
	if len(doc.Uncertain) == 0 {
		return
	}
	b.WriteString("\n## Uncertainties\n\n")
	for _, item := range doc.Uncertain {
		known := "unresolved"
		if item.Known {
			known = "resolved"
		}
		fmt.Fprintf(b, "- **%s** (%s): %s\n", oneLine(item.Field, "field"), known, oneLine(item.Reason, ""))
	}
	b.WriteString("\n")
}

func writeValidation(b *strings.Builder, doc *extract.Doc) {
	if len(doc.Validation.Warnings) == 0 {
		return
	}
	b.WriteString("\n## Validation\n\n")
	for _, warning := range doc.Validation.Warnings {
		fmt.Fprintf(b, "- %s\n", oneLine(warning, ""))
	}
	b.WriteString("\n")
}

func writeProvenance(b *strings.Builder, doc *extract.Doc) {
	prov := doc.Provenance
	b.WriteString("\n## Provenance\n\n")
	writeProvisionLine(b, "Provider", prov.Provider)
	writeProvisionLine(b, "Model", prov.Model)
	writeProvisionLine(b, "CLI version", prov.CLIVersion)
	writeProvisionLine(b, "Session", prov.SessionID)
	writeProvisionLine(b, "Segment", prov.SegmentID)
	writeProvisionLine(b, "Segment hash", prov.SegmentHash)
	if prov.EventRange != [2]int{} {
		fmt.Fprintf(b, "- Event range: %d-%d\n", prov.EventRange[0], prov.EventRange[1])
	}
	writeProvisionLine(b, "Prompt", prov.PromptVersion)
	writeProvisionLine(b, "Cache key", prov.CacheKey)
	if !prov.ExtractedAt.IsZero() {
		fmt.Fprintf(b, "- Extracted: %s\n", prov.ExtractedAt.UTC().Format(time.RFC3339))
	}
	b.WriteString("\n")
}

func writeProvisionLine(b *strings.Builder, label, value string) {
	if value = strings.TrimSpace(value); value != "" {
		fmt.Fprintf(b, "- %s: %s\n", label, value)
	}
}

func writeBulletSection(b *strings.Builder, title string, values []string) {
	clean := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			clean = append(clean, value)
		}
	}
	if len(clean) == 0 {
		return
	}
	fmt.Fprintf(b, "### %s\n\n", title)
	for _, value := range clean {
		fmt.Fprintf(b, "- %s\n", oneLine(value, ""))
	}
	b.WriteString("\n")
}

func writeEvidenceRefs(b *strings.Builder, seqs []int) {
	if refs := evidenceRefs(seqs); refs != "" {
		b.WriteString(refs)
		b.WriteString("\n\n")
	}
}

func evidenceRefs(seqs []int) string {
	if len(seqs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(seqs))
	for _, seq := range seqs {
		parts = append(parts, strconv.Itoa(seq))
	}
	return "_Evidence: " + strings.Join(parts, ", ") + "._"
}

func blockquote(text string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		b.WriteString("> ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func fenced(language, content string) string {
	content = strings.TrimRight(content, "\n")
	longest, run := 0, 0
	for _, r := range content {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}
			continue
		}
		run = 0
	}
	count := 3
	if longest >= count {
		count = longest + 1
	}
	fence := strings.Repeat("`", count)
	return fence + language + "\n" + content + "\n" + fence + "\n"
}

func indent(text, prefix string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i := range lines {
		if lines[i] != "" {
			lines[i] = prefix + lines[i]
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func tableCell(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\n", "<br>")
	value = strings.ReplaceAll(value, "|", `\|`)
	if value == "" {
		return "-"
	}
	return value
}

func oneLine(value, fallback string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return fallback
	}
	return value
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func writeYAMLString(b *strings.Builder, key, value string) {
	if value = strings.TrimSpace(value); value == "" {
		return
	}
	fmt.Fprintf(b, "%s: %s\n", key, yamlQuote(value))
}

func writeYAMLList(b *strings.Builder, key string, values []string) {
	clean := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			clean = append(clean, value)
		}
	}
	if len(clean) == 0 {
		return
	}
	fmt.Fprintf(b, "%s:\n", key)
	for _, value := range clean {
		fmt.Fprintf(b, "  - %s\n", yamlQuote(value))
	}
}

// yamlQuote emits a JSON double-quoted string, which is also a valid YAML
// double-quoted scalar. HTML escaping is disabled so CJK and punctuation stay
// readable in the front matter.
func yamlQuote(value string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(value)
	return strings.TrimSuffix(buf.String(), "\n")
}

// safeComponent keeps letters, digits, '-' and '_'; everything else becomes
// '-'. The result is never empty and avoids Windows reserved device names.
func safeComponent(value, fallback string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.TrimSpace(value) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			b.WriteRune(r)
			lastDash = false
		case r == '-':
			if !lastDash {
				b.WriteRune('-')
				lastDash = true
			}
		default:
			if !lastDash {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	runes := []rune(out)
	if len(runes) > 80 {
		out = strings.Trim(string(runes[:80]), "-")
	}
	if out == "" {
		out = fallback
	}
	if windowsReserved[strings.ToLower(out)] {
		out = "_" + out
	}
	return out
}

func shortID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	runes := []rune(id)
	if len(runes) > 8 {
		runes = runes[:8]
	}
	out := safeComponent(string(runes), "doc")
	return out
}
