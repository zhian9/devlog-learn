package render

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"devlog-learn/internal/experience"
)

// ExperienceOverviewPath is the global experience list written at the output
// root. It intentionally differs from index's README.md.
const ExperienceOverviewPath = "EXPERIENCE.md"

// ExperiencePlan assigns one output path to one project experience.
type ExperiencePlan struct {
	Experience *experience.Experience
	Path       string
}

// PlanExperiences sorts experiences by project and assigns collision-free
// "<project>/EXPERIENCE.md" paths. The seen key is case-insensitive because the
// output may live on a case-insensitive filesystem.
func PlanExperiences(exps []experience.Experience) []ExperiencePlan {
	ordered := make([]experience.Experience, len(exps))
	copy(ordered, exps)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Project < ordered[j].Project
	})

	seen := map[string]bool{}
	plans := make([]ExperiencePlan, 0, len(ordered))
	for i := range ordered {
		dir := SafeProject(ordered[i].Project)
		rel := dir + "/EXPERIENCE.md"
		if seen[strings.ToLower(rel)] {
			base := dir + "/EXPERIENCE-" + shortHash(ordered[i].Project)
			rel = base + ".md"
			for n := 2; seen[strings.ToLower(rel)]; n++ {
				rel = base + "-" + strconv.Itoa(n) + ".md"
			}
		}
		seen[strings.ToLower(rel)] = true
		plans = append(plans, ExperiencePlan{Experience: &ordered[i], Path: rel})
	}
	return plans
}

// ExperiencePaths returns project -> output path for list responses.
func ExperiencePaths(exps []experience.Experience) map[string]string {
	out := make(map[string]string, len(exps))
	for _, plan := range PlanExperiences(exps) {
		out[plan.Experience.Project] = plan.Path
	}
	return out
}

// ExperienceMarkdown renders one project experience. It is deterministic: the
// output contains no current timestamp, so repeated renders of the same input
// are byte-identical.
func ExperienceMarkdown(exp *experience.Experience, docPaths map[string]string) string {
	if exp == nil {
		return ""
	}
	var b strings.Builder

	b.WriteString("---\n")
	writeYAMLString(&b, "project", exp.Project)
	writeYAMLString(&b, "kind", exp.Kind)
	writeYAMLString(&b, "kind_label", exp.KindLabel)
	writeYAMLString(&b, "kind_source", exp.KindSource)
	writeYAMLString(&b, "organization", exp.Organization)
	writeYAMLString(&b, "role", exp.Role)
	writeYAMLString(&b, "period", exp.Period)
	fmt.Fprintf(&b, "documents: %d\n", exp.Docs)
	fmt.Fprintf(&b, "sessions: %d\n", exp.Sessions)
	writeYAMLList(&b, "sources", exp.Sources)
	writeYAMLList(&b, "stack", exp.Stack)
	b.WriteString("---\n\n")

	name := strings.TrimSpace(exp.Project)
	if exp.Kind == experience.KindInternship || exp.Kind == experience.KindWork {
		if org := strings.TrimSpace(exp.Organization); org != "" {
			name = org
		}
	}
	label := strings.TrimSpace(exp.KindLabel)
	if label == "" {
		label = experience.KindLabel(exp.Kind)
	}
	fmt.Fprintf(&b, "# %s %s\n\n", oneLine(name, "未命名项目"), label)

	if headline := strings.TrimSpace(exp.Headline); headline != "" {
		b.WriteString(blockquote(headline))
		b.WriteString("\n")
	}

	writeExperienceOverview(&b, exp)
	writeExperienceStack(&b, exp)
	writeExperienceHighlights(&b, "关键工作", exp.Highlights, docPaths)
	writeExperienceHighlights(&b, "难点与解决", exp.Challenges, docPaths)
	writeExperienceBullets(&b, "结果与数据", exp.Outcomes)
	writeExperienceHighlights(&b, "可讲的故事（STAR）", exp.Stories, docPaths)
	writeExperienceLessons(&b, exp)
	writeExperienceChecklist(&b, exp.ReviewChecklist, docPaths)
	writeExperienceDocIndex(&b, exp.DocRefs, docPaths)
	writeExperienceSynthesis(&b, exp.Synthesized)
	writeExperienceWarnings(&b, exp.Warnings)
	return b.String()
}

func writeExperienceOverview(b *strings.Builder, exp *experience.Experience) {
	b.WriteString("\n## 概览\n\n")
	rows := [][2]string{
		{"类型", exp.KindLabel + "（" + kindSourceLabel(exp.KindSource) + "）"},
		{"组织", exp.Organization},
		{"角色", exp.Role},
		{"时间", exp.Period},
		{"规模", fmt.Sprintf("%d 篇文档，%d 个会话", exp.Docs, exp.Sessions)},
		{"来源", strings.Join(exp.Sources, "、")},
		{"目录", strings.Join(exp.Paths, "；")},
	}
	for _, row := range rows {
		value := strings.TrimSpace(row[1])
		if value == "" {
			continue
		}
		if row[0] == "类型" {
			fmt.Fprintf(b, "- **类型**：%s\n", value)
			continue
		}
		if row[0] == "目录" {
			fmt.Fprintf(b, "- **目录**：%s\n", codeList(exp.Paths))
			continue
		}
		fmt.Fprintf(b, "- **%s**：%s\n", row[0], value)
	}
	b.WriteString("\n")
}

func writeExperienceStack(b *strings.Builder, exp *experience.Experience) {
	if len(exp.Stack) == 0 {
		return
	}
	b.WriteString("\n## 技术栈\n\n")
	for i, item := range exp.Stack {
		if i > 0 {
			b.WriteString(" · ")
		}
		b.WriteString(item)
	}
	b.WriteString("\n")
}

func writeExperienceHighlights(b *strings.Builder, title string, items []experience.Highlight, docPaths map[string]string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s\n\n", title)
	for _, item := range items {
		text := strings.TrimSpace(item.Text)
		if text == "" {
			continue
		}
		fmt.Fprintf(b, "- %s", oneLine(text, ""))
		if refs := experienceRefs(item.DocIDs, docPaths); refs != "" {
			fmt.Fprintf(b, "（%s）", refs)
		}
		b.WriteString("\n")
	}
}

func writeExperienceBullets(b *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s\n\n", title)
	for _, item := range items {
		if item = strings.TrimSpace(item); item != "" {
			fmt.Fprintf(b, "- %s\n", oneLine(item, ""))
		}
	}
}

func writeExperienceLessons(b *strings.Builder, exp *experience.Experience) {
	if len(exp.Lessons) == 0 && len(exp.BlindSpots) == 0 {
		return
	}
	b.WriteString("\n## 复盘与收获\n\n")
	if len(exp.Lessons) > 0 {
		b.WriteString("### 经验\n\n")
		for _, lesson := range exp.Lessons {
			fmt.Fprintf(b, "- %s\n", oneLine(lesson, ""))
		}
		b.WriteString("\n")
	}
	if len(exp.BlindSpots) > 0 {
		b.WriteString("### 盲区\n\n")
		for _, spot := range exp.BlindSpots {
			fmt.Fprintf(b, "- %s", oneLine(spot.Text, "未命名盲区"))
			if spot.DocID != "" {
				fmt.Fprintf(b, "（%s）", experienceRefs([]string{spot.DocID}, nil))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
}

func writeExperienceChecklist(b *strings.Builder, items []experience.ReviewItem, docPaths map[string]string) {
	if len(items) == 0 {
		return
	}
	b.WriteString("\n## 复习清单\n\n")
	b.WriteString("> 静态清单，不记录完成状态。看到题目先自己回答，再回到对应文档核对。\n\n")
	for _, item := range items {
		fmt.Fprintf(b, "- [ ] %s", oneLine(item.Text, ""))
		if item.DocID != "" {
			if refs := experienceRefs([]string{item.DocID}, docPaths); refs != "" {
				fmt.Fprintf(b, "（%s）", refs)
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func writeExperienceDocIndex(b *strings.Builder, docs []experience.DocRef, docPaths map[string]string) {
	if len(docs) == 0 {
		return
	}
	b.WriteString("\n## 文档索引\n\n")
	b.WriteString("| 日期 | 标题 | 类型 | 状态 | 链接 |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, doc := range docs {
		date := ""
		if !doc.OccurredAt.IsZero() {
			date = doc.OccurredAt.UTC().Format("2006-01-02")
		}
		link := experienceRefs([]string{doc.ID}, docPaths)
		if link == "" {
			link = "`" + doc.ID + "`"
		}
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s |\n",
			tableCell(date), tableCell(doc.Title), tableCell(doc.KnowledgeType), tableCell(doc.Status), link)
	}
	b.WriteString("\n")
}

func writeExperienceSynthesis(b *strings.Builder, synthesis *experience.Synthesis) {
	if synthesis == nil {
		return
	}
	b.WriteString("\n## 模型补充（未验证）\n\n")
	b.WriteString("> 以下内容由本地模型根据文档摘要生成，未逐条核验；上面的确定性内容优先。\n\n")
	if synthesis.Summary != "" {
		b.WriteString("### 概括\n\n")
		b.WriteString(oneLine(synthesis.Summary, ""))
		b.WriteString("\n\n")
	}
	if synthesis.Background != "" {
		b.WriteString("### 背景\n\n")
		b.WriteString(oneLine(synthesis.Background, ""))
		b.WriteString("\n\n")
	}
	writeSynthesisHighlights(b, "关键贡献", synthesis.Contributions)
	writeSynthesisHighlights(b, "难点", synthesis.Challenges)
	writeSynthesisHighlights(b, "结果", synthesis.Outcomes)
	writeSynthesisHighlights(b, "故事", synthesis.Stories)
}

func writeSynthesisHighlights(b *strings.Builder, title string, items []experience.Highlight) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "### %s\n\n", title)
	for _, item := range items {
		fmt.Fprintf(b, "- %s", oneLine(item.Text, ""))
		if len(item.DocIDs) > 0 {
			fmt.Fprintf(b, "（%s）", codeList(item.DocIDs))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func writeExperienceWarnings(b *strings.Builder, warnings []string) {
	if len(warnings) == 0 {
		return
	}
	b.WriteString("\n## 备注\n\n")
	for _, warning := range warnings {
		if warning = strings.TrimSpace(warning); warning != "" {
			fmt.Fprintf(b, "- %s\n", oneLine(warning, ""))
		}
	}
	b.WriteString("\n")
}

// ExperienceOverviewMarkdown renders the global list at the output root.
func ExperienceOverviewMarkdown(plans []ExperiencePlan) string {
	var b strings.Builder
	b.WriteString("# 经历总览\n\n")
	b.WriteString("同一项目目录下已提炼的会话会聚合为一份项目经历或实习经验。")
	b.WriteString("复习清单是静态的；模型补充只在显式运行 synthesize 后出现。\n\n")
	if len(plans) == 0 {
		b.WriteString("_暂无经历。先运行 extract / run 提炼会话。_\n")
		return b.String()
	}
	b.WriteString("| 项目 | 类型 | 时间 | 文档 | 链接 |\n")
	b.WriteString("|---|---|---|---:|---|\n")
	for _, plan := range plans {
		exp := plan.Experience
		label := strings.TrimSpace(exp.KindLabel)
		if label == "" {
			label = experience.KindLabel(exp.Kind)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %d | [%s](%s) |\n",
			tableCell(exp.Project), tableCell(label), tableCell(exp.Period), exp.Docs, linkLabel(label), plan.Path)
	}
	return b.String()
}

func experienceRefs(ids []string, docPaths map[string]string) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		rel := ""
		if docPaths != nil {
			rel = docPaths[id]
		}
		if rel == "" {
			parts = append(parts, "`"+id+"`")
			continue
		}
		file := path.Base(rel)
		parts = append(parts, fmt.Sprintf("[%s](./%s)", linkLabel(file), file))
	}
	return strings.Join(parts, "、")
}

func codeList(values []string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			parts = append(parts, "`"+value+"`")
		}
	}
	return strings.Join(parts, "、")
}

func kindSourceLabel(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case experience.KindSourceManual:
		return "人工确认"
	case experience.KindSourceSuggestion:
		return "自动建议，待确认"
	default:
		return "默认"
	}
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:8]
}

func linkLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Untitled"
	}
	value = strings.ReplaceAll(value, "[", `\[`)
	value = strings.ReplaceAll(value, "]", `\]`)
	return value
}
