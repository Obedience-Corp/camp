package cmdutil

import "testing"

func TestPlanAutoWriteLeavesDeclaredRootsOut(t *testing.T) {
	sizes := map[string]int64{
		"notes.md":                  20,
		"videos/my-video/broll.mp4": 3 << 20,
		"releases/big.bin":          30 << 20,
	}
	paths, additions, foreground := planAutoWrite(
		[]string{"notes.md"},
		[]string{"videos/my-video/broll.mp4", "videos/my-video/takes/"},
		[]string{"videos/my-video"},
		[]string{"projects/camp"},
		func(rel string) (int64, bool) {
			size, ok := sizes[rel]
			return size, ok
		},
		10<<20,
	)
	if foreground {
		t.Fatal("files inside a declared root must not force the full guard")
	}
	if len(paths) != 1 || paths[0] != "notes.md" {
		t.Fatalf("paths = %v, want notes.md only", paths)
	}
	if len(additions) != 1 || additions[0].files != 1 || additions[0].dirs != 1 {
		t.Fatalf("additions = %+v", additions)
	}
	if additions[0].bytes != 3<<20 {
		t.Fatalf("bytes = %d", additions[0].bytes)
	}
}

func TestPlanAutoWriteStopsForANewLargeFile(t *testing.T) {
	_, _, foreground := planAutoWrite(
		nil,
		[]string{"releases/big.bin"},
		nil,
		nil,
		func(string) (int64, bool) { return 30 << 20, true },
		10<<20,
	)
	if !foreground {
		t.Fatal("a new over-threshold file must stay in the foreground so the report prints")
	}
}

func TestPlanAutoWriteStopsForAnUntrackedDirectory(t *testing.T) {
	_, _, foreground := planAutoWrite(
		nil,
		[]string{"vendor/"},
		nil,
		nil,
		func(string) (int64, bool) { return 0, false },
		10<<20,
	)
	if !foreground {
		t.Fatal("an untracked directory outside a declared root must stay in the foreground")
	}
}

func TestPlanAutoWriteIgnoresCampStateDirectories(t *testing.T) {
	paths, additions, foreground := planAutoWrite(
		nil,
		[]string{".campaign/artifacts/", "videos/my-video/broll.mp4", "caption.md"},
		[]string{"videos/my-video"},
		nil,
		func(rel string) (int64, bool) {
			if rel == "videos/my-video/broll.mp4" {
				return 3 << 20, true
			}
			return 12, true
		},
		10<<20,
	)
	if foreground {
		t.Fatal("camp's own untracked directories must not force the full guard")
	}
	if len(paths) != 1 || paths[0] != "caption.md" {
		t.Fatalf("paths = %v", paths)
	}
	if len(additions) != 1 || additions[0].files != 1 {
		t.Fatalf("additions = %+v", additions)
	}
}

func TestPlanAutoWriteSkipsSubmodules(t *testing.T) {
	paths, _, foreground := planAutoWrite(
		[]string{"projects/camp", "readme.md"},
		nil,
		nil,
		[]string{"projects/camp"},
		func(string) (int64, bool) { return 10, true },
		10<<20,
	)
	if foreground {
		t.Fatal("a submodule pointer must be skipped, not treated as new content")
	}
	if len(paths) != 1 || paths[0] != "readme.md" {
		t.Fatalf("paths = %v", paths)
	}
}
