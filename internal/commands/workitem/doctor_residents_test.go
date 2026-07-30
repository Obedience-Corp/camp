package workitem

import (
	"os"
	"path/filepath"
	"testing"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

func codesOf(findings []docFinding) map[string][]string {
	out := map[string][]string{}
	for _, f := range findings {
		out[f.Code] = append(out[f.Code], f.Target)
	}
	return out
}

func TestUnclassifiableLifecycleDirs(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string, files map[string]string) {
		dir := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, body := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	mk("festivals/active/mystery", map[string]string{"NOTES.md": "# ?\n"})
	mk("festivals/active/realfest", map[string]string{"fest.yaml": "version: \"1.0\"\n"})
	mk("festivals/ready/goalonly", map[string]string{"FESTIVAL_GOAL.md": "# Goal\n"})
	mk("festivals/ready/overview", map[string]string{"FESTIVAL_OVERVIEW.md": "# Overview\n"})
	mk("festivals/ready/stamped", map[string]string{".workitem": "type: design\n"})
	mk("festivals/ready/.hidden", map[string]string{"NOTES.md": "# hidden\n"})
	// planning is not a rail stage, so it is out of scope for this check.
	mk("festivals/planning/whatever", map[string]string{"NOTES.md": "# ?\n"})

	got := codesOf(unclassifiableLifecycleDirs(root))
	targets := got[codeUnstampedResident]
	if len(targets) != 1 || targets[0] != "festivals/active/mystery" {
		t.Errorf("flagged %v, want only [festivals/active/mystery]", targets)
	}
}

func TestResidentsWithoutHome(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "workflow", "design"), 0o755); err != nil {
		t.Fatal(err)
	}

	items := []wkitem.WorkItem{
		{WorkflowType: wkitem.WorkflowTypeDesign, RelativePath: "festivals/active/has-home"},
		{WorkflowType: "bug", RelativePath: "festivals/active/no-home"},
		{WorkflowType: wkitem.WorkflowTypeDesign, RelativePath: "workflow/design/not-a-resident"},
		{WorkflowType: wkitem.WorkflowTypeFestival, RelativePath: "festivals/active/a-festival"},
	}

	got := codesOf(residentsWithoutHome(root, items))
	targets := got[codeResidentMissingHome]
	if len(targets) != 1 || targets[0] != "festivals/active/no-home" {
		t.Errorf("flagged %v, want only [festivals/active/no-home]", targets)
	}
}

// Neither resident finding is auto-fixable: camp cannot guess a type or invent a
// type root, so --fix must not touch them.
func TestResidentFindingsAreNotAutoFixable(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "festivals", "active", "mystery")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	items := []wkitem.WorkItem{{WorkflowType: "bug", RelativePath: "festivals/active/no-home"}}

	for _, f := range collectResidentFindings(root, items) {
		if f.AutoFixable {
			t.Errorf("finding %s is auto-fixable; camp must not guess here", f.Code)
		}
		if f.Severity != docSeverityWarning {
			t.Errorf("finding %s severity = %q, want warning", f.Code, f.Severity)
		}
		if f.FixHint == "" {
			t.Errorf("finding %s has no fix hint", f.Code)
		}
	}
}

func TestIsFestivalDir(t *testing.T) {
	for _, marker := range []string{"fest.yaml", "FESTIVAL_GOAL.md", "FESTIVAL_OVERVIEW.md"} {
		t.Run(marker, func(t *testing.T) {
			dir := t.TempDir()
			if isFestivalDir(dir) {
				t.Fatal("empty dir should not read as a festival")
			}
			if err := os.WriteFile(filepath.Join(dir, marker), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			if !isFestivalDir(dir) {
				t.Errorf("%s should mark a festival directory", marker)
			}
		})
	}
}
