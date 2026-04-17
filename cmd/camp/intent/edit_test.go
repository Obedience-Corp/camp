package intent

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// newEditTestCmd returns a cobra.Command with all edit flags registered.
func newEditTestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:  "edit",
		RunE: func(cmd *cobra.Command, args []string) error { return nil },
	}
	flags := cmd.Flags()
	flags.String("body", "", "")
	flags.String("body-file", "", "")
	flags.String("append-body", "", "")
	flags.String("append-body-file", "", "")
	flags.String("title", "", "")
	flags.String("set-type", "", "")
	flags.String("set-status", "", "")
	flags.String("set-concept", "", "")
	flags.String("priority", "", "")
	flags.String("horizon", "", "")
	flags.String("author", "", "")
	return cmd
}

func TestValidateEditBodyFlags_NoFlags(t *testing.T) {
	cmd := newEditTestCmd()
	if err := validateEditBodyFlags(cmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEditBodyFlags_BodyOnly(t *testing.T) {
	cmd := newEditTestCmd()
	_ = cmd.Flags().Set("body", "some text")
	if err := validateEditBodyFlags(cmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEditBodyFlags_BodyFileOnly(t *testing.T) {
	cmd := newEditTestCmd()
	_ = cmd.Flags().Set("body-file", "file.txt")
	if err := validateEditBodyFlags(cmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEditBodyFlags_AppendBodyOnly(t *testing.T) {
	cmd := newEditTestCmd()
	_ = cmd.Flags().Set("append-body", "appended text")
	if err := validateEditBodyFlags(cmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEditBodyFlags_AppendBodyFileOnly(t *testing.T) {
	cmd := newEditTestCmd()
	_ = cmd.Flags().Set("append-body-file", "file.txt")
	if err := validateEditBodyFlags(cmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEditBodyFlags_BodyAndBodyFile(t *testing.T) {
	cmd := newEditTestCmd()
	_ = cmd.Flags().Set("body", "literal")
	_ = cmd.Flags().Set("body-file", "file.txt")
	err := validateEditBodyFlags(cmd)
	if err == nil {
		t.Fatal("expected error for --body + --body-file")
	}
	if !strings.Contains(err.Error(), "--body and --body-file are mutually exclusive") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEditBodyFlags_AppendAndAppendFile(t *testing.T) {
	cmd := newEditTestCmd()
	_ = cmd.Flags().Set("append-body", "text")
	_ = cmd.Flags().Set("append-body-file", "file.txt")
	err := validateEditBodyFlags(cmd)
	if err == nil {
		t.Fatal("expected error for --append-body + --append-body-file")
	}
	if !strings.Contains(err.Error(), "--append-body and --append-body-file are mutually exclusive") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEditBodyFlags_ReplaceAndAppend(t *testing.T) {
	tests := []struct {
		name    string
		replace string
		append  string
	}{
		{"body + append-body", "body", "append-body"},
		{"body + append-body-file", "body", "append-body-file"},
		{"body-file + append-body", "body-file", "append-body"},
		{"body-file + append-body-file", "body-file", "append-body-file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newEditTestCmd()
			_ = cmd.Flags().Set(tt.replace, "value")
			_ = cmd.Flags().Set(tt.append, "value")
			err := validateEditBodyFlags(cmd)
			if err == nil {
				t.Fatal("expected error for replace + append combination")
			}
			if !strings.Contains(err.Error(), "mutually exclusive") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestHasProgrammaticEditFlags_None(t *testing.T) {
	cmd := newEditTestCmd()
	if hasProgrammaticEditFlags(cmd) {
		t.Fatal("expected false when no programmatic flags set")
	}
}

func TestHasProgrammaticEditFlags_TitleSet(t *testing.T) {
	cmd := newEditTestCmd()
	_ = cmd.Flags().Set("title", "new title")
	if !hasProgrammaticEditFlags(cmd) {
		t.Fatal("expected true when --title set")
	}
}

func TestHasProgrammaticEditFlags_AllFlags(t *testing.T) {
	flags := []string{
		"title", "body", "body-file", "append-body", "append-body-file",
		"set-type", "set-status", "set-concept", "priority", "horizon", "author",
	}
	for _, f := range flags {
		t.Run(f, func(t *testing.T) {
			cmd := newEditTestCmd()
			_ = cmd.Flags().Set(f, "value")
			if !hasProgrammaticEditFlags(cmd) {
				t.Fatalf("expected true when --%s set", f)
			}
		})
	}
}

func TestBuildUpdateOptions_Title(t *testing.T) {
	cmd := newEditTestCmd()
	_ = cmd.Flags().Set("title", "new title")
	opts, err := buildUpdateOptions(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.Title == nil {
		t.Fatal("expected Title to be set")
	}
	if *opts.Title != "new title" {
		t.Fatalf("Title = %q, want %q", *opts.Title, "new title")
	}
	if opts.Body != nil {
		t.Fatal("expected Body to be nil")
	}
}

func TestBuildUpdateOptions_MultipleFields(t *testing.T) {
	cmd := newEditTestCmd()
	_ = cmd.Flags().Set("title", "updated")
	_ = cmd.Flags().Set("set-type", "feature")
	_ = cmd.Flags().Set("priority", "high")
	_ = cmd.Flags().Set("horizon", "now")
	_ = cmd.Flags().Set("author", "lance")

	opts, err := buildUpdateOptions(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if opts.Title == nil || *opts.Title != "updated" {
		t.Fatalf("Title mismatch")
	}
	if opts.Type == nil || string(*opts.Type) != "feature" {
		t.Fatalf("Type mismatch")
	}
	if opts.Priority == nil || string(*opts.Priority) != "high" {
		t.Fatalf("Priority mismatch")
	}
	if opts.Horizon == nil || string(*opts.Horizon) != "now" {
		t.Fatalf("Horizon mismatch")
	}
	if opts.Author == nil || *opts.Author != "lance" {
		t.Fatalf("Author mismatch")
	}
}

func TestBuildUpdateOptions_AppendBody(t *testing.T) {
	cmd := newEditTestCmd()
	_ = cmd.Flags().Set("append-body", "appended text")
	opts, err := buildUpdateOptions(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.Append == nil {
		t.Fatal("expected Append to be set")
	}
	if *opts.Append != "appended text" {
		t.Fatalf("Append = %q, want %q", *opts.Append, "appended text")
	}
	if opts.Body != nil {
		t.Fatal("expected Body to be nil when only append set")
	}
}

func TestBuildUpdateOptions_UnchangedFields(t *testing.T) {
	cmd := newEditTestCmd()
	// No flags set at all
	opts, err := buildUpdateOptions(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.Title != nil || opts.Body != nil || opts.Append != nil ||
		opts.Type != nil || opts.Status != nil || opts.Concept != nil ||
		opts.Author != nil || opts.Priority != nil || opts.Horizon != nil {
		t.Fatal("expected all fields to be nil when no flags changed")
	}
}
