package intent

import (
	"testing"
)

func TestDeduplicateIDs(t *testing.T) {
	tests := []struct {
		name string
		ids  []string
		want []string
	}{
		{
			name: "no duplicates",
			ids:  []string{"a", "b", "c"},
			want: []string{"a", "b", "c"},
		},
		{
			name: "with duplicates",
			ids:  []string{"a", "b", "a", "c", "b"},
			want: []string{"a", "b", "c"},
		},
		{
			name: "all same",
			ids:  []string{"x", "x", "x"},
			want: []string{"x"},
		},
		{
			name: "empty",
			ids:  []string{},
			want: []string{},
		},
		{
			name: "single",
			ids:  []string{"only"},
			want: []string{"only"},
		},
		{
			name: "preserves order",
			ids:  []string{"c", "a", "b", "a", "c"},
			want: []string{"c", "a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deduplicateIDs(tt.ids)
			if len(got) != len(tt.want) {
				t.Fatalf("deduplicateIDs() returned %d items, want %d", len(got), len(tt.want))
			}
			for i, id := range got {
				if id != tt.want[i] {
					t.Errorf("deduplicateIDs()[%d] = %q, want %q", i, id, tt.want[i])
				}
			}
		})
	}
}
