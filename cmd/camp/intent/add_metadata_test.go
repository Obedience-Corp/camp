package intent

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	intentcore "github.com/Obedience-Corp/camp/internal/intent"
)

func newAddMetadataTestCmd(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "add"}
	registerAddMetadataFlags(cmd.Flags())
	if err := cmd.Flags().Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return cmd
}

func TestResolveAddMetadata_AllFlags(t *testing.T) {
	cmd := newAddMetadataTestCmd(t,
		"--priority", "high", "--horizon", "now",
		"--blocked-by", " first-20260101-000001 ", "--blocked-by", "EX0001",
		"--depends-on", "second-20260101-000002",
		"--promotion-criteria", "  The contract is agreed  ",
	)

	meta, err := resolveAddMetadata(cmd, false)
	if err != nil {
		t.Fatalf("resolveAddMetadata: %v", err)
	}

	var opts intentcore.CreateOptions
	meta.apply(&opts)
	if opts.Priority != intentcore.PriorityHigh || opts.Horizon != intentcore.HorizonNow {
		t.Fatalf("priority/horizon = %q/%q", opts.Priority, opts.Horizon)
	}
	if !slices.Equal(opts.BlockedBy, []string{"first-20260101-000001", "EX0001"}) {
		t.Fatalf("BlockedBy = %q", opts.BlockedBy)
	}
	if !slices.Equal(opts.DependsOn, []string{"second-20260101-000002"}) {
		t.Fatalf("DependsOn = %q", opts.DependsOn)
	}
	if opts.PromotionCriteria != "The contract is agreed" {
		t.Fatalf("PromotionCriteria = %q", opts.PromotionCriteria)
	}
}

func TestResolveAddMetadata_NoFlagsLeavesDefaults(t *testing.T) {
	meta, err := resolveAddMetadata(newAddMetadataTestCmd(t), false)
	if err != nil {
		t.Fatalf("resolveAddMetadata: %v", err)
	}
	opts := intentcore.CreateOptions{Title: "kept"}
	meta.apply(&opts)
	if opts.Priority != "" || opts.Horizon != "" || opts.BlockedBy != nil ||
		opts.DependsOn != nil || opts.PromotionCriteria != "" || opts.Title != "kept" {
		t.Fatalf("unexpected options: %+v", opts)
	}
}

func TestResolveAddMetadata_RejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "priority", args: []string{"--priority", "urgent"}, want: []string{"--priority", `"urgent"`, "low, medium, high"}},
		{name: "empty priority", args: []string{"--priority", ""}, want: []string{"--priority"}},
		{name: "horizon", args: []string{"--horizon", "soon"}, want: []string{"--horizon", `"soon"`, "now, next, later, someday"}},
		{name: "empty blocked-by", args: []string{"--blocked-by", ""}, want: []string{"--blocked-by", "must not be empty"}},
		{name: "empty depends-on", args: []string{"--depends-on", "ok", "--depends-on", " "}, want: []string{"--depends-on", "must not be empty"}},
		{name: "multi-line depends-on", args: []string{"--depends-on", "a\nb"}, want: []string{"--depends-on", "single line"}},
		{name: "invalid utf-8 blocked-by", args: []string{"--blocked-by", "caf\xe9"}, want: []string{"--blocked-by", "not valid UTF-8"}},
		{name: "invalid utf-8 criteria", args: []string{"--promotion-criteria", "caf\xe9"}, want: []string{"--promotion-criteria", "not valid UTF-8"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveAddMetadata(newAddMetadataTestCmd(t, tt.args...), false)
			if !errors.Is(err, camperrors.ErrInvalidInput) {
				t.Fatalf("error = %v, want ErrInvalidInput", err)
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

func TestResolveAddMetadata_NoteRejectsEveryMetadataFlag(t *testing.T) {
	values := map[string]string{
		flagPriority:          "high",
		flagHorizon:           "now",
		flagBlockedBy:         "a",
		flagDependsOn:         "b",
		flagPromotionCriteria: "c",
	}
	for _, name := range addMetadataFlags {
		t.Run(name, func(t *testing.T) {
			_, err := resolveAddMetadata(newAddMetadataTestCmd(t, "--"+name, values[name]), true)
			if !errors.Is(err, camperrors.ErrInvalidInput) {
				t.Fatalf("error = %v, want ErrInvalidInput", err)
			}
			if !strings.Contains(err.Error(), "--note cannot be combined with --"+name) {
				t.Errorf("error %q does not name the conflict", err)
			}
		})
	}

	if _, err := resolveAddMetadata(newAddMetadataTestCmd(t), true); err != nil {
		t.Fatalf("note without metadata flags: %v", err)
	}
}

func TestIntentAdd_RegistersMetadataFlags(t *testing.T) {
	cmd := newIntentAddCommand()
	for _, name := range addMetadataFlags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("camp idea add is missing --%s", name)
		}
	}
	for _, want := range []string{"ONE-CALL CAPTURE", "--body-file -", "--priority"} {
		if !strings.Contains(cmd.Long, want) {
			t.Errorf("add help does not mention %q", want)
		}
	}
}
