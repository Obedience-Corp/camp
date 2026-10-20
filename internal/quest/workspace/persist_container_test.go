//go:build container_fs

package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStorePush_AtomicIdempotentAndRejectsUserReplace(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "docs"))
	questDir := filepath.Join(root, ".campaign", "quests", "demo")
	mustMkdir(t, questDir)
	uiPath := filepath.Join(questDir, FileName)
	if err := os.WriteFile(uiPath, fixture(t, "conflicting-ownership.json"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(uiPath)
	if err != nil {
		t.Fatal(err)
	}

	store := &Store{}
	hijack := PushSpec{
		ID:       "user-main",
		Title:    "Nope",
		PaneTree: json.RawMessage(`{"id":"p","type":"pane","content":{"type":"empty"}}`),
	}
	if _, _, err := store.Push(context.Background(), root, uiPath, &hijack); err == nil {
		t.Fatal("expected user replace rejection")
	}
	afterFail, err := os.ReadFile(uiPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterFail) != string(before) {
		t.Fatal("malformed/rejected push must leave bytes unchanged")
	}

	var spec PushSpec
	if err := json.Unmarshal(fixture(t, "push-spec-valid.json"), &spec); err != nil {
		t.Fatal(err)
	}
	first, gen1, err := store.Push(context.Background(), root, uiPath, &spec)
	if err != nil {
		t.Fatal(err)
	}
	if first.Action != "appended" {
		t.Fatalf("action=%s", first.Action)
	}
	second, gen2, err := store.Push(context.Background(), root, uiPath, &spec)
	if err != nil {
		t.Fatal(err)
	}
	if second.Action != "replaced" {
		t.Fatalf("action=%s", second.Action)
	}
	raw, err := os.ReadFile(uiPath)
	if err != nil {
		t.Fatal(err)
	}
	if Generation(raw) != gen2 {
		t.Fatalf("on-disk generation mismatch")
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	tabs := doc["workspaceTabs"].([]any)
	agentCount := 0
	for _, tab := range tabs {
		m := tab.(map[string]any)
		if m["id"] == "agent-review" {
			agentCount++
			if m["origin"] != OriginAgent {
				t.Fatalf("origin=%v", m["origin"])
			}
		}
		if m["id"] == "user-main" && m["title"] != "User" {
			t.Fatalf("user tab mutated: %#v", m)
		}
	}
	if agentCount != 1 {
		t.Fatalf("agent tabs=%d gens=%s/%s", agentCount, gen1, gen2)
	}
}

func TestStorePush_ConcurrentIndependentTabs(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "docs"))
	questDir := filepath.Join(root, ".campaign", "quests", "demo")
	mustMkdir(t, questDir)
	uiPath := filepath.Join(questDir, FileName)
	if err := os.WriteFile(uiPath, fixture(t, "concurrent-additions-base.json"), 0o644); err != nil {
		t.Fatal(err)
	}

	specA := PushSpec{
		ID:       "agent-a",
		Title:    "A",
		PaneTree: json.RawMessage(`{"id":"pa","type":"pane","content":{"type":"file","filePath":"docs/a.md"}}`),
	}
	specB := PushSpec{
		ID:       "agent-b",
		Title:    "B",
		PaneTree: json.RawMessage(`{"id":"pb","type":"pane","content":{"type":"file","filePath":"docs/b.md"}}`),
	}

	errCh := make(chan error, 2)
	go func() {
		_, _, err := (&Store{}).Push(context.Background(), root, uiPath, &specA)
		errCh <- err
	}()
	go func() {
		_, _, err := (&Store{}).Push(context.Background(), root, uiPath, &specB)
		errCh <- err
	}()
	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("push: %v", err)
		}
	}
	raw, err := os.ReadFile(uiPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, `"agent-a"`) || !strings.Contains(body, `"agent-b"`) || !strings.Contains(body, `"user-main"`) {
		t.Fatalf("missing tabs: %s", body)
	}
}

func TestAcquireOSLock_HonorsContextCancel(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, LockFileName)
	holder, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := acquireOSLock(ctx, lockPath, 5*time.Second)
		errCh <- err
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected cancellation error")
		}
		if !strings.Contains(err.Error(), "cancel") && err != context.Canceled {
			// context.Canceled string is "context canceled"
			if err != context.Canceled && !strings.Contains(strings.ToLower(err.Error()), "canceled") {
				t.Fatalf("err=%v", err)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("lock wait did not honor cancellation")
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
