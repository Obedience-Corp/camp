package git

import "testing"

func TestResolveRelativeSubmoduleURL(t *testing.T) {
	tests := []struct{ remote, relative, want string }{
		{"https://example.com/org/team/parent.git", "../child.git", "https://example.com/org/team/child.git"},
		{"https://example.com/org/team/parent.git", "../../child.git", "https://example.com/org/child.git"},
		{"git@example.com:org/team/parent.git", "../../child.git", "git@example.com:org/child.git"},
		{"/tmp/org/team/parent.git", "../child.git", "/tmp/org/team/child.git"},
		{"ssh://git@example.com/org/parent.git", "./child.git", "ssh://git@example.com/org/parent.git/child.git"},
		{"https://example.com/org/parent", "./child", "https://example.com/org/parent/child"},
		{"git@example.com:org/parent", "./child", "git@example.com:org/parent/child"},
		{"/tmp/org/parent", "./child", "/tmp/org/parent/child"},
	}
	for _, test := range tests {
		if got := resolveRelativeSubmoduleURL(test.remote, test.relative); got != test.want {
			t.Errorf("resolve %q against %q = %q, want %q", test.relative, test.remote, got, test.want)
		}
	}
}
