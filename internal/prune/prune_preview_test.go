//go:build !integration

package prune

import "testing"

// The preview position must never leak into a real pass: PreviewPrimaryAtBase
// drops the current-branch protection, which is only safe while nothing is
// being deleted.
func TestOptionsPreviewsPrimaryAtBase(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want bool
	}{
		{name: "default", opts: Options{}, want: false},
		{name: "dry-run only", opts: Options{DryRun: true}, want: false},
		{name: "preview flag without dry-run", opts: Options{PreviewPrimaryAtBase: true}, want: false},
		{name: "dry-run preview", opts: Options{DryRun: true, PreviewPrimaryAtBase: true}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.opts.previewsPrimaryAtBase(); got != tt.want {
				t.Fatalf("previewsPrimaryAtBase() = %v, want %v", got, tt.want)
			}
		})
	}
}
