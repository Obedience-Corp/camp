package release

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunProfile_ChannelAnnotation(t *testing.T) {
	root := &cobra.Command{Use: "test"}
	stable := &cobra.Command{Use: "deploy", GroupID: "ops"}
	dev := &cobra.Command{Use: "experiment", GroupID: "ops"}
	MarkDevOnly(dev)

	root.AddGroup(&cobra.Group{ID: "ops", Title: "Ops:"})
	root.AddCommand(stable, dev)

	out := captureProfile(t, root)

	if !strings.Contains(out, "deploy") {
		t.Fatal("expected deploy in output")
	}
	if !strings.Contains(out, "experiment") {
		t.Fatal("expected experiment in output")
	}

	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "deploy") && !strings.Contains(line, "stable") {
			t.Errorf("deploy should be stable: %s", line)
		}
		if strings.HasPrefix(line, "experiment") && !strings.Contains(line, "dev") {
			t.Errorf("experiment should be dev: %s", line)
		}
	}
}

func TestRunProfile_SortedAlphabetically(t *testing.T) {
	root := &cobra.Command{Use: "test"}
	root.AddCommand(
		&cobra.Command{Use: "zebra"},
		&cobra.Command{Use: "alpha"},
		&cobra.Command{Use: "middle"},
	)

	out := captureProfile(t, root)
	lines := nonEmptyLines(out)

	// First line is header, then alpha, middle, zebra.
	if len(lines) < 4 {
		t.Fatalf("expected 4 lines, got %d: %s", len(lines), out)
	}
	if !strings.HasPrefix(lines[1], "alpha") {
		t.Errorf("expected alpha first, got: %s", lines[1])
	}
	if !strings.HasPrefix(lines[2], "middle") {
		t.Errorf("expected middle second, got: %s", lines[2])
	}
	if !strings.HasPrefix(lines[3], "zebra") {
		t.Errorf("expected zebra third, got: %s", lines[3])
	}
}

func TestRunProfile_SkipsHelpAndSelf(t *testing.T) {
	root := &cobra.Command{Use: "test"}
	root.AddCommand(
		&cobra.Command{Use: "real"},
		&cobra.Command{Use: "help"},
		&cobra.Command{Use: "build-profile"},
		&cobra.Command{Use: "completion"},
	)

	out := captureProfile(t, root)

	if strings.Contains(out, "help") && strings.Contains(out, "\nhelp") {
		t.Error("help should be filtered out")
	}
	if strings.Contains(out, "build-profile") {
		t.Error("build-profile should be filtered out")
	}
	if strings.Contains(out, "completion") {
		t.Error("completion should be filtered out")
	}
	if !strings.Contains(out, "real") {
		t.Error("real command should be present")
	}
}

func TestRunProfile_HiddenWithoutAnnotationSkipped(t *testing.T) {
	root := &cobra.Command{Use: "test"}
	hidden := &cobra.Command{Use: "secret", Hidden: true}
	root.AddCommand(hidden)

	out := captureProfile(t, root)

	if strings.Contains(out, "secret") {
		t.Error("hidden command without annotation should be skipped")
	}
}

func TestRunProfile_HiddenWithAnnotationShown(t *testing.T) {
	root := &cobra.Command{Use: "test"}
	hidden := &cobra.Command{Use: "internal-dev", Hidden: true}
	MarkDevOnly(hidden)
	root.AddCommand(hidden)

	out := captureProfile(t, root)

	if !strings.Contains(out, "internal-dev") {
		t.Error("hidden command with release annotation should be shown")
	}
}

func TestRunProfile_EmptyGroupShowsDash(t *testing.T) {
	root := &cobra.Command{Use: "test"}
	root.AddCommand(&cobra.Command{Use: "ungrouped"})

	out := captureProfile(t, root)

	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "ungrouped") && !strings.Contains(line, "-") {
			t.Errorf("ungrouped command should show dash: %s", line)
		}
	}
}

// captureProfile redirects stdout and runs runProfile.
func captureProfile(t *testing.T, root *cobra.Command) string {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	if err := runProfile(root); err != nil {
		w.Close()
		os.Stdout = old
		t.Fatal(err)
	}
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func nonEmptyLines(s string) []string {
	var result []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			result = append(result, line)
		}
	}
	return result
}
