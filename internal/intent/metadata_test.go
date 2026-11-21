package intent

import (
	"errors"
	"slices"
	"strings"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func TestParsePriority(t *testing.T) {
	for _, want := range AllPriorities() {
		got, err := ParsePriority(string(want))
		if err != nil || got != want {
			t.Fatalf("ParsePriority(%q) = %q, %v", want, got, err)
		}
	}

	_, err := ParsePriority("urgent")
	if !errors.Is(err, camperrors.ErrInvalidInput) {
		t.Fatalf("ParsePriority(urgent) error = %v, want ErrInvalidInput", err)
	}
	for _, want := range []string{`"urgent"`, "low, medium, high"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestParseHorizon(t *testing.T) {
	for _, want := range AllHorizons() {
		got, err := ParseHorizon(string(want))
		if err != nil || got != want {
			t.Fatalf("ParseHorizon(%q) = %q, %v", want, got, err)
		}
	}

	_, err := ParseHorizon("soon")
	if !errors.Is(err, camperrors.ErrInvalidInput) {
		t.Fatalf("ParseHorizon(soon) error = %v, want ErrInvalidInput", err)
	}
	if !strings.Contains(err.Error(), "now, next, later, someday") {
		t.Errorf("error %q does not list the valid horizons", err)
	}
}

func TestParseType(t *testing.T) {
	for _, want := range AllTypes() {
		got, err := ParseType(string(want))
		if err != nil || got != want {
			t.Fatalf("ParseType(%q) = %q, %v", want, got, err)
		}
	}

	for _, bad := range []string{"bogus", "", "Bug"} {
		if _, err := ParseType(bad); !errors.Is(err, camperrors.ErrInvalidInput) {
			t.Errorf("ParseType(%q) error = %v, want ErrInvalidInput", bad, err)
		}
	}
}

func TestNormalizeDependencyRefs(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		want    []string
		wantErr bool
	}{
		{name: "nil stays nil"},
		{name: "trims and keeps order", in: []string{" b-20260101-000001 ", "AC0002"}, want: []string{"b-20260101-000001", "AC0002"}},
		{name: "drops duplicates", in: []string{"a", "b", "a"}, want: []string{"a", "b"}},
		{name: "keeps yaml metacharacters", in: []string{`odd: "ref" #1`}, want: []string{`odd: "ref" #1`}},
		{name: "rejects empty", in: []string{"a", ""}, wantErr: true},
		{name: "rejects whitespace only", in: []string{"   "}, wantErr: true},
		{name: "rejects newline", in: []string{"a\nb"}, wantErr: true},
		{name: "rejects invalid utf-8", in: []string{"caf\xe9"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeDependencyRefs("blocked_by", tt.in)
			if tt.wantErr {
				if !errors.Is(err, camperrors.ErrInvalidInput) {
					t.Fatalf("error = %v, want ErrInvalidInput", err)
				}
				if !strings.Contains(err.Error(), "blocked_by") {
					t.Errorf("error %q does not name the field", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizePromotionCriteria(t *testing.T) {
	got, err := NormalizePromotionCriteria("  The contract is agreed\n")
	if err != nil || got != "The contract is agreed" {
		t.Fatalf("got %q, %v", got, err)
	}
	if got, err := NormalizePromotionCriteria("   "); err != nil || got != "" {
		t.Fatalf("whitespace-only criteria = %q, %v, want empty", got, err)
	}
	if _, err := NormalizePromotionCriteria("caf\xe9"); !errors.Is(err, camperrors.ErrInvalidInput) {
		t.Fatalf("invalid UTF-8 error = %v, want ErrInvalidInput", err)
	}
}

func TestNormalizeCreateOptions_RejectsInvalidMetadata(t *testing.T) {
	tests := []struct {
		name string
		opts CreateOptions
	}{
		{name: "priority", opts: CreateOptions{Priority: "high\nassigned_to: someone"}},
		{name: "horizon", opts: CreateOptions{Horizon: "soon"}},
		{name: "blocked_by", opts: CreateOptions{BlockedBy: []string{""}}},
		{name: "depends_on", opts: CreateOptions{DependsOn: []string{"a\nb"}}},
		{name: "promotion_criteria", opts: CreateOptions{PromotionCriteria: "\xff"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := normalizeCreateOptions(tt.opts); !errors.Is(err, camperrors.ErrInvalidInput) {
				t.Fatalf("error = %v, want ErrInvalidInput", err)
			}
		})
	}

	got, err := normalizeCreateOptions(CreateOptions{
		Priority:          PriorityHigh,
		Horizon:           HorizonNow,
		BlockedBy:         []string{" a ", "a"},
		PromotionCriteria: " ready ",
	})
	if err != nil {
		t.Fatalf("valid options: %v", err)
	}
	if got.Priority != PriorityHigh || got.Horizon != HorizonNow ||
		!slices.Equal(got.BlockedBy, []string{"a"}) || got.PromotionCriteria != "ready" {
		t.Fatalf("unexpected normalized options: %+v", got)
	}

	if _, err := normalizeCreateOptions(CreateOptions{Title: "defaults"}); err != nil {
		t.Fatalf("empty metadata should be accepted: %v", err)
	}
}
