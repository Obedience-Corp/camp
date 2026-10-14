package dirlink

import "testing"

func TestKnownDirectoryLinkTag(t *testing.T) {
	t.Parallel()
	cases := []struct {
		tag  uint32
		want bool
	}{
		{ReparseTagSymlink, true},
		{ReparseTagMountPoint, true},
		{0x80000013, false}, // dedup
		{0, false},
	}
	for _, tc := range cases {
		if got := KnownDirectoryLinkTag(tc.tag); got != tc.want {
			t.Errorf("KnownDirectoryLinkTag(%#x) = %t, want %t", tc.tag, got, tc.want)
		}
	}
}

func TestUNCPathPrefersDirectorySymlink(t *testing.T) {
	t.Parallel()
	if !uncPath(`\\server\share\project`) {
		t.Fatal("expected UNC path to prefer directory symlink strategy")
	}
	if uncPath(`/local/unix/style`) {
		t.Fatal("unix-style path must not be treated as UNC")
	}
}
