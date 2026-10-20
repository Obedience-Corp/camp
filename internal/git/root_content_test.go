package git

import (
	"reflect"
	"testing"
)

func TestRenameSizeCompatibleMatchesGitsSizeBound(t *testing.T) {
	sizes := []int64{300, 4096}
	cases := map[int64]bool{150: true, 149: false, 600: true, 601: false, 8192: true, 8193: false, 3 << 30: false}
	for size, want := range cases {
		if got := renameSizeCompatible(size, sizes); got != want {
			t.Errorf("renameSizeCompatible(%d) = %v, want %v", size, got, want)
		}
	}
	if renameSizeCompatible(10, nil) {
		t.Error("nothing deleted means nothing can be a rename destination")
	}
}

func TestRenamePairsReadsSourceAndDestination(t *testing.T) {
	out := []byte("R100\x00notes/model.bin\x00videos/my-video/model.bin\x00R087\x00a b.md\x00videos/my-video/a b.md\x00")
	want := [][2]string{
		{"notes/model.bin", "videos/my-video/model.bin"},
		{"a b.md", "videos/my-video/a b.md"},
	}
	if got := renamePairs(out); !reflect.DeepEqual(got, want) {
		t.Fatalf("renamePairs = %v, want %v", got, want)
	}
}
