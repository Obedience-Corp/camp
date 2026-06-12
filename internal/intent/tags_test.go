package intent

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func TestValidateAndNormalizeTags(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		want    []string
		wantErr bool
	}{
		{"nil", nil, nil, false},
		{"trim and drop empties", []string{" personal ", "", "  "}, []string{"personal"}, false},
		{"dedup preserves order", []string{"a", "b", "a"}, []string{"a", "b"}, false},
		{"allowed separators", []string{"follow-up", "v1.2", "a/b", "c_d", "c++"}, []string{"follow-up", "v1.2", "a/b", "c_d", "c++"}, false},
		{"spaces allowed", []string{"in progress"}, []string{"in progress"}, false},
		{"comma rejected", []string{"a,b"}, nil, true},
		{"colon rejected", []string{"key:value"}, nil, true},
		{"bracket rejected", []string{"a]b"}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateAndNormalizeTags(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got nil (out=%v)", got)
				}
				if !errors.Is(err, camperrors.ErrInvalidInput) {
					t.Errorf("err = %v, want ErrInvalidInput", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCreateDirect_RejectsHostileTags(t *testing.T) {
	tmp := t.TempDir()
	svc := NewIntentService(tmp, filepath.Join(tmp, "intents"))
	ctx := context.Background()

	for _, bad := range []string{"a,b", "key: value"} {
		_, err := svc.CreateDirect(ctx, CreateOptions{
			Title:     "tagged intent",
			Type:      TypeIdea,
			Tags:      []string{bad},
			Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		})
		if !errors.Is(err, camperrors.ErrInvalidInput) {
			t.Errorf("CreateDirect with tag %q err = %v, want ErrInvalidInput", bad, err)
		}
	}
}

func TestCreateDirect_NormalizesTags(t *testing.T) {
	tmp := t.TempDir()
	svc := NewIntentService(tmp, filepath.Join(tmp, "intents"))
	ctx := context.Background()

	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "deduped tags",
		Type:      TypeIdea,
		Tags:      []string{" personal ", "personal", "reference"},
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect: %v", err)
	}
	if !slices.Equal(created.Tags, []string{"personal", "reference"}) {
		t.Errorf("tags = %v, want [personal reference]", created.Tags)
	}
}
