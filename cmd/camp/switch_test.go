package main

import (
	"context"
	"testing"
	"time"

	"github.com/obediencecorp/camp/internal/config"
)

func newTestRegistry(campaigns ...config.RegisteredCampaign) *config.Registry {
	reg := config.NewRegistry()
	for _, c := range campaigns {
		_ = reg.Register(c.ID, c.Name, c.Path, c.Type)
	}
	return reg
}

func TestSwitchLookup(t *testing.T) {
	reg := newTestRegistry(
		config.RegisteredCampaign{
			ID:         "aaaa-1111",
			Name:       "obey-campaign",
			Path:       "/home/user/obey-campaign",
			Type:       "product",
			LastAccess: time.Now(),
		},
		config.RegisteredCampaign{
			ID:         "bbbb-2222",
			Name:       "side-project",
			Path:       "/home/user/side-project",
			Type:       "personal",
			LastAccess: time.Now(),
		},
		config.RegisteredCampaign{
			ID:         "cccc-3333",
			Name:       "research-ai",
			Path:       "/home/user/research-ai",
			Type:       "research",
			LastAccess: time.Now(),
		},
	)

	tests := []struct {
		name    string
		query   string
		want    string
		wantErr bool
	}{
		{
			name:  "exact name match",
			query: "obey-campaign",
			want:  "aaaa-1111",
		},
		{
			name:  "exact ID match",
			query: "bbbb-2222",
			want:  "bbbb-2222",
		},
		{
			name:  "ID prefix match",
			query: "cccc",
			want:  "cccc-3333",
		},
		{
			name:    "no match",
			query:   "nonexistent",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, ok := reg.Get(tt.query)
			if tt.wantErr {
				if ok {
					t.Fatalf("expected no match for %q, got %q", tt.query, c.Name)
				}
				return
			}
			if !ok {
				t.Fatalf("expected match for %q, got none", tt.query)
			}
			if c.ID != tt.want {
				t.Errorf("Get(%q).ID = %q, want %q", tt.query, c.ID, tt.want)
			}
		})
	}
}

func TestSwitchEmptyRegistry(t *testing.T) {
	reg := config.NewRegistry()
	if reg.Len() != 0 {
		t.Fatalf("expected empty registry, got %d", reg.Len())
	}
	_, ok := reg.Get("anything")
	if ok {
		t.Fatal("expected no match from empty registry")
	}
}

func TestSwitchContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := config.LoadRegistry(ctx)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}
