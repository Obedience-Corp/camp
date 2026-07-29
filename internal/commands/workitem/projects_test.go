package workitem

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func TestNormalizeProjects(t *testing.T) {
	t.Run("normalizes trailing slash and dedupes, first occurrence wins", func(t *testing.T) {
		got, err := normalizeProjects([]string{"projects/camp/", "projects/./camp", "projects/fest"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"projects/camp", "projects/fest"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %#v, want %#v", got, want)
		}
	})

	t.Run("empty after normalization returns validation error naming original input", func(t *testing.T) {
		_, err := normalizeProjects([]string{"/"})
		if err == nil {
			t.Fatal("expected error for a project that is empty after normalization")
		}
		var verr *camperrors.ValidationError
		if !errors.As(err, &verr) {
			t.Fatalf("expected ValidationError, got %T: %v", err, err)
		}
		if verr.Field != "project" {
			t.Errorf("Field = %q, want project", verr.Field)
		}
		if !strings.Contains(err.Error(), `"/"`) {
			t.Errorf("error must name the original input, got: %v", err)
		}
	})
}

func TestNormalizeExistingProjects(t *testing.T) {
	cases := []struct {
		name        string
		in          []string
		wantKept    []string
		wantDropped []string
	}{
		{"already clean, unchanged", []string{"projects/camp", "projects/fest"}, []string{"projects/camp", "projects/fest"}, nil},
		{"normalizes and dedupes", []string{"projects/camp/", "projects/./camp", "projects/fest"}, []string{"projects/camp", "projects/fest"}, nil},
		{"drops entries that normalize to empty", []string{"/", "projects/camp", "///"}, []string{"projects/camp"}, []string{"/", "///"}},
		{"all dropped", []string{"/"}, nil, []string{"/"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kept, dropped := normalizeExistingProjects(tc.in)
			if !reflect.DeepEqual(kept, tc.wantKept) {
				t.Errorf("kept = %#v, want %#v", kept, tc.wantKept)
			}
			if !reflect.DeepEqual(dropped, tc.wantDropped) {
				t.Errorf("dropped = %#v, want %#v", dropped, tc.wantDropped)
			}
		})
	}
}
