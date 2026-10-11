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
