package promote

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyIntoFestivalIngest_SkipsAbsentIngestDir(t *testing.T) {
	campaignRoot := t.TempDir()
	festivalDir := filepath.Join(campaignRoot, "festivals", "planning", "my-festival-abc123")
	if err := os.MkdirAll(festivalDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(festivalDir) error = %v", err)
	}

	srcDoc := filepath.Join(campaignRoot, "intent.md")
	if err := os.WriteFile(srcDoc, []byte("# Test"), 0o644); err != nil {
		t.Fatalf("WriteFile(srcDoc) error = %v", err)
	}

	if CopyIntoFestivalIngest(campaignRoot, "planning", "my-festival-abc123", srcDoc) {
		t.Fatal("CopyIntoFestivalIngest returned true, want false")
	}

	if _, err := os.Stat(filepath.Join(festivalDir, "001_INGEST")); !os.IsNotExist(err) {
		t.Fatalf("001_INGEST should not have been created, stat err = %v", err)
	}
	entries, err := os.ReadDir(festivalDir)
	if err != nil {
		t.Fatalf("ReadDir(festivalDir) error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("festival dir should remain empty, got %d entries", len(entries))
	}
}

func TestExtractFestStderr(t *testing.T) {
	cmd := exec.Command("sh", "-c", "echo 'fest: festival already exists' >&2; exit 1")
	_, err := cmd.Output()
	if err == nil {
		t.Fatal("expected non-zero exit")
	}

	got := extractFestStderr(err)
	if !strings.Contains(got, "fest: festival already exists") {
		t.Fatalf("extractFestStderr() = %q, want stderr text", got)
	}
}

func TestCopyTree_CopiesContentAndSkipsBookkeeping(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "out")

	writeFile(t, filepath.Join(src, "a.md"), "alpha")
	writeFile(t, filepath.Join(src, "sub", "b.md"), "beta")
	writeFile(t, filepath.Join(src, ".workitem"), "marker")
	writeFile(t, filepath.Join(src, ".workflow.yaml"), "wf")
	writeFile(t, filepath.Join(src, "nested", ".workitem", "c.md"), "skip-dir")

	if err := CopyTree(src, dst); err != nil {
		t.Fatalf("CopyTree() error = %v", err)
	}

	if got := readFile(t, filepath.Join(dst, "a.md")); got != "alpha" {
		t.Fatalf("a.md = %q, want %q", got, "alpha")
	}
	if got := readFile(t, filepath.Join(dst, "sub", "b.md")); got != "beta" {
		t.Fatalf("sub/b.md = %q, want %q", got, "beta")
	}

	skipped := []string{".workitem", ".workflow.yaml", filepath.Join("nested", ".workitem")}
	for _, rel := range skipped {
		if _, err := os.Stat(filepath.Join(dst, rel)); !os.IsNotExist(err) {
			t.Fatalf("%s should have been skipped, stat err = %v", rel, err)
		}
	}
}

func TestRecordPromotion(t *testing.T) {
	t.Run("propagates record to save", func(t *testing.T) {
		var got PromotionRecord
		if err := RecordPromotion("planning/my-festival-abc123", func(r PromotionRecord) error {
			got = r
			return nil
		}); err != nil {
			t.Fatalf("RecordPromotion() error = %v", err)
		}
		if got.PromotedTo != "planning/my-festival-abc123" {
			t.Fatalf("PromotedTo = %q, want %q", got.PromotedTo, "planning/my-festival-abc123")
		}
		if got.PromotedAt.IsZero() {
			t.Fatal("PromotedAt should be set")
		}
	})

	t.Run("returns save error", func(t *testing.T) {
		want := errors.New("save failed")
		if err := RecordPromotion("festival", func(PromotionRecord) error {
			return want
		}); !errors.Is(err, want) {
			t.Fatalf("RecordPromotion() error = %v, want %v", err, want)
		}
	})
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	return string(data)
}
