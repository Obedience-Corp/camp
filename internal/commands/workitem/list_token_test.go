package workitem

import (
	"strings"
	"testing"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

func TestListTokenLabel(t *testing.T) {
	tests := []struct {
		count int
		want  string
	}{
		{0, "-"},
		{-1, "-"},
		{1, "1"},
		{42, "42"},
		{999, "999"},
		{1000, "1.0k"},
		{1500, "1.5k"},
		{9999, "10.0k"},
		{1_000_000, "1.0M"},
		{1_500_000, "1.5M"},
	}
	for _, tc := range tests {
		got := listTokenLabel(tc.count)
		if got != tc.want {
			t.Errorf("listTokenLabel(%d) = %q, want %q", tc.count, got, tc.want)
		}
	}
}

func TestListRow_IncludesTokenColumn(t *testing.T) {
	item := wkitem.WorkItem{
		Key:          "intent:foo",
		Title:        "Foo",
		RelativePath: "workflow/intent/foo",
		TokenCount:   1234,
	}
	row := listRow(item)
	// 1.2k should appear in the rendered row for a 1234-token item.
	if !strings.Contains(row, "1.2k") {
		t.Errorf("expected token label '1.2k' in row, got: %q", row)
	}
}

func TestListRow_ZeroTokensShowsDash(t *testing.T) {
	item := wkitem.WorkItem{
		Key:          "intent:bar",
		Title:        "Bar",
		RelativePath: "workflow/intent/bar",
		TokenCount:   0,
	}
	row := listRow(item)
	// A zero count should show "-" in the token column. The padded dash
	// segment should be present in the rendered row.
	if !strings.Contains(row, "-") {
		t.Errorf("expected '-' token label in row for zero tokens, got: %q", row)
	}
	// And it must NOT contain a numeric token count like "0" as the token column.
	// The age column may contain letters but the token column should be dashes.
	if strings.Contains(row, "0t") {
		t.Errorf("did not expect '0t' in row, got: %q", row)
	}
}
