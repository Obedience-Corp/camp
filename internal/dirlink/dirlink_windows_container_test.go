//go:build windows && container_fs

package dirlink

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestUNCDirectoryLinkUnavailableTarget(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "network-project")
	target := `\\127.0.0.1\camp_dirlink_unavailable\project`
	sentinel := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("stay"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Create(CreateOptions{LinkPath: link, TargetPath: target})
	if err != nil {
		// A nonprivileged Windows container may reject directory symlinks.
		// The error must tell the user how to enable them, and Create must not
		// leave a partial link behind.
		if !strings.Contains(err.Error(), "Developer Mode") || !strings.Contains(err.Error(), "elevated") {
			t.Fatalf("UNC link error lacks symlink privilege guidance: %v", err)
		}
		if _, statErr := os.Lstat(link); !os.IsNotExist(statErr) {
			t.Fatalf("failed UNC link left an entry: %v", statErr)
		}
	} else {
		st, statErr := Lstat(link)
		if statErr != nil || st.Kind != KindSymlink {
			t.Fatalf("UNC link classification = %+v, %v; want directory symlink", st, statErr)
		}
		got, readErr := ReadTarget(link)
		if readErr != nil || !strings.EqualFold(filepath.Clean(got), filepath.Clean(target)) {
			t.Fatalf("UNC link target = %q, %v; want %q", got, readErr, target)
		}
		linkp, pathErr := syscall.UTF16PtrFromString(link)
		if pathErr != nil {
			t.Fatal(pathErr)
		}
		attrs, attrErr := syscall.GetFileAttributes(linkp)
		if attrErr != nil || attrs&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 {
			t.Fatalf("UNC link attributes = %#x, %v; want directory symlink", attrs, attrErr)
		}
		if err := Remove(link); err != nil {
			t.Fatalf("remove UNC link: %v", err)
		}
		if _, statErr := os.Lstat(link); !os.IsNotExist(statErr) {
			t.Fatalf("removed UNC link still exists: %v", statErr)
		}
	}
	if got, err := os.ReadFile(sentinel); err != nil || string(got) != "stay" {
		t.Fatalf("unrelated file changed during UNC link handling: %q, %v", got, err)
	}
}

func TestCrossVolumeDirectoryLinkSafeRemoval(t *testing.T) {
	root := t.TempDir()
	if strings.EqualFold(filepath.VolumeName(root), "D:") {
		t.Fatalf("test root %q must be on a different volume from D:", root)
	}
	target, err := os.MkdirTemp(`D:\`, "camp-dirlink-target-")
	if err != nil {
		t.Fatalf("create external target on D: fixture volume: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(target); err != nil {
			t.Errorf("clean external D: fixture: %v", err)
		}
	})
	if strings.EqualFold(filepath.VolumeName(root), filepath.VolumeName(target)) {
		t.Fatalf("link root %q and target %q must use different drive letters", root, target)
	}
	payload := filepath.Join(target, "keep.txt")
	if err := os.WriteFile(payload, []byte("external"), 0o644); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(root, "cross-volume")
	if err := Create(CreateOptions{LinkPath: link, TargetPath: target}); err != nil {
		t.Fatalf("create cross-volume link: %v", err)
	}
	st, err := Lstat(link)
	if err != nil || st.Kind != KindJunction || st.ReparseTag != ReparseTagMountPoint {
		t.Fatalf("cross-volume link classification = %+v, %v; want junction", st, err)
	}
	if got, err := os.ReadFile(filepath.Join(link, "keep.txt")); err != nil || string(got) != "external" {
		t.Fatalf("read through cross-volume link: %q, %v", got, err)
	}
	if err := Remove(link); err != nil {
		t.Fatalf("remove cross-volume link: %v", err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("cross-volume link still exists: %v", err)
	}
	if got, err := os.ReadFile(payload); err != nil || string(got) != "external" {
		t.Fatalf("external D: payload changed after unlink: %q, %v", got, err)
	}
}
