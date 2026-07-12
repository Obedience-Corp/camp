package project

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spf13/cobra"
)

func TestParseProjectRunArgs(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantProject string
		wantCommand []string
	}{
		{"no flags", []string{"ls", "-la"}, "", []string{"ls", "-la"}},
		{"empty args", nil, "", nil},
		{"-p value", []string{"-p", "fest", "just", "build"}, "fest", []string{"just", "build"}},
		{"--project value", []string{"--project", "camp", "go", "test"}, "camp", []string{"go", "test"}},
		{"--project= inline", []string{"--project=fest", "just", "build"}, "fest", []string{"just", "build"}},
		{"-p= inline", []string{"-p=fest", "just", "build"}, "fest", []string{"just", "build"}},
		{"-- separator with no flags", []string{"--", "ls", "-la"}, "", []string{"ls", "-la"}},
		{"-p then --", []string{"-p", "fest", "--", "just", "build"}, "fest", []string{"just", "build"}},
		{"flag after command word is not a camp flag", []string{"ls", "-p", "fest"}, "", []string{"ls", "-p", "fest"}},
		{"-p with no following arg falls through as a command", []string{"-p"}, "", []string{"-p"}},
		{"only --", []string{"--"}, "", []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotProject, gotCommand := parseProjectRunArgs(tt.args)
			if gotProject != tt.wantProject {
				t.Errorf("project = %q, want %q", gotProject, tt.wantProject)
			}
			if !reflect.DeepEqual(gotCommand, tt.wantCommand) {
				t.Errorf("command = %v, want %v", gotCommand, tt.wantCommand)
			}
		})
	}
}

func TestProjectRunFlagZone(t *testing.T) {
	tests := []struct {
		name            string
		args            []string
		wantInZone      bool
		wantAwaitsValue bool
	}{
		{"no args yet", nil, true, false},
		{"trailing -p awaits value", []string{"-p"}, true, true},
		{"trailing --project awaits value", []string{"--project"}, true, true},
		{"-p already has a value", []string{"-p", "fest"}, true, false},
		{"--project= inline consumed", []string{"--project=fest"}, true, false},
		{"-p= inline consumed", []string{"-p=fest"}, true, false},
		{"-- ends the zone", []string{"--"}, false, false},
		{"-p then -- ends the zone", []string{"-p", "--"}, true, false},
		{"command word ends the zone", []string{"ls"}, false, false},
		{"command word then -p is not a flag", []string{"ls", "-p"}, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotInZone, gotAwaits := projectRunFlagZone(tt.args)
			if gotInZone != tt.wantInZone || gotAwaits != tt.wantAwaitsValue {
				t.Errorf("projectRunFlagZone(%v) = (%v, %v), want (%v, %v)",
					tt.args, gotInZone, gotAwaits, tt.wantInZone, tt.wantAwaitsValue)
			}
		})
	}
}

// setupRunTestCampaign creates a temporary campaign root with a projects
// directory and one project per name. projectsvc.List only checks for a
// .git path's existence (internal/project/list.go), so a marker directory
// is enough here; no real git repo (and no host-side git exec.Command) is
// needed for name-only completion coverage.
func setupRunTestCampaign(t *testing.T, projectNames ...string) string {
	t.Helper()

	campRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(campRoot, ".campaign"), 0o755); err != nil {
		t.Fatal(err)
	}

	projectsDir := filepath.Join(campRoot, "projects")
	for _, name := range projectNames {
		if err := os.MkdirAll(filepath.Join(projectsDir, name, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	return campRoot
}

func newProjectRunTestCmd(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "run", RunE: runProjectRun}
	cmd.SetContext(context.Background())
	return cmd
}

func TestCompleteProjectRunArgs(t *testing.T) {
	campRoot := setupRunTestCampaign(t, "camp", "fest", "festival")
	t.Chdir(campRoot)

	cmd := newProjectRunTestCmd(t)

	t.Run("bare -p offers every project", func(t *testing.T) {
		got, directive := completeProjectRunArgs(cmd, []string{"-p"}, "")
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Fatalf("directive = %v, want NoFileComp", directive)
		}
		want := []string{"camp", "fest", "festival"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("completions = %v, want %v", got, want)
		}
	})

	t.Run("bare --project offers every project", func(t *testing.T) {
		got, _ := completeProjectRunArgs(cmd, []string{"--project"}, "")
		want := []string{"camp", "fest", "festival"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("completions = %v, want %v", got, want)
		}
	})

	t.Run("-p with prefix filters", func(t *testing.T) {
		got, _ := completeProjectRunArgs(cmd, []string{"-p"}, "fe")
		want := []string{"fest", "festival"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("completions = %v, want %v", got, want)
		}
	})

	t.Run("--project= inline form re-prefixes results", func(t *testing.T) {
		got, directive := completeProjectRunArgs(cmd, nil, "--project=fe")
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Fatalf("directive = %v, want NoFileComp", directive)
		}
		want := []string{"--project=fest", "--project=festival"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("completions = %v, want %v", got, want)
		}
	})

	t.Run("-p= inline form re-prefixes results", func(t *testing.T) {
		got, _ := completeProjectRunArgs(cmd, nil, "-p=fe")
		want := []string{"-p=fest", "-p=festival"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("completions = %v, want %v", got, want)
		}
	})

	t.Run("past the -- separator falls back to default completion", func(t *testing.T) {
		got, directive := completeProjectRunArgs(cmd, []string{"-p", "--"}, "")
		if directive != cobra.ShellCompDirectiveDefault {
			t.Fatalf("directive = %v, want Default", directive)
		}
		if len(got) != 0 {
			t.Fatalf("completions = %v, want none", got)
		}
	})

	t.Run("after a command word falls back to default completion", func(t *testing.T) {
		got, directive := completeProjectRunArgs(cmd, []string{"ls"}, "-p")
		if directive != cobra.ShellCompDirectiveDefault {
			t.Fatalf("directive = %v, want Default", directive)
		}
		if len(got) != 0 {
			t.Fatalf("completions = %v, want none", got)
		}
	})
}
