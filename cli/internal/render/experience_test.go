package render

import (
	"strings"
	"testing"

	"devlog-learn/internal/experience"
)

func experienceFixture(kind string) experience.Experience {
	return experience.Experience{
		Project:      "demo",
		ProjectPath:  `~/work/demo`,
		Paths:        []string{`~/work/demo`},
		Kind:         kind,
		KindLabel:    experience.KindLabel(kind),
		KindSource:   experience.KindSourceManual,
		Organization: "ACME",
		Role:         "Backend Intern",
		Headline:     "Built the demo service",
		Period:       "2025-07 ~ 2025-09",
		Docs:         2,
		Sessions:     2,
		Sources:      []string{"claude-code"},
		Stack:        []string{"Go", "PostgreSQL"},
		Highlights:   []experience.Highlight{{Text: "Fixed cache", DocIDs: []string{"d1"}}},
		Challenges:   []experience.Highlight{{Text: "Cache invalidation", DocIDs: []string{"d1"}}},
		Outcomes:     []string{"文档 2 篇：已解决 2，待跟进 0，状态未知 0"},
		Lessons:      []string{"measure before optimizing"},
		BlindSpots:   []experience.ReviewItem{{ID: "ex_1", Text: "connection pooling", DocID: "d1", Kind: "blind_spot"}},
		Stories:      []experience.Highlight{{Text: "背景/任务：cache broke；行动：fix；结果：ok", DocIDs: []string{"d1"}}},
		ReviewChecklist: []experience.ReviewItem{
			{ID: "ex_2", Text: "讲清根因", DocID: "d1", Kind: "root_cause"},
		},
		DocRefs: []experience.DocRef{
			{ID: "d1", Title: "Fix cache", KnowledgeType: "bug_fix", Status: "resolved"},
		},
	}
}

func TestExperienceMarkdownProjectAndInternship(t *testing.T) {
	paths := map[string]string{"d1": "demo/2025-08-01-fix-cache.md"}

	projectExp := experienceFixture(experience.KindProject)
	project := ExperienceMarkdown(&projectExp, paths)
	if !strings.Contains(project, "# demo 项目经历") {
		t.Fatalf("project title missing:\n%s", project)
	}
	if !strings.Contains(project, "[2025-08-01-fix-cache.md](./2025-08-01-fix-cache.md)") {
		t.Fatalf("doc link missing:\n%s", project)
	}
	if !strings.Contains(project, "- [ ] 讲清根因") {
		t.Fatalf("checklist missing:\n%s", project)
	}
	if strings.Contains(project, "## 模型补充（未验证）") {
		t.Fatal("synthesis section rendered without synthesis")
	}

	internshipExp := experienceFixture(experience.KindInternship)
	internship := ExperienceMarkdown(&internshipExp, paths)
	if !strings.Contains(internship, "# ACME 实习经验") {
		t.Fatalf("internship title missing:\n%s", internship)
	}
	if !strings.Contains(internship, "- **组织**：ACME") || !strings.Contains(internship, "- **角色**：Backend Intern") {
		t.Fatalf("internship metadata missing:\n%s", internship)
	}
}

func TestExperienceMarkdownRendersSynthesisLabel(t *testing.T) {
	exp := experienceFixture(experience.KindInternship)
	exp.Synthesized = &experience.Synthesis{
		Summary:       "model summary",
		Contributions: []experience.Highlight{{Text: "model contribution", DocIDs: []string{"d1"}}},
		Unverified:    true,
	}
	got := ExperienceMarkdown(&exp, nil)
	if !strings.Contains(got, "## 模型补充（未验证）") || !strings.Contains(got, "model summary") {
		t.Fatalf("synthesis section missing:\n%s", got)
	}
	if !strings.Contains(got, "未逐条核验") {
		t.Fatalf("unverified warning missing:\n%s", got)
	}
}

func TestExperienceMarkdownIsDeterministic(t *testing.T) {
	exp := experienceFixture(experience.KindProject)
	first := ExperienceMarkdown(&exp, nil)
	second := ExperienceMarkdown(&exp, nil)
	if first != second {
		t.Fatal("experience markdown is not deterministic")
	}
	if strings.Contains(first, "generated_at") {
		t.Fatal("baseline markdown contains a timestamp")
	}
}

func TestPlanExperiencesAvoidsCaseCollisions(t *testing.T) {
	exps := []experience.Experience{
		{Project: "Foo"},
		{Project: "foo"},
		{Project: "demo"},
	}
	plans := PlanExperiences(exps)
	seen := map[string]bool{}
	for _, plan := range plans {
		key := strings.ToLower(plan.Path)
		if seen[key] {
			t.Fatalf("duplicate path %q in %+v", plan.Path, plans)
		}
		seen[key] = true
	}
	if len(plans) != 3 {
		t.Fatalf("plans = %+v", plans)
	}
}

func TestExperienceOverviewEmpty(t *testing.T) {
	got := ExperienceOverviewMarkdown(nil)
	if !strings.Contains(got, "暂无经历") {
		t.Fatalf("empty overview = %s", got)
	}
}
