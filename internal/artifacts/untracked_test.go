package artifacts

import (
	"reflect"
	"testing"

	"github.com/Obedience-Corp/camp/internal/stageguard"
)

func TestGroupByRootFilesUnderDeepestRoot(t *testing.T) {
	roots := []string{"videos", "videos/cut", "renders"}
	violations := []stageguard.GuardViolation{
		{Kind: stageguard.OverThreshold, Path: "videos/cut/b.mp4", Size: 2},
		{Kind: stageguard.OverThreshold, Path: "videos/raw.mov", Size: 5},
		{Kind: stageguard.OverThreshold, Path: "videos/cut/a.mp4", Size: 3},
		{Kind: stageguard.OverThreshold, Path: "videos-old/x.mp4", Size: 7},
	}

	got := groupByRoot(roots, violations)
	want := []UntrackedRoot{
		{Root: "videos", Files: []UntrackedFile{{Path: "videos/raw.mov", Size: 5}}, Bytes: 5},
		{Root: "videos/cut", Files: []UntrackedFile{{Path: "videos/cut/a.mp4", Size: 3}, {Path: "videos/cut/b.mp4", Size: 2}}, Bytes: 5},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("groupByRoot() =\n%+v\nwant\n%+v", got, want)
	}

	if paths := UntrackedPaths(got); !reflect.DeepEqual(paths, []string{"videos/raw.mov", "videos/cut/a.mp4", "videos/cut/b.mp4"}) {
		t.Errorf("UntrackedPaths() = %q", paths)
	}
}
