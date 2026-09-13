package initcmd

import (
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/scaffold"
)

func TestBuildInitCommitFiles_StagesScaffoldSkillsAndFestivals(t *testing.T) {
	result := &scaffold.InitResult{
		CampaignRoot:  "/camp",
		DirsCreated:   []string{"projects", "workflow/design"},
		FilesCreated:  []string{"/camp/.campaign/campaign.yaml", "AGENTS.md"},
		FilesModified: []string{".gitignore"},
	}
	got := buildInitCommitFiles(result, []string{".claude/skills/camp-projects"}, true)
	want := []string{".campaign/campaign.yaml", "AGENTS.md", ".gitignore", "projects", "workflow/design", ".claude/skills/camp-projects", festivalsDir}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("buildInitCommitFiles() = %v, want %v", got, want)
	}
}

func TestBuildInitCommitFiles_OmitsFestivalsWhenFestDidNotRun(t *testing.T) {
	result := &scaffold.InitResult{CampaignRoot: "/camp", FilesCreated: []string{"AGENTS.md"}}
	for _, p := range buildInitCommitFiles(result, nil, false) {
		if p == festivalsDir {
			t.Fatalf("festivals staged although fest init did not run")
		}
	}
}

func TestBuildInitCommitFiles_EmptyScaffoldStagesNothing(t *testing.T) {
	if got := buildInitCommitFiles(&scaffold.InitResult{CampaignRoot: "/camp"}, nil, false); len(got) != 0 {
		t.Fatalf("expected no files, got %v", got)
	}
}

func TestBuildInitCommitMessage_DescribesEverythingStaged(t *testing.T) {
	result := &scaffold.InitResult{
		CampaignRoot:  "/camp",
		DirsCreated:   []string{"/camp/projects", "/camp/docs"},
		FilesCreated:  []string{"/camp/AGENTS.md", ".campaign/.gitignore"},
		FilesModified: []string{"/camp/.gitignore"},
	}
	msg := buildInitCommitMessage(result, []string{".claude/skills/camp-projects"}, true)
	for _, want := range []string{
		"Directories created: 2",
		"Files created:\n  - AGENTS.md\n  - .campaign/.gitignore",
		"Files updated:\n  - .gitignore",
		"Skill links projected:\n  - .claude/skills/camp-projects",
		"Festival Methodology initialized in festivals/",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "/camp/") {
		t.Errorf("message leaks absolute paths:\n%s", msg)
	}
	if strings.HasSuffix(msg, "\n") {
		t.Errorf("message has trailing newline:\n%q", msg)
	}
}

func TestBuildInitCommitMessage_OmitsSectionsWithNothingToSay(t *testing.T) {
	msg := buildInitCommitMessage(&scaffold.InitResult{FilesCreated: []string{"AGENTS.md"}}, nil, false)
	for _, absent := range []string{"Directories created", "Files updated", "Skill links", "Festival Methodology"} {
		if strings.Contains(msg, absent) {
			t.Errorf("message mentions %q with nothing behind it:\n%s", absent, msg)
		}
	}
}
