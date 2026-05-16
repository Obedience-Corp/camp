package main

import (
	"reflect"
	"testing"
)

func TestNormalizeAutoWriteAlias(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "root commit",
			args: []string{"camp", "commit", "-aw", "-m", "msg", "-aw=false"},
			want: []string{"camp", "commit", "--auto-write", "-m", "msg", "-aw=false"},
		},
		{
			name: "project commit",
			args: []string{"camp", "project", "commit", "-aw", "-m", "msg"},
			want: []string{"camp", "project", "commit", "--auto-write", "-m", "msg"},
		},
		{
			name: "worktrees commit with global flag",
			args: []string{"camp", "--verbose", "worktrees", "commit", "-aw"},
			want: []string{"camp", "--verbose", "worktrees", "commit", "--auto-write"},
		},
		{
			name: "unrelated command",
			args: []string{"camp", "status", "-aw"},
			want: []string{"camp", "status", "-aw"},
		},
		{
			name: "plugin style command",
			args: []string{"camp", "external-tool", "-aw"},
			want: []string{"camp", "external-tool", "-aw"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeAutoWriteAlias(tc.args)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("normalizeAutoWriteAlias() = %#v, want %#v", got, tc.want)
			}
		})
	}
}
