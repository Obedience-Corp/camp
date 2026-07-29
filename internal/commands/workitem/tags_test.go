package workitem

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func TestNormalizeTag(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"already normalized", "public-launch", "public-launch"},
		{"uppercase", "Public-Launch", "public-launch"},
		{"spaces", "public launch", "public-launch"},
		{"underscores", "public_launch", "public-launch"},
		{"repeated spaces collapse", "public   launch", "public-launch"},
		{"repeated hyphens collapse", "public---launch", "public-launch"},
		{"leading and trailing trimmed", "  -Public Launch-  ", "public-launch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeTag(tc.in); got != tc.want {
				t.Errorf("normalizeTag(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeTags(t *testing.T) {
	t.Run("normalizes and dedupes order-preserving, first occurrence wins", func(t *testing.T) {
		got, err := normalizeTags([]string{"Public-Launch", "public launch", "Schema", "schema"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"public-launch", "schema"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %#v, want %#v", got, want)
		}
	})

	t.Run("normalization still invalid returns validation error naming original input", func(t *testing.T) {
		_, err := normalizeTags([]string{"screaming CASE 日本"})
		if err == nil {
			t.Fatal("expected error for a tag that is invalid after normalization")
		}
		var verr *camperrors.ValidationError
		if !errors.As(err, &verr) {
			t.Fatalf("expected ValidationError, got %T: %v", err, err)
		}
		if verr.Field != "tag" {
			t.Errorf("Field = %q, want tag", verr.Field)
		}
		if !strings.Contains(err.Error(), "screaming CASE 日本") {
			t.Errorf("error must name the original input, got: %v", err)
		}
	})

	t.Run("boundary rejections", func(t *testing.T) {
		for _, in := range []string{strings.Repeat("a", 65), "", "foo!bar"} {
			if _, err := normalizeTags([]string{in}); err == nil {
				t.Errorf("normalizeTags(%q) expected error, got nil", in)
			}
		}
	})
}

func TestNormalizeExistingTags(t *testing.T) {
	cases := []struct {
		name        string
		in          []string
		wantKept    []string
		wantDropped []string
	}{
		{"already clean, unchanged", []string{"a", "b"}, []string{"a", "b"}, nil},
		{"normalizes and dedupes", []string{"Public-Launch", "public launch", "schema"}, []string{"public-launch", "schema"}, nil},
		{"drops entries that normalize to empty", []string{"---", "schema", "  "}, []string{"schema"}, []string{"---", "  "}},
		{"all dropped", []string{"---"}, nil, []string{"---"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kept, dropped := normalizeExistingTags(tc.in)
			if !reflect.DeepEqual(kept, tc.wantKept) {
				t.Errorf("kept = %#v, want %#v", kept, tc.wantKept)
			}
			if !reflect.DeepEqual(dropped, tc.wantDropped) {
				t.Errorf("dropped = %#v, want %#v", dropped, tc.wantDropped)
			}
		})
	}
}
