package initcmd

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func withColorProfile(t *testing.T, p termenv.Profile) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(p)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

func TestFormatHermesContextWarning(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		files []string
		want  string
		quiet bool
	}{
		{name: "none", files: nil, quiet: true},
		{
			name:  "hermes hidden",
			files: []string{".hermes.md"},
			want:  "warning: .hermes.md sits next to AGENTS.md; Hermes loads exactly one project-context file per session (first match wins), so that file replaces AGENTS.md instead of overlaying it. " + hermesContextDocsURL,
		},
		{
			name:  "two files",
			files: []string{".hermes.md", "AGENTS.override.md"},
			want:  "warning: .hermes.md and AGENTS.override.md sit next to AGENTS.md; Hermes loads exactly one project-context file per session (first match wins), so those files replace AGENTS.md instead of overlaying it. " + hermesContextDocsURL,
		},
		{
			name:  "all three",
			files: []string{".hermes.md", "HERMES.md", "AGENTS.override.md"},
			want:  "warning: .hermes.md, HERMES.md, and AGENTS.override.md sit next to AGENTS.md; Hermes loads exactly one project-context file per session (first match wins), so those files replace AGENTS.md instead of overlaying it. " + hermesContextDocsURL,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := formatHermesContextWarning(tc.files)
			if tc.quiet {
				if got != "" {
					t.Fatalf("formatHermesContextWarning() = %q, want empty", got)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("formatHermesContextWarning() = %q, want %q", got, tc.want)
			}
			if strings.Contains(got, "\n") {
				t.Fatalf("warning must be one line, got %q", got)
			}
		})
	}
}

func TestListHermesContextOverrides(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		fsys fstest.MapFS
		want []string
	}{
		{name: "empty", fsys: fstest.MapFS{}, want: nil},
		{
			name: "only agents",
			fsys: fstest.MapFS{"AGENTS.md": {Data: []byte("# agents")}},
			want: nil,
		},
		{
			name: "hidden hermes",
			fsys: fstest.MapFS{".hermes.md": {Data: []byte("x")}},
			want: []string{".hermes.md"},
		},
		{
			name: "all overrides in priority order",
			fsys: fstest.MapFS{
				"AGENTS.override.md": {Data: []byte("o")},
				"HERMES.md":          {Data: []byte("h")},
				".hermes.md":         {Data: []byte("d")},
			},
			want: []string{".hermes.md", "HERMES.md", "AGENTS.override.md"},
		},
		{
			name: "directory is not a context file",
			fsys: fstest.MapFS{".hermes.md": {Mode: fs.ModeDir}},
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := listHermesContextOverrides(context.Background(), tc.fsys)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("listHermesContextOverrides() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestListHermesContextOverrides_ContextCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fsys := fstest.MapFS{".hermes.md": {Data: []byte("x")}}
	if got := listHermesContextOverrides(ctx, fsys); len(got) != 0 {
		t.Fatalf("canceled context returned %v", got)
	}
}

func TestEmitHermesContextWarningFS_OneLineOnStderr(t *testing.T) {
	withColorProfile(t, termenv.Ascii)

	var human, errOut strings.Builder
	emitHermesContextWarningFS(context.Background(), fstest.MapFS{
		".hermes.md": {Data: []byte("x")},
	}, Writers{HumanOut: &human, ErrOut: &errOut})

	if got := human.String(); got != "" {
		t.Fatalf("HumanOut = %q, want empty (diagnostics go to stderr)", got)
	}
	got := errOut.String()
	if strings.Count(got, "\n") != 1 {
		t.Fatalf("want one line on ErrOut, got %q", got)
	}
	for _, want := range []string{"warning:", ".hermes.md", "replaces AGENTS.md", hermesContextDocsURL} {
		if !strings.Contains(got, want) {
			t.Errorf("ErrOut missing %q in %q", want, got)
		}
	}
}

func TestEmitHermesContextWarningFS_QuietWhenNone(t *testing.T) {
	var errOut strings.Builder
	emitHermesContextWarningFS(context.Background(), fstest.MapFS{
		"AGENTS.md": {Data: []byte("# agents")},
	}, Writers{ErrOut: &errOut})
	if got := errOut.String(); got != "" {
		t.Fatalf("ErrOut = %q, want empty", got)
	}
}

func TestEmitHermesContextWarningFS_CanceledContextIsQuiet(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var errOut strings.Builder
	emitHermesContextWarningFS(ctx, fstest.MapFS{".hermes.md": {Data: []byte("x")}}, Writers{ErrOut: &errOut})
	if got := errOut.String(); got != "" {
		t.Fatalf("canceled emit wrote %q", got)
	}
}
