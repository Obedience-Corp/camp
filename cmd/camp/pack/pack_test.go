package pack

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/obey-shared/festivalbundle"
)

func writeCampaign(t *testing.T, root, id, name string) {
	t.Helper()
	dir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := "id: " + id + "\nname: " + name + "\ntype: product\n"
	if err := os.WriteFile(filepath.Join(dir, "campaign.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readInfoFromZip(t *testing.T, festivalPath string) festivalbundle.Info {
	t.Helper()
	r, err := zip.OpenReader(festivalPath)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Name == "info.json" {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer rc.Close()
			var info festivalbundle.Info
			if err := json.NewDecoder(rc).Decode(&info); err != nil {
				t.Fatal(err)
			}
			return info
		}
	}
	t.Fatal("info.json missing")
	return festivalbundle.Info{}
}

func TestFromMetaForSource_insideCampaign(t *testing.T) {
	camp := t.TempDir()
	writeCampaign(t, camp, "camp-aaa", "alpha")
	src := filepath.Join(camp, "workflow", "explore", "topic")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "note.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	from := fromMetaForSource(context.Background(), src)
	if from == nil {
		t.Fatal("expected From metadata")
	}
	if from.CampaignID != "camp-aaa" || from.CampaignName != "alpha" {
		t.Fatalf("got %+v", from)
	}
	if from.RelativePath != "workflow/explore/topic" {
		t.Fatalf("relative_path = %q", from.RelativePath)
	}
}

func TestFromMetaForSource_outsideCampaign(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "note.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Standing inside a campaign must not stamp provenance onto /tmp source.
	camp := t.TempDir()
	writeCampaign(t, camp, "camp-bbb", "beta")
	t.Chdir(camp)

	from := fromMetaForSource(context.Background(), src)
	if from != nil {
		t.Fatalf("expected no From for out-of-campaign source, got %+v", from)
	}
}

func TestFromMetaForSource_differentCampaign(t *testing.T) {
	// Caller cwd = campaign A; source under campaign B → From should be B.
	campA := t.TempDir()
	writeCampaign(t, campA, "camp-a", "A")
	campB := t.TempDir()
	writeCampaign(t, campB, "camp-b", "B")
	src := filepath.Join(campB, "workflow", "design", "thing")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("d\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(campA)

	from := fromMetaForSource(context.Background(), src)
	if from == nil {
		t.Fatal("expected From from campaign B")
	}
	if from.CampaignID != "camp-b" || from.CampaignName != "B" {
		t.Fatalf("got %+v want campaign B", from)
	}
	if from.RelativePath != "workflow/design/thing" {
		t.Fatalf("relative_path = %q", from.RelativePath)
	}
}

func TestPackUsesSourceCampaignNotCwd(t *testing.T) {
	campA := t.TempDir()
	writeCampaign(t, campA, "caller-camp", "Caller")
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "note.md"), []byte("solo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(campA)

	out := filepath.Join(t.TempDir(), "x.festival")
	// Use library via run path: set flags and invoke runPack through cobra is heavy;
	// assert fromMeta + Pack options path by packing with resolved From only when set.
	from := fromMetaForSource(context.Background(), outside)
	if from != nil {
		t.Fatalf("outside source must not inherit caller campaign, got %+v", from)
	}
	info, err := festivalbundle.Pack(context.Background(), outside, out, festivalbundle.PackOptions{
		Kind:            festivalbundle.KindNote,
		Name:            "solo",
		WriteSentRecord: false,
		From:            from, // nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got := readInfoFromZip(t, out)
	if got.From != nil {
		t.Fatalf("info.From should be nil, got %+v", got.From)
	}
	if info.Bundle.ID == "" {
		t.Fatal("empty id")
	}
}

func TestPackInsideCampaignStampsFrom(t *testing.T) {
	camp := t.TempDir()
	writeCampaign(t, camp, "camp-in", "Inside")
	src := filepath.Join(camp, "workflow", "explore", "e1")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "n.md"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Cwd elsewhere
	t.Chdir(t.TempDir())

	from := fromMetaForSource(context.Background(), src)
	out := filepath.Join(t.TempDir(), "in.festival")
	_, err := festivalbundle.Pack(context.Background(), src, out, festivalbundle.PackOptions{
		Kind: festivalbundle.KindExplore,
		Name: "e1",
		From: from,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := readInfoFromZip(t, out)
	if got.From == nil || got.From.CampaignID != "camp-in" {
		t.Fatalf("From = %+v", got.From)
	}
}
