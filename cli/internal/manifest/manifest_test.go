package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var manifestTime = time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)

func TestReconcilePrunesOnlyUnchangedManagedFiles(t *testing.T) {
	outDir := t.TempDir()
	oldPath := filepath.Join(outDir, "demo", "old.md")
	manualPath := filepath.Join(outDir, "manual.md")
	mustWrite(t, oldPath, "old generated")
	mustWrite(t, manualPath, "manual")

	if _, err := Reconcile(outDir, "agent", []GeneratedFile{{
		Path: "demo/old.md", DocID: "old",
	}}, false, manifestTime); err != nil {
		t.Fatalf("initial Reconcile: %v", err)
	}

	newPath := filepath.Join(outDir, "demo", "new.md")
	mustWrite(t, newPath, "new generated")
	report, err := Reconcile(outDir, "agent", []GeneratedFile{{
		Path: "demo/new.md", DocID: "new",
	}}, true, manifestTime.Add(time.Hour))
	if err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if report.Pruned != 1 {
		t.Fatalf("pruned = %d report = %+v", report.Pruned, report)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old generated file still present: %v", err)
	}
	if _, err := os.Stat(manualPath); err != nil {
		t.Fatalf("manual file was removed: %v", err)
	}

	value, err := Load(outDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := value.Files["demo/new.md"]; !ok {
		t.Fatalf("new file missing from manifest: %+v", value.Files)
	}
}

func TestReconcileKeepsModifiedGeneratedFile(t *testing.T) {
	outDir := t.TempDir()
	path := filepath.Join(outDir, "demo", "doc.md")
	mustWrite(t, path, "generated")
	if _, err := Reconcile(outDir, "agent", []GeneratedFile{{
		Path: "demo/doc.md", DocID: "doc",
	}}, false, manifestTime); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, path, "edited by user")

	report, err := Reconcile(outDir, "agent", nil, true, manifestTime.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if report.Pruned != 0 || report.Skipped != 1 {
		t.Fatalf("report = %+v", report)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "edited by user" {
		t.Fatalf("modified file changed: %q", data)
	}
	if !strings.Contains(strings.Join(report.Warnings, " "), "modified") {
		t.Fatalf("warnings = %v", report.Warnings)
	}
}

func TestReconcileDoesNotDeleteOutsideOutput(t *testing.T) {
	root := t.TempDir()
	outDir := filepath.Join(root, "out")
	outside := filepath.Join(root, "outside.md")
	mustWrite(t, outside, "do not delete")
	manifestValue := &Manifest{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   manifestTime,
		Files: map[string]FileRecord{
			"../outside.md": {
				Profile:     "agent",
				SHA256:      hashString("do not delete"),
				GeneratedAt: manifestTime,
			},
		},
	}
	if err := save(outDir, manifestValue); err != nil {
		t.Fatal(err)
	}

	report, err := Reconcile(outDir, "agent", nil, true, manifestTime)
	if err != nil {
		t.Fatal(err)
	}
	if report.Pruned != 0 || len(report.Warnings) == 0 {
		t.Fatalf("report = %+v", report)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside file removed: %v", err)
	}
}

func TestReconcileProfilesAreIndependent(t *testing.T) {
	outDir := t.TempDir()
	agentPath := filepath.Join(outDir, "demo", "doc.md")
	learningPath := filepath.Join(outDir, "demo", "doc.learn.md")
	mustWrite(t, agentPath, "agent")
	mustWrite(t, learningPath, "learning")
	if _, err := Reconcile(outDir, "agent", []GeneratedFile{{
		Path: "demo/doc.md",
	}}, false, manifestTime); err != nil {
		t.Fatal(err)
	}
	if _, err := Reconcile(outDir, "learning", []GeneratedFile{{
		Path: "demo/doc.learn.md",
	}}, false, manifestTime); err != nil {
		t.Fatal(err)
	}
	if _, err := Reconcile(outDir, "agent", nil, true, manifestTime); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(learningPath); err != nil {
		t.Fatalf("other profile removed: %v", err)
	}
}

func TestReconcileRejectsSymlinkTraversal(t *testing.T) {
	root := t.TempDir()
	outDir := filepath.Join(root, "out")
	outsideDir := filepath.Join(root, "outside")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outsideDir, "secret.md")
	mustWrite(t, outsideFile, "do not delete")
	link := filepath.Join(outDir, "linked")
	if err := os.Symlink(outsideDir, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	manifestValue := &Manifest{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   manifestTime,
		Files: map[string]FileRecord{
			"linked/secret.md": {
				Profile:     "agent",
				SHA256:      hashString("do not delete"),
				GeneratedAt: manifestTime,
			},
		},
	}
	if err := save(outDir, manifestValue); err != nil {
		t.Fatal(err)
	}

	report, err := Reconcile(outDir, "agent", nil, true, manifestTime)
	if err != nil {
		t.Fatal(err)
	}
	if report.Pruned != 0 || len(report.Warnings) == 0 {
		t.Fatalf("report = %+v", report)
	}
	if _, err := os.Stat(outsideFile); err != nil {
		t.Fatalf("outside file removed through symlink: %v", err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
