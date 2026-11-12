package render

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"devlog-learn/internal/extract"
)

// LearnMarkdown renders a human-facing, Chinese learning template. It is a
// separate profile from Markdown so the agent-facing Markdown contract stays
// unchanged. Sections with no content are omitted, so old Docs without the
// learning fields do not gain empty headings.
func LearnMarkdown(doc *extract.Doc) string {
	if doc == nil {
		return ""
	}
	var b strings.Builder
	writeFrontMatter(&b, doc)
	b.WriteString("\n# ")
	b.WriteString(oneLine(doc.Title, "未命名案例"))
	b.WriteString("\n\n")
	if summary := strings.TrimSpace(doc.Summary); summary != "" {
		b.WriteString(blockquote(summary))
		b.WriteString("\n")
	}
	if value := strings.TrimSpace(doc.LearningValue); value != "" {
		fmt.Fprintf(&b, "**学习价值**：%s\n\n", learningValueLabel(value))
	}

	writeLearnSymptoms(&b, doc)
	writeLearnThinkFirst(&b, doc)
	writeLearnDiagnosis(&b, doc)
	writeLearnRejected(&b, doc)
	writeLearnRootCause(&b, doc)
	writeLearnSolution(&b, doc)
	writeLearnVerification(&b, doc)
	writeLearnTransferable(&b, doc)
	writeLearnConcepts(&b, doc)
	writeLearnBlindSpots(&b, doc)
	writeLearnChecklist(&b, doc)
	writeLearnSelfTest(&b, doc)
	writeLearnEvidence(&b, doc)
	return b.String()
}

func writeLearnSymptoms(b *strings.Builder, doc *extract.Doc) {
	problem := doc.Problem
	if strings.TrimSpace(problem.Statement) == "" && len(problem.Symptoms) == 0 && len(problem.ErrorSignatures) == 0 {
		return
	}
	b.WriteString("\n## 症状\n\n")
	if statement := strings.TrimSpace(problem.Statement); statement != "" {
		b.WriteString(statement)
		b.WriteString("\n\n")
	}
	if len(problem.Symptoms) > 0 {
		b.WriteString("**表现**\n\n")
		for _, symptom := range problem.Symptoms {
			fmt.Fprintf(b, "- %s\n", oneLine(symptom, ""))
		}
		b.WriteString("\n")
	}
	if len(problem.ErrorSignatures) > 0 {
		b.WriteString("**错误信息**\n\n")
		b.WriteString(fenced("text", strings.Join(problem.ErrorSignatures, "\n")))
		b.WriteString("\n")
	}
	writeLearnEvidenceRefs(b, problem.Evidence)
}

func writeLearnThinkFirst(b *strings.Builder, doc *extract.Doc) {
	if len(doc.Diagnosis) == 0 && strings.TrimSpace(doc.Problem.Statement) == "" && len(doc.Problem.Symptoms) == 0 {
		return
	}
	b.WriteString("\n## 先自己想\n\n")
	b.WriteString("先不要往下翻。只看上面的症状，尝试回答：\n\n")
	b.WriteString("1. 你会先验证哪两个假设？为什么是它们？\n")
	b.WriteString("2. 哪个命令能以最小代价确认或排除它们？\n")
	b.WriteString("3. 如果输出符合预期或不符合预期，下一步分别是什么？\n\n")
	b.WriteString("写下你的答案后，再对照「定位路径」和「根因」，看看差在哪里。\n")
}

func writeLearnDiagnosis(b *strings.Builder, doc *extract.Doc) {
	if len(doc.Diagnosis) == 0 && len(doc.Commands) == 0 {
		return
	}
	b.WriteString("\n## 定位路径\n\n")
	for i, step := range doc.Diagnosis {
		order := step.Order
		if order <= 0 {
			order = i + 1
		}
		fmt.Fprintf(b, "### 步骤 %d：%s\n\n", order, oneLine(step.Observation, "定位"))
		if hypothesis := strings.TrimSpace(step.Hypothesis); hypothesis != "" {
			fmt.Fprintf(b, "- **假设**：%s\n", oneLine(hypothesis, ""))
		}
		if action := strings.TrimSpace(step.Action); action != "" {
			fmt.Fprintf(b, "- **动作**：%s\n", oneLine(action, ""))
		}
		if command := strings.TrimSpace(step.Command); command != "" {
			b.WriteString("- **命令**：\n\n")
			b.WriteString(indent(fenced("bash", command), "  "))
			b.WriteString("\n")
		}
		if signal := strings.TrimSpace(step.ExpectedSignal); signal != "" {
			fmt.Fprintf(b, "- **期望信号**：%s\n", oneLine(signal, ""))
		}
		if result := strings.TrimSpace(step.Result); result != "" {
			fmt.Fprintf(b, "- **实际结果**：%s\n", oneLine(result, ""))
		}
		if conclusion := strings.TrimSpace(step.Conclusion); conclusion != "" {
			fmt.Fprintf(b, "- **结论**：%s\n", oneLine(conclusion, ""))
		}
		if refs := learnEvidenceRefs(step.Evidence); refs != "" {
			fmt.Fprintf(b, "- %s\n", refs)
		}
		b.WriteString("\n")
	}

	if len(doc.Commands) > 0 {
		b.WriteString("### 有效命令（C1..Cn）\n\n")
		b.WriteString("> 只保留改变/确认假设、复现问题或验证修复的命令。\n\n")
		for i, command := range doc.Commands {
			fmt.Fprintf(b, "#### C%d. %s\n\n", i+1, oneLine(command.Purpose, "命令"))
			if kind := strings.TrimSpace(command.Kind); kind != "" && kind != "unknown" {
				fmt.Fprintf(b, "- 类型：%s\n", kind)
			}
			if risk := strings.TrimSpace(command.Risk); risk != "" {
				fmt.Fprintf(b, "- 风险：%s\n", oneLine(risk, ""))
			}
			if signal := strings.TrimSpace(command.ExpectedSignal); signal != "" {
				fmt.Fprintf(b, "- 期望信号：%s\n", oneLine(signal, ""))
			}
			if refs := learnEvidenceRefs(command.Evidence); refs != "" {
				fmt.Fprintf(b, "- %s\n", refs)
			}
			b.WriteString("\n")
			b.WriteString(fenced("bash", command.Command))
			b.WriteString("\n")
		}
	}
}

func writeLearnRejected(b *strings.Builder, doc *extract.Doc) {
	if len(doc.RejectedHypotheses) == 0 {
		return
	}
	b.WriteString("\n## 被排除的假设\n\n")
	for _, rejected := range doc.RejectedHypotheses {
		fmt.Fprintf(b, "- **%s**\n", oneLine(rejected.Hypothesis, "未命名假设"))
		if why := strings.TrimSpace(rejected.WhyRejected); why != "" {
			fmt.Fprintf(b, "  - 为什么排除：%s\n", oneLine(why, ""))
		}
		if refs := learnEvidenceRefs(rejected.Evidence); refs != "" {
			fmt.Fprintf(b, "  - %s\n", refs)
		}
	}
	b.WriteString("\n")
}

func writeLearnRootCause(b *strings.Builder, doc *extract.Doc) {
	cause := doc.RootCause
	if strings.TrimSpace(cause.Text) == "" && !cause.Known {
		return
	}
	b.WriteString("\n## 根因\n\n")
	text := strings.TrimSpace(cause.Text)
	if text == "" {
		text = "_尚未确定。_"
	}
	b.WriteString(text)
	b.WriteString("\n\n")
	if cause.Known {
		b.WriteString("- 是否确认：是\n")
	} else {
		b.WriteString("- 是否确认：否\n")
	}
	if cause.Confidence != "" && cause.Confidence != "unknown" {
		fmt.Fprintf(b, "- 置信度：%s\n", confidenceLabel(cause.Confidence))
	}
	if refs := learnEvidenceRefs(cause.Evidence); refs != "" {
		fmt.Fprintf(b, "- %s\n", refs)
	}
	b.WriteString("\n")
}

func writeLearnSolution(b *strings.Builder, doc *extract.Doc) {
	if len(doc.Solution) == 0 {
		return
	}
	b.WriteString("\n## 修复\n\n")
	for i, step := range doc.Solution {
		order := step.Order
		if order <= 0 {
			order = i + 1
		}
		fmt.Fprintf(b, "%d. **%s**\n", order, oneLine(step.Action, "修复步骤"))
		if detail := strings.TrimSpace(step.Detail); detail != "" {
			b.WriteString("\n   ")
			b.WriteString(strings.ReplaceAll(detail, "\n", "\n   "))
			b.WriteString("\n")
		}
		if command := strings.TrimSpace(step.Command); command != "" {
			b.WriteString("\n")
			b.WriteString(indent(fenced("bash", command), "   "))
		}
		if refs := learnEvidenceRefs(step.Evidence); refs != "" {
			b.WriteString("\n   " + refs + "\n")
		}
		b.WriteString("\n")
	}
}

func writeLearnVerification(b *strings.Builder, doc *extract.Doc) {
	if len(doc.Verification) == 0 {
		return
	}
	b.WriteString("\n## 验证\n\n")
	b.WriteString("| 状态 | 检查 | 期望 | 实际 |\n")
	b.WriteString("|---|---|---|---|\n")
	for _, check := range doc.Verification {
		fmt.Fprintf(b, "| %s | %s | %s | %s |\n",
			tableCell(verificationStatusLabel(check.Status)), tableCell(check.Check),
			tableCell(check.Expected), tableCell(check.Observed))
	}
	b.WriteString("\n")
}

func writeLearnTransferable(b *strings.Builder, doc *extract.Doc) {
	if len(doc.Insights) == 0 && len(doc.Lessons) == 0 {
		return
	}
	b.WriteString("\n## 可迁移方法\n\n")
	if len(doc.Insights) > 0 {
		b.WriteString("### 洞察\n\n")
		for _, insight := range doc.Insights {
			fmt.Fprintf(b, "- %s", oneLine(insight.Text, "(空)"))
			if insight.Confidence != "" && insight.Confidence != "unknown" {
				fmt.Fprintf(b, " _（置信度：%s）_", confidenceLabel(insight.Confidence))
			}
			if refs := learnEvidenceRefs(insight.Evidence); refs != "" {
				b.WriteString(" " + refs)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if len(doc.Lessons) > 0 {
		b.WriteString("### 经验\n\n")
		for _, lesson := range doc.Lessons {
			fmt.Fprintf(b, "- %s\n", oneLine(lesson, ""))
		}
		b.WriteString("\n")
	}
}

func writeLearnConcepts(b *strings.Builder, doc *extract.Doc) {
	if len(doc.Concepts) == 0 {
		return
	}
	b.WriteString("\n## 知识点卡片\n\n")
	for _, concept := range doc.Concepts {
		fmt.Fprintf(b, "### %s\n\n", oneLine(concept.Name, "未命名概念"))
		if stack := strings.TrimSpace(concept.Stack); stack != "" {
			fmt.Fprintf(b, "- 所属技术栈：%s\n", oneLine(stack, ""))
		}
		if why := strings.TrimSpace(concept.WhyRelevant); why != "" {
			fmt.Fprintf(b, "- 为什么与本案相关：%s\n", oneLine(why, ""))
		}
		if url := strings.TrimSpace(concept.DocsURL); url != "" {
			fmt.Fprintf(b, "- 官方文档：%s\n", url)
		}
		lang := learnCodeLanguage(concept.Stack)
		if syntax := strings.TrimSpace(concept.Syntax); syntax != "" {
			b.WriteString("\n**最小语法**\n\n")
			b.WriteString(fenced(lang, syntax))
			b.WriteString("\n")
		}
		if example := strings.TrimSpace(concept.Example); example != "" {
			b.WriteString("**最小示例**\n\n")
			b.WriteString(fenced(lang, example))
			b.WriteString("\n")
		}
		if refs := learnEvidenceRefs(concept.Evidence); refs != "" {
			b.WriteString("- " + refs + "\n")
		}
		b.WriteString("\n")
	}
}

func writeLearnBlindSpots(b *strings.Builder, doc *extract.Doc) {
	if len(doc.BlindSpots) == 0 {
		return
	}
	b.WriteString("\n## 盲区\n\n")
	groups := map[string][]extract.BlindSpot{}
	for _, spot := range doc.BlindSpots {
		groups[spot.Kind] = append(groups[spot.Kind], spot)
	}
	for _, kind := range []string{"general_method", "framework_knowledge", "project_context", ""} {
		spots := groups[kind]
		if len(spots) == 0 {
			continue
		}
		fmt.Fprintf(b, "### %s\n\n", blindSpotKindLabel(kind))
		for _, spot := range spots {
			fmt.Fprintf(b, "- **%s**\n", oneLine(spot.Point, "未命名盲区"))
			if why := strings.TrimSpace(spot.WhyItMatters); why != "" {
				fmt.Fprintf(b, "  - 为什么重要：%s\n", oneLine(why, ""))
			}
			if refs := learnEvidenceRefs(spot.Evidence); refs != "" {
				fmt.Fprintf(b, "  - %s\n", refs)
			}
		}
		b.WriteString("\n")
	}
}

func writeLearnChecklist(b *strings.Builder, doc *extract.Doc) {
	var items []string
	for _, pitfall := range doc.Pitfalls {
		item := strings.TrimSpace(pitfall.Avoid)
		if item == "" {
			item = strings.TrimSpace(pitfall.Trap)
		}
		items = appendUniqueLine(items, item)
	}
	for _, lesson := range doc.Lessons {
		items = appendUniqueLine(items, lesson)
	}
	for _, spot := range doc.BlindSpots {
		if point := strings.TrimSpace(spot.Point); point != "" {
			items = appendUniqueLine(items, "补上："+point)
		}
	}
	if len(items) == 0 {
		return
	}
	b.WriteString("\n## 下次检查清单\n\n")
	for _, item := range items {
		fmt.Fprintf(b, "- [ ] %s\n", oneLine(item, ""))
	}
	b.WriteString("\n")
}

func writeLearnSelfTest(b *strings.Builder, doc *extract.Doc) {
	type question struct {
		prompt string
		answer string
	}
	var questions []question

	if cause := strings.TrimSpace(doc.RootCause.Text); cause != "" {
		questions = append(questions, question{
			prompt: "本案的根因是什么？",
			answer: cause,
		})
	}
	for i, concept := range doc.Concepts {
		if i >= 8 {
			break
		}
		var parts []string
		if why := strings.TrimSpace(concept.WhyRelevant); why != "" {
			parts = append(parts, why)
		}
		if example := strings.TrimSpace(concept.Example); example != "" {
			parts = append(parts, fenced(learnCodeLanguage(concept.Stack), example))
		} else if syntax := strings.TrimSpace(concept.Syntax); syntax != "" {
			parts = append(parts, fenced(learnCodeLanguage(concept.Stack), syntax))
		}
		answer := strings.Join(parts, "\n\n")
		if answer == "" {
			continue
		}
		questions = append(questions, question{
			prompt: fmt.Sprintf("「%s」是什么？它为什么与本案相关？", oneLine(concept.Name, "该概念")),
			answer: answer,
		})
	}
	for i, rejected := range doc.RejectedHypotheses {
		if i >= 5 {
			break
		}
		answer := strings.TrimSpace(rejected.WhyRejected)
		if answer == "" {
			continue
		}
		questions = append(questions, question{
			prompt: fmt.Sprintf("为什么排除了「%s」？", oneLine(rejected.Hypothesis, "该假设")),
			answer: answer,
		})
	}
	for i, pitfall := range doc.Pitfalls {
		if i >= 5 {
			break
		}
		answer := strings.TrimSpace(pitfall.Avoid)
		if answer == "" {
			answer = strings.TrimSpace(pitfall.Why)
		}
		if answer == "" {
			continue
		}
		questions = append(questions, question{
			prompt: fmt.Sprintf("下次如何避免「%s」？", oneLine(pitfall.Trap, "这个坑")),
			answer: answer,
		})
	}
	if len(questions) == 0 {
		return
	}

	b.WriteString("\n## 案例内自测题\n\n")
	b.WriteString("先自己写答案，再展开核对。这里只做自测，不记录掌握度，也不安排复习。\n\n")
	for i, q := range questions {
		fmt.Fprintf(b, "### %d. %s\n\n", i+1, q.prompt)
		b.WriteString("<details><summary>答案</summary>\n\n")
		b.WriteString(strings.TrimRight(q.answer, "\n"))
		b.WriteString("\n\n</details>\n\n")
	}
}

func writeLearnEvidence(b *strings.Builder, doc *extract.Doc) {
	if len(doc.Evidence) == 0 {
		return
	}
	b.WriteString("\n## 证据来源\n\n")
	b.WriteString("| 序号 | 类型 | 已核验 | 引用 |\n")
	b.WriteString("|---:|---|---|---|\n")
	for _, ev := range doc.Evidence {
		verified := "否"
		if ev.Verified {
			verified = "是"
		}
		fmt.Fprintf(b, "| %d | %s | %s | %s |\n", ev.EventSeq, tableCell(ev.Kind), verified, tableCell(ev.Quote))
	}
	b.WriteString("\n")
}

func writeLearnEvidenceRefs(b *strings.Builder, seqs []int) {
	if refs := learnEvidenceRefs(seqs); refs != "" {
		b.WriteString(refs)
		b.WriteString("\n\n")
	}
}

func learnEvidenceRefs(seqs []int) string {
	if len(seqs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(seqs))
	for _, seq := range seqs {
		parts = append(parts, strconv.Itoa(seq))
	}
	return "_证据：" + strings.Join(parts, ", ") + "。_"
}

func learningValueLabel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "high":
		return "高"
	case "medium":
		return "中"
	case "low":
		return "低"
	default:
		return value
	}
}

func confidenceLabel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "high":
		return "高"
	case "medium":
		return "中"
	case "low":
		return "低"
	default:
		return value
	}
}

func verificationStatusLabel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "passed":
		return "通过"
	case "failed":
		return "失败"
	case "not_run":
		return "未运行"
	case "unknown":
		return "未知"
	default:
		return value
	}
}

func blindSpotKindLabel(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "general_method":
		return "通用方法"
	case "framework_knowledge":
		return "框架知识"
	case "project_context":
		return "项目上下文"
	default:
		return "其他"
	}
}

func learnCodeLanguage(stack string) string {
	value := strings.ToLower(strings.TrimSpace(stack))
	switch {
	case strings.Contains(value, "typescript"), strings.Contains(value, "tsx"), strings.Contains(value, "react"):
		return "tsx"
	case strings.Contains(value, "javascript"), strings.Contains(value, "node"):
		return "js"
	case strings.Contains(value, "python"):
		return "python"
	case strings.Contains(value, "java") && !strings.Contains(value, "javascript"):
		return "java"
	case strings.Contains(value, "rust"):
		return "rust"
	case strings.Contains(value, "shell"), strings.Contains(value, "bash"), strings.Contains(value, "kafka"):
		return "bash"
	case containsWord(value, "go"), strings.Contains(value, "golang"), strings.Contains(value, "gin"),
		strings.Contains(value, "goframe"), strings.Contains(value, "gorm"):
		return "go"
	default:
		return "text"
	}
}

func containsWord(value, word string) bool {
	for _, field := range strings.FieldsFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	}) {
		if field == word {
			return true
		}
	}
	return false
}

func appendUniqueLine(values []string, addition string) []string {
	addition = strings.TrimSpace(addition)
	if addition == "" {
		return values
	}
	key := strings.ToLower(addition)
	for _, existing := range values {
		if strings.ToLower(strings.TrimSpace(existing)) == key {
			return values
		}
	}
	return append(values, addition)
}

// LearnDocPath returns the learning-profile Markdown path for one document.
// It never collides with the agent profile because it adds a suffix.
func LearnDocPath(doc *extract.Doc) string {
	path := DocPath(doc)
	if path == "" {
		return ""
	}
	return strings.TrimSuffix(path, ".md") + ".learn.md"
}

// LearnPlans mirrors PlanDocs for the learning profile so renamed paths stay
// collision-free.
func LearnPlans(docs []*extract.Doc) []Plan {
	plans := PlanDocs(docs)
	for i := range plans {
		plans[i].Path = strings.TrimSuffix(plans[i].Path, ".md") + ".learn.md"
	}
	sort.SliceStable(plans, func(i, j int) bool { return plans[i].Path < plans[j].Path })
	return plans
}
