//go:build container_fs

package project

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// ListLocations stages real git repositories, so these run inside the pooled
// container rather than on a developer's machine (decision D007).

// ListLocations skips the git subprocesses List spends filling URL and Type and
// comparing commit dates. That saving is only safe if it still reports the same
// projects, so pin the two walks against each other on a campaign where dedup
// has nothing to remove.
func TestListLocations_ReportsTheSameProjectsAsList(t *testing.T) {
	root := stageLocationsCampaign(t, map[string]string{
		"camp": "git@github.com:Obedience-Corp/camp.git",
		"fest": "git@github.com:Obedience-Corp/fest.git",
	})
	ctx := context.Background()

	full, err := List(ctx, root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	lite, err := ListLocations(ctx, root)
	if err != nil {
		t.Fatalf("ListLocations: %v", err)
	}

	if len(lite) != len(full) {
		t.Fatalf("ListLocations returned %d projects %v, List returned %d %v",
			len(lite), projectNames(lite), len(full), projectNames(full))
	}
	for i := range full {
		switch {
		case lite[i].Name != full[i].Name:
			t.Errorf("project %d name = %q, List says %q", i, lite[i].Name, full[i].Name)
		case lite[i].Path != full[i].Path:
			t.Errorf("project %q path = %q, List says %q", lite[i].Name, lite[i].Path, full[i].Path)
		case lite[i].Source != full[i].Source:
			t.Errorf("project %q source = %q, List says %q", lite[i].Name, lite[i].Source, full[i].Source)
		}
	}
}

// The two fields ListLocations drops are exactly the ones that cost a
// subprocess. They must read as absent rather than as a value a caller could
// act on, because acting on them is what List is for.
func TestListLocations_LeavesTheEnrichedFieldsEmpty(t *testing.T) {
	root := stageLocationsCampaign(t, map[string]string{
		"camp": "git@github.com:Obedience-Corp/camp.git",
	})

	projects, err := ListLocations(context.Background(), root)
	if err != nil {
		t.Fatalf("ListLocations: %v", err)
	}
	if len(projects) == 0 {
		t.Fatal("ListLocations found no projects")
	}
	for _, p := range projects {
		if p.URL != "" {
			t.Errorf("project %q URL = %q, want empty", p.Name, p.URL)
		}
		if p.Type != "" {
			t.Errorf("project %q Type = %q, want empty", p.Name, p.Type)
		}
	}
}

// List drops a checkout that shares a remote with a more recently committed
// one. ListLocations cannot: the comparison needs the URL it deliberately does
// not fetch. That is the documented difference between them, so pin it — a
// caller needing one entry per remote has to use List.
func TestListLocations_KeepsCheckoutsThatShareARemote(t *testing.T) {
	const shared = "git@github.com:Obedience-Corp/camp.git"
	root := stageLocationsCampaign(t, map[string]string{
		"camp":      shared,
		"camp-copy": shared,
	})
	ctx := context.Background()

	full, err := List(ctx, root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(full) != 1 {
		t.Fatalf("List returned %v, want a single deduped checkout", projectNames(full))
	}

	lite, err := ListLocations(ctx, root)
	if err != nil {
		t.Fatalf("ListLocations: %v", err)
	}
	if len(lite) != 2 {
		t.Fatalf("ListLocations returned %v, want both checkouts", projectNames(lite))
	}
}

// stageLocationsCampaign builds a campaign whose projects/ holds one committed
// git repo per name, each with the given origin URL.
func stageLocationsCampaign(t *testing.T, remotes map[string]string) string {
	t.Helper()

	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	projectsDir := filepath.Join(root, "projects")
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	for name, remote := range remotes {
		path := filepath.Join(projectsDir, name)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		initGitRepoWithRemoteAndCommit(t, path, remote, "commit in "+name)
		if err := os.WriteFile(filepath.Join(path, "go.mod"), []byte("module "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func projectNames(projects []Project) []string {
	names := make([]string, len(projects))
	for i, p := range projects {
		names[i] = p.Name
	}
	return names
}
