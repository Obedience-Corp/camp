package intent

import (
	"slices"
	"testing"
)

func TestMergeTags(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want []string
	}{
		{"flag and overlay union", []string{"urgent"}, []string{"backend"}, []string{"urgent", "backend"}},
		{"dedup across both", []string{"x", "y"}, []string{"y", "z"}, []string{"x", "y", "z"}},
		{"drops empties", []string{"", "a"}, []string{"", "b"}, []string{"a", "b"}},
		{"flag only", []string{"flagged"}, nil, []string{"flagged"}},
		{"overlay only", nil, []string{"picked"}, []string{"picked"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mergeTags(tt.a, tt.b); !slices.Equal(got, tt.want) {
				t.Errorf("mergeTags(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
