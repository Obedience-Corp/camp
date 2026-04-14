package intent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// newTestCmd returns a cobra.Command with body/body-file flags registered,
// ready for unit-testing resolveBody.
func newTestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:  "test",
		RunE: func(cmd *cobra.Command, args []string) error { return nil },
	}
	cmd.Flags().String("body", "", "")
	cmd.Flags().String("body-file", "", "")
	return cmd
}

func TestResolveBody_NoFlags(t *testing.T) {
	cmd := newTestCmd()
	body, set, err := resolveBody(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if set {
		t.Fatal("expected set=false when no flags provided")
	}
	if body != "" {
		t.Fatalf("expected empty body, got %q", body)
	}
}

func TestResolveBody_BodyFlag(t *testing.T) {
	cmd := newTestCmd()
	if err := cmd.Flags().Set("body", "hello world"); err != nil {
		t.Fatalf("Set(body) error: %v", err)
	}

	body, set, err := resolveBody(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !set {
		t.Fatal("expected set=true when --body provided")
	}
	if body != "hello world" {
		t.Fatalf("body = %q, want %q", body, "hello world")
	}
}

func TestResolveBody_BodyFileFlag(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "body.txt")
	if err := os.WriteFile(tmp, []byte("from file"), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	cmd := newTestCmd()
	if err := cmd.Flags().Set("body-file", tmp); err != nil {
		t.Fatalf("Set(body-file) error: %v", err)
	}

	body, set, err := resolveBody(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !set {
		t.Fatal("expected set=true when --body-file provided")
	}
	if body != "from file" {
		t.Fatalf("body = %q, want %q", body, "from file")
	}
}

func TestResolveBody_MutualExclusivity(t *testing.T) {
	cmd := newTestCmd()
	if err := cmd.Flags().Set("body", "literal"); err != nil {
		t.Fatalf("Set(body) error: %v", err)
	}
	if err := cmd.Flags().Set("body-file", "some.txt"); err != nil {
		t.Fatalf("Set(body-file) error: %v", err)
	}

	_, _, err := resolveBody(cmd)
	if err == nil {
		t.Fatal("expected error for mutually exclusive flags")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestResolveBody_BodyFileMissing(t *testing.T) {
	cmd := newTestCmd()
	if err := cmd.Flags().Set("body-file", "/nonexistent/path/to/file.txt"); err != nil {
		t.Fatalf("Set(body-file) error: %v", err)
	}

	_, _, err := resolveBody(cmd)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestResolveBody_EmptyBodyFlag(t *testing.T) {
	cmd := newTestCmd()
	// Explicitly set --body to empty string (distinct from "not set")
	if err := cmd.Flags().Set("body", ""); err != nil {
		t.Fatalf("Set(body) error: %v", err)
	}

	body, set, err := resolveBody(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !set {
		t.Fatal("expected set=true when --body explicitly provided (even empty)")
	}
	if body != "" {
		t.Fatalf("body = %q, want empty", body)
	}
}

func TestReadBodySource_SizeLimit(t *testing.T) {
	// Create a file just over the size limit
	tmp := filepath.Join(t.TempDir(), "large.txt")
	data := make([]byte, maxBodyFileSize+1)
	for i := range data {
		data[i] = 'x'
	}
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	_, err := readBodySource(tmp)
	if err == nil {
		t.Fatal("expected error for file exceeding size limit")
	}
	if !strings.Contains(err.Error(), "10 MiB") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadBodySource_ExactLimit(t *testing.T) {
	// Create a file exactly at the limit - should succeed
	tmp := filepath.Join(t.TempDir(), "exact.txt")
	data := make([]byte, maxBodyFileSize)
	for i := range data {
		data[i] = 'y'
	}
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	content, err := readBodySource(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(content) != maxBodyFileSize {
		t.Fatalf("content length = %d, want %d", len(content), maxBodyFileSize)
	}
}
