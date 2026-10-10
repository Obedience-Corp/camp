//go:build container_fs

package explore

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestReadRegularRejectsSpecialFiles(t *testing.T) {
	root := t.TempDir()
	regular := filepath.Join(root, "regular.md")
	mustWrite(t, regular, "readable content")
	fifo := filepath.Join(root, "pending.md")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.md")
	if err := os.Symlink(regular, link); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(root, "socket.md")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	for _, path := range []string{root, fifo, link, socket} {
		result := make(chan error, 1)
		go func() { _, err := ReadRegular(context.Background(), path, 256); result <- err }()
		select {
		case err := <-result:
			if err == nil {
				t.Fatalf("read nonregular file %s", path)
			}
		case <-time.After(time.Second):
			t.Fatalf("blocked reading %s", path)
		}
	}
	if got, err := ReadRegular(context.Background(), regular, 4); err != nil || string(got) != "read" {
		t.Fatalf("bounded read: %q, %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadRegular(ctx, regular, 256); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	// The Unix opener must itself remain nonblocking if a regular path is
	// replaced by a FIFO between Lstat and open; ReadRegular then rejects f.Stat.
	result := make(chan error, 1)
	go func() {
		f, err := openRegular(fifo)
		if err == nil {
			err = f.Close()
		}
		result <- err
	}()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("open blocks on replacement FIFO")
	}
}

func TestBuildSpecialMarkdownDoesNotBlock(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".dungeon", "completed")
	mustWrite(t, filepath.Join(dir, "normal.md"), "readable")
	if err := unix.Mkfifo(filepath.Join(dir, "pending.md"), 0600); err != nil {
		t.Fatal(err)
	}
	type result struct {
		index Index
		err   error
	}
	done := make(chan result, 1)
	go func() { idx, err := Build(context.Background(), root); done <- result{idx, err} }()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if len(got.index.Items) != 2 {
			t.Fatalf("items: %+v", got.index.Items)
		}
		for _, item := range got.index.Items {
			if filepath.Base(item.Path) == "pending.md" && item.Warning == "" {
				t.Fatal("special file lacks warning")
			}
		}
	case <-time.After(time.Second):
		t.Fatal("feed hangs on FIFO")
	}
}

func TestBuildHoldingDirectoryItems(t *testing.T) {
	root := t.TempDir()
	itemDir := filepath.Join(root, ".dungeon", "design-one")
	mustWrite(t, filepath.Join(itemDir, ".workitem"), "title: Design one\nref: WI-one\n")
	mustWrite(t, filepath.Join(itemDir, "README.md"), "# Design\n\nHolding summary.\n")
	mustWrite(t, filepath.Join(itemDir, "notes.md"), "Not a feed row.")
	mustWrite(t, filepath.Join(root, ".dungeon", "custom-status", "README.md"), "Custom status child")
	idx, err := Build(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	holding, err := Apply(idx, Query{Status: StatusHolding})
	if err != nil {
		t.Fatal(err)
	}
	if len(holding.Items) != 1 {
		t.Fatalf("holding: %+v", holding.Items)
	}
	// Previously cached indices classified this directory as a status. Force a
	// rebuild on upgrade even when the dungeon fingerprint has not changed.
	cache := filepath.Join(root, ".cache")
	mustWrite(t, filepath.Join(cache, "index.json"), `{"schema_version":"camp-dungeon-explore-cache/v2","index":{"items":[]}}`)
	if _, hit, err := LoadCache(cache); err != nil || hit {
		t.Fatalf("old holding layout cache accepted: %v", err)
	}
	if err := SaveCache(cache, idx); err != nil {
		t.Fatal(err)
	}
	if cached, hit, err := LoadCache(cache); err != nil || !hit || len(cached.Items) != len(idx.Items) {
		t.Fatalf("current cache failed roundtrip: %v", err)
	}
	item := holding.Items[0]
	if item.Path != ".dungeon/design-one" || !item.IsDir || item.Kind != KindWorkitem || item.Title != "Design one" || item.ID != "WI-one" || item.Summary != "Holding summary." {
		t.Fatalf("holding item: %+v", item)
	}
	all, err := Apply(idx, Query{Status: "all"})
	if err != nil || len(all.Items) != 2 {
		t.Fatalf("all: %+v, %v", all.Items, err)
	}
	custom, err := Apply(idx, Query{Status: "custom-status"})
	if err != nil || len(custom.Items) != 1 {
		t.Fatalf("custom status: %+v, %v", custom.Items, err)
	}
	mustWrite(t, filepath.Join(itemDir, ".workitem"), "title: Revised holding item\nref: WI-one\n")
	refreshed, changed, err := Refresh(context.Background(), root, idx)
	if err != nil || !changed {
		t.Fatalf("refresh: %v, changed=%v", err, changed)
	}
	holding, err = Apply(refreshed, Query{Status: StatusHolding})
	if err != nil || holding.Items[0].Title != "Revised holding item" {
		t.Fatalf("holding metadata stale: %+v, %v", holding, err)
	}
}
