//go:build container_fs

package settings

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	fixtureCampID    = "00000000-0000-4000-8000-000000000001"
	externalSentinel = "SENTINEL-EXTERNAL-BYTES\n"
)

// Cancellation becomes observable as soon as the new bytes are published,
// exercising the real operation's post-commit readback failure boundary.
type cancelAfterPublication struct {
	context.Context
	cancel  context.CancelFunc
	path    string
	content []byte
}

func (c cancelAfterPublication) Err() error {
	data, err := os.ReadFile(c.path)
	if err == nil && bytes.Equal(data, c.content) {
		c.cancel()
	}
	return c.Context.Err()
}

func TestDocumentFS_AppliedWriteReportsReadbackFailure(t *testing.T) {
	svc, root := newDocumentFixture(t)
	before, after := []byte("before\n"), []byte("persisted\n")
	writeRel(t, root, "notes/readback.txt", before)
	base, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx := cancelAfterPublication{Context: base, cancel: cancel, path: campFile(root, "notes/readback.txt"), content: after}
	_, err := svc.Set(ctx, "notes/readback.txt", RevisionOf(before), after)
	var op *OpError
	if !errors.As(err, &op) || op.Code != ReadbackFailedCode || !op.Applied || op.Path != "notes/readback.txt" || op.CampID != fixtureCampID {
		t.Fatalf("post-commit cancellation = %#v (%v)", op, err)
	}
	assertFile(t, root, "notes/readback.txt", after)
	snap, err := svc.Get(t.Context(), "notes/readback.txt")
	if err != nil || snap.Revision != RevisionOf(after) {
		t.Fatalf("retry readback = %#v, %v", snap, err)
	}
}

func TestDocumentFS_CatalogInventoriesCustomFilesAndAbsentSchemas(t *testing.T) {
	svc, root := newDocumentFixture(t)
	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret-not-in-catalog\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeRel(t, root, "custom/tools/run.py", []byte("# comment\nprint(\"hi\")\n"))
	writeRel(t, root, "custom/tools/run.sh", []byte("#!/bin/sh\necho hi\n"))
	writeRel(t, root, "custom/theme.toml", []byte("# theme\nname = \"camp\"\n"))
	writeRel(t, root, "custom/app.ini", []byte("; comment\n[section]\nkey=value\n"))
	writeRel(t, root, "custom/feed.jsonl", []byte("{\"a\":1}\n{\"b\":2}\n"))
	if err := os.Mkdir(campFile(root, "custom/empty-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../campaign.yaml", campFile(root, "custom/linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../outside", campFile(root, "custom/outside-dir")); err != nil {
		t.Fatal(err)
	}

	cat, err := svc.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if cat.SchemaVersion != DocumentSchemaVersion || cat.CampID != fixtureCampID || cat.CampRoot != svc.Root || cat.Truncated {
		t.Fatalf("catalog identity truncated=%t camp=%s root=%s", cat.Truncated, cat.CampID, cat.CampRoot)
	}
	if cat.Limits != DefaultLimits() {
		t.Fatalf("limits = %+v", cat.Limits)
	}
	paths := make([]string, len(cat.Documents))
	byPath := make(map[string]DocumentEntry, len(cat.Documents))
	for i, doc := range cat.Documents {
		paths[i] = doc.Path
		if _, dup := byPath[doc.Path]; dup {
			t.Fatalf("duplicate catalog path %s", doc.Path)
		}
		byPath[doc.Path] = doc
		if strings.Contains(doc.Path, "secret.txt") || doc.Path == "custom/outside-dir/secret.txt" {
			t.Fatalf("catalog followed a symlink to %s", doc.Path)
		}
	}
	if !slices.IsSorted(paths) {
		t.Fatalf("catalog paths are not sorted: %v", paths)
	}

	authored := []string{
		"custom/tools/run.py",
		"custom/tools/run.sh",
		"custom/theme.toml",
		"custom/app.ini",
		"custom/feed.jsonl",
	}
	extraDirs := []string{"custom", "custom/tools", "custom/empty-dir"}
	if len(cat.Documents) != len(KnownPaths())+len(authored)+len(extraDirs)+2 {
		t.Fatalf("catalog has %d documents", len(cat.Documents))
	}
	for _, rel := range authored {
		doc := catalogEntry(t, byPath, rel)
		if !doc.Exists || doc.Format != DocumentText || !doc.Capabilities.SourceEdit || doc.Format == DocumentBinary {
			t.Fatalf("%s = format %s edit %t exists %t", rel, doc.Format, doc.Capabilities.SourceEdit, doc.Exists)
		}
	}
	for _, rel := range extraDirs {
		doc := catalogEntry(t, byPath, rel)
		if !doc.Exists || doc.Format != DocumentDirectory || doc.Capabilities.SourceEdit || doc.Capabilities.Reason == "" {
			t.Fatalf("directory %s = %+v reason %q", rel, doc.Format, doc.Capabilities.Reason)
		}
	}
	linked := catalogEntry(t, byPath, "custom/linked")
	if linked.Format != DocumentSymlink || linked.LinkTarget != "../campaign.yaml" || !linked.Exists || linked.Capabilities.SourceEdit || linked.Capabilities.Reason == "" {
		t.Fatalf("file symlink = %+v", linked)
	}
	dirLink := catalogEntry(t, byPath, "custom/outside-dir")
	if dirLink.Format != DocumentSymlink || dirLink.LinkTarget != "../../outside" || dirLink.Capabilities.SourceEdit || dirLink.Capabilities.Reason == "" {
		t.Fatalf("directory symlink = %+v", dirLink)
	}

	manifest := catalogEntry(t, byPath, "campaign.yaml")
	if manifest.ID != "campaign_manifest" || !manifest.Exists || manifest.Format != DocumentYAML || !manifest.Capabilities.SourceEdit {
		t.Fatalf("manifest = %+v", manifest)
	}
	for _, rel := range KnownPaths() {
		if rel == "campaign.yaml" {
			continue
		}
		doc := catalogEntry(t, byPath, rel)
		want, wantErr := Classify(rel, DocumentState{Kind: KindMissing, CampID: fixtureCampID})
		if wantErr != nil {
			t.Fatal(wantErr)
		}
		if doc.Exists || doc.ID != want.ID || doc.Format != want.Format || doc.Title != want.Title ||
			doc.Capabilities.Create != want.Capabilities.Create || doc.Capabilities.Form != want.Capabilities.Form ||
			doc.Capabilities.SourceEdit != want.Capabilities.SourceEdit || len(doc.Fields) != len(want.Fields) {
			t.Fatalf("absent %s got id=%s format=%s create=%t form=%t fields=%d", rel, doc.ID, doc.Format, doc.Capabilities.Create, doc.Capabilities.Form, len(doc.Fields))
		}
	}
	if _, err := os.Lstat(campFile(root, "settings")); !os.IsNotExist(err) {
		t.Fatal("catalog scaffolded settings/")
	}
}

func TestDocumentFS_SetPreservesExactSource(t *testing.T) {
	svc, root := newDocumentFixture(t)
	ctx := t.Context()
	cases := []struct {
		rel  string
		body string
	}{
		{rel: "custom/tools/run.py", body: "# comment\nprint(\"hi\")\n"},
		{rel: "custom/tools/run.sh", body: "#!/bin/sh\necho hi\n"},
		{rel: "custom/theme.toml", body: "# theme\nname = \"camp\"\n"},
		{rel: "custom/app.ini", body: "; comment\n[section]\nkey=value\n"},
		{rel: "custom/feed.jsonl", body: "{\"a\":1}\n{\"b\":2}\n"},
		{rel: "notes/exact.yaml", body: "# keep me\ntitle: old # trailing\ncustom_note: stay\nitems:\n  - one\n  - two\n"},
	}
	seed := []byte("seed\n")
	for _, tc := range cases {
		writeRel(t, root, tc.rel, seed)
		body := []byte(tc.body)
		snap, err := svc.Set(ctx, tc.rel, RevisionOf(seed), body)
		if err != nil {
			t.Fatalf("set %s: %v", tc.rel, err)
		}
		assertAppliedText(t, snap, tc.rel, body)
		assertFile(t, root, tc.rel, body)
		got, err := svc.Get(ctx, tc.rel)
		if err != nil {
			t.Fatalf("get %s: %v", tc.rel, err)
		}
		if got.Applied || got.Content == nil || *got.Content != tc.body || got.Revision != RevisionOf(body) {
			t.Fatalf("get %s applied=%t revision=%s", tc.rel, got.Applied, got.Revision)
		}
	}
}

func TestDocumentFS_PatchPreservesYAMLCommentsUnknownKeysAndLists(t *testing.T) {
	svc, root := newDocumentFixture(t)
	ctx := t.Context()
	original := []byte("# keep me\ntitle: old # trailing\ncustom_note: stay\ndescription: gone\nitems:\n  - one\n  - two\n")
	writeRel(t, root, "notes/page.yaml", original)
	patch := []byte(`{"changes":[{"op":"set","path":["title"],"value":"new"},{"op":"set","path":["items"],"value":["two","one"]},{"op":"remove","path":["description"]}]}`)
	snap, err := svc.Patch(ctx, "notes/page.yaml", RevisionOf(original), patch)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Applied || snap.Path != "notes/page.yaml" {
		t.Fatalf("patch snapshot path=%s applied=%t", snap.Path, snap.Applied)
	}
	text := string(readCamp(t, root, "notes/page.yaml"))
	for _, want := range []string{"# keep me", "# trailing", "custom_note", "new"} {
		if !strings.Contains(text, want) {
			t.Fatalf("patched source missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "description") || strings.Contains(text, "gone") {
		t.Fatalf("removed field remained:\n%s", text)
	}
	two := strings.Index(text, "- two")
	one := strings.Index(text, "- one")
	if two < 0 || one < 0 || two > one {
		t.Fatalf("list order lost:\n%s", text)
	}
	if snap.Revision != RevisionOf(readCamp(t, root, "notes/page.yaml")) {
		t.Fatal("patch revision does not match the persisted bytes")
	}
}

func TestDocumentFS_InvalidCandidateWritesNothing(t *testing.T) {
	svc, root := newDocumentFixture(t)
	ctx := t.Context()
	valid := []byte("# stable\npaths:\n  projects: projects\n")
	writeRel(t, root, "settings/jumps.yaml", valid)
	before := stampCampaign(t, root)
	invalid := []byte("paths:\n  projects: ../outside\n")

	checked, err := svc.Validate(ctx, "settings/jumps.yaml", invalid)
	if err != nil || checked.Valid || !HasErrors(checked.Diagnostics) {
		t.Fatalf("validate = valid:%t err:%v diags:%+v", checked.Valid, err, checked.Diagnostics)
	}
	assertStamp(t, root, before)

	snap, err := svc.Set(ctx, "settings/jumps.yaml", RevisionOf(valid), invalid)
	assertUnapplied(t, snap, err)
	assertStamp(t, root, before)

	created, err := svc.Create(ctx, "settings/fresh.yaml", RevisionAbsent, []byte("completed_runs: nope\n"))
	assertUnapplied(t, created, err)
	assertStamp(t, root, before)

	patched, err := svc.Patch(ctx, "settings/jumps.yaml", RevisionOf(valid), []byte(`{"changes":`))
	assertUnapplied(t, patched, err)
	assertStamp(t, root, before)

	rejected, err := svc.Patch(ctx, "settings/jumps.yaml", RevisionOf(valid), []byte(`{"changes":[{"op":"set","path":["paths","projects"],"value":"../outside"}]}`))
	assertUnapplied(t, rejected, err)
	assertStamp(t, root, before)
	assertFile(t, root, "settings/jumps.yaml", valid)
}

func TestDocumentFS_RevisionConflictPreservesExternalBytes(t *testing.T) {
	svc, root := newDocumentFixture(t)
	ctx := t.Context()
	original := []byte("title: v1\n")
	external := []byte("title: v2\n")
	writeRel(t, root, "notes/doc.yaml", original)
	rev := RevisionOf(original)
	writeRel(t, root, "notes/doc.yaml", external)
	outside := filepath.Join(root, "outside-bytes.txt")
	if err := os.WriteFile(outside, []byte(externalSentinel), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := svc.Set(ctx, "notes/doc.yaml", rev, []byte("title: next\n"))
	requireConflict(t, err)
	assertFile(t, root, "notes/doc.yaml", external)

	_, err = svc.Patch(ctx, "notes/doc.yaml", rev, []byte(`{"changes":[{"op":"set","path":["title"],"value":"next"}]}`))
	requireConflict(t, err)
	assertFile(t, root, "notes/doc.yaml", external)

	_, err = svc.Delete(ctx, "notes/doc.yaml", rev)
	requireConflict(t, err)
	assertFile(t, root, "notes/doc.yaml", external)

	got, err := os.ReadFile(outside)
	if err != nil || string(got) != externalSentinel {
		t.Fatalf("outside bytes = %q %v", got, err)
	}
}

func TestDocumentFS_CreateRenameDeleteStaleAndOccupied(t *testing.T) {
	svc, root := newDocumentFixture(t)
	ctx := t.Context()
	if err := os.Mkdir(campFile(root, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	created := []byte("created\n")
	snap, err := svc.Create(ctx, "notes/made.txt", RevisionAbsent, created)
	if err != nil {
		t.Fatal(err)
	}
	assertAppliedText(t, snap, "notes/made.txt", created)
	if snap.SchemaVersion != DocumentSchemaVersion || snap.CampID != fixtureCampID || snap.CampRoot != svc.Root {
		t.Fatalf("create identity camp=%s root=%s", snap.CampID, snap.CampRoot)
	}
	assertFile(t, root, "notes/made.txt", created)

	_, err = svc.Create(ctx, "notes/made.txt", RevisionAbsent, []byte("nope\n"))
	requireConflict(t, err)
	assertFile(t, root, "notes/made.txt", created)

	_, err = svc.Create(ctx, "notes/other.txt", RevisionOf(created), []byte("x\n"))
	requireConflict(t, err)
	assertAbsent(t, root, "notes/other.txt")

	renamed, err := svc.Rename(ctx, "notes/made.txt", "notes/moved.txt", RevisionOf(created))
	if err != nil {
		t.Fatal(err)
	}
	assertAppliedText(t, renamed, "notes/moved.txt", created)
	assertAbsent(t, root, "notes/made.txt")
	assertFile(t, root, "notes/moved.txt", created)

	replaced := []byte("replaced\n")
	writeRel(t, root, "notes/moved.txt", replaced)
	_, err = svc.Rename(ctx, "notes/moved.txt", "notes/again.txt", RevisionOf(created))
	requireConflict(t, err)
	assertAbsent(t, root, "notes/again.txt")
	assertFile(t, root, "notes/moved.txt", replaced)

	taken := []byte("taken\n")
	writeRel(t, root, "notes/taken.txt", taken)
	_, err = svc.Rename(ctx, "notes/moved.txt", "notes/taken.txt", RevisionOf(replaced))
	requireRefused(t, err)
	assertFile(t, root, "notes/moved.txt", replaced)
	assertFile(t, root, "notes/taken.txt", taken)

	_, err = svc.Delete(ctx, "notes/moved.txt", RevisionOf(created))
	requireConflict(t, err)
	assertFile(t, root, "notes/moved.txt", replaced)

	deleted, err := svc.Delete(ctx, "notes/moved.txt", RevisionOf(replaced))
	if err != nil {
		t.Fatal(err)
	}
	if !deleted.Applied || deleted.Exists || deleted.Revision != RevisionAbsent || deleted.Path != "notes/moved.txt" || deleted.Content != nil {
		t.Fatalf("delete snapshot exists=%t revision=%s path=%s", deleted.Exists, deleted.Revision, deleted.Path)
	}
	assertAbsent(t, root, "notes/moved.txt")
	assertFile(t, root, "notes/taken.txt", taken)
}

func TestDocumentFS_SymlinksTraversalAndBinaryRefuseMutation(t *testing.T) {
	svc, root := newDocumentFixture(t)
	ctx := t.Context()
	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinelPath := filepath.Join(outside, "sentinel.txt")
	sentinel := []byte(externalSentinel)
	if err := os.WriteFile(sentinelPath, sentinel, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, ref := range []string{"../outside/sentinel.txt", "notes/../../outside/sentinel.txt", "/tmp/sentinel.txt"} {
		snap, err := svc.Set(ctx, ref, RevisionAbsent, []byte("x\n"))
		op := mustOp(t, err)
		if op.Code != CodeInvalidPath || op.Applied || snap.Applied {
			t.Fatalf("traversal %s code=%s applied=%t", ref, op.Code, op.Applied)
		}
	}
	if got, err := os.ReadFile(sentinelPath); err != nil || !bytes.Equal(got, sentinel) {
		t.Fatalf("sentinel after traversal = %q %v", got, err)
	}

	blob := []byte{0x00, 0x01, 0xff}
	writeRel(t, root, "data/blob.bin", blob)
	rev := RevisionOf(blob)
	_, err := svc.Set(ctx, "data/blob.bin", rev, []byte("text\n"))
	requireRefused(t, err)
	_, err = svc.Patch(ctx, "data/blob.bin", rev, []byte(`{"changes":[]}`))
	requireRefused(t, err)
	_, err = svc.Rename(ctx, "data/blob.bin", "data/other.bin", rev)
	requireRefused(t, err)
	_, err = svc.Delete(ctx, "data/blob.bin", rev)
	requireRefused(t, err)
	assertFile(t, root, "data/blob.bin", blob)
	assertAbsent(t, root, "data/other.bin")

	t.Run("symlinks", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink mutation checks need a Unix symlink")
		}
		if err := os.Symlink(sentinelPath, campFile(root, "links/jump")); err != nil {
			if err := os.Mkdir(campFile(root, "links"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(sentinelPath, campFile(root, "links/jump")); err != nil {
				t.Fatal(err)
			}
		}
		for _, call := range []func() error{
			func() error {
				_, err := svc.Set(ctx, "links/jump", RevisionOf(sentinel), []byte("changed\n"))
				return err
			},
			func() error {
				_, err := svc.Patch(ctx, "links/jump", RevisionOf(sentinel), []byte(`{"changes":[]}`))
				return err
			},
			func() error { _, err := svc.Create(ctx, "links/jump", RevisionAbsent, []byte("changed\n")); return err },
			func() error { _, err := svc.Rename(ctx, "links/jump", "links/moved", RevisionAbsent); return err },
			func() error { _, err := svc.Delete(ctx, "links/jump", RevisionOf(sentinel)); return err },
		} {
			requireRefused(t, call())
		}
		assertAbsent(t, root, "links/moved")
		if target, err := os.Readlink(campFile(root, "links/jump")); err != nil || target != sentinelPath {
			t.Fatalf("file symlink target = %q %v", target, err)
		}
		if info, err := os.Lstat(campFile(root, "links/jump")); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatal("file symlink was replaced")
		}

		if err := os.Symlink(outside, campFile(root, "alias")); err != nil {
			t.Fatal(err)
		}
		for _, call := range []func() error{
			func() error {
				_, err := svc.Set(ctx, "alias/sentinel.txt", RevisionOf(sentinel), []byte("changed\n"))
				return err
			},
			func() error {
				_, err := svc.Create(ctx, "alias/planted.txt", RevisionAbsent, []byte("planted\n"))
				return err
			},
			func() error { _, err := svc.Delete(ctx, "alias/sentinel.txt", RevisionOf(sentinel)); return err },
		} {
			requireRefused(t, call())
		}
		if target, err := os.Readlink(campFile(root, "alias")); err != nil || target != outside {
			t.Fatalf("parent symlink target = %q %v", target, err)
		}
		if _, err := os.Lstat(filepath.Join(outside, "planted.txt")); !os.IsNotExist(err) {
			t.Fatal("parent symlink mutation planted a file outside the camp")
		}
		if got, err := os.ReadFile(sentinelPath); err != nil || !bytes.Equal(got, sentinel) {
			t.Fatalf("sentinel after symlink mutation = %q %v", got, err)
		}
	})
}

func TestDocumentFS_MalformedJumpsStayRepairable(t *testing.T) {
	svc, root := newDocumentFixture(t)
	ctx := t.Context()
	broken := []byte(":\n  -\n")
	writeRel(t, root, "settings/jumps.yaml", broken)
	snap, err := svc.Get(ctx, "settings/jumps.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Applied || snap.Content == nil || *snap.Content != string(broken) || !HasErrors(snap.Diagnostics) {
		t.Fatalf("broken jumps applied=%t diags=%+v", snap.Applied, snap.Diagnostics)
	}
	repaired := []byte("# repaired\npaths:\n  projects: projects\n")
	fixed, err := svc.Set(ctx, "settings/jumps.yaml", RevisionOf(broken), repaired)
	if err != nil {
		t.Fatal(err)
	}
	assertAppliedText(t, fixed, "settings/jumps.yaml", repaired)
	assertFile(t, root, "settings/jumps.yaml", repaired)
	got, err := svc.Get(ctx, "settings/jumps.yaml")
	if err != nil || HasErrors(got.Diagnostics) {
		t.Fatalf("repaired jumps err=%v diags=%+v", err, got.Diagnostics)
	}

	bad := []byte("# keep-comment\npaths:\n  projects: ../outside\ncustom_unknown: stay\nitems:\n  - b\n  - a\n")
	writeRel(t, root, "settings/jumps.yaml", bad)
	current, err := svc.Get(ctx, "settings/jumps.yaml")
	if err != nil || current.Content == nil || *current.Content != string(bad) || !HasErrors(current.Diagnostics) {
		t.Fatalf("invalid jumps err=%v diags=%+v", err, current.Diagnostics)
	}
	patched, err := svc.Patch(ctx, "settings/jumps.yaml", RevisionOf(bad), []byte(`{"changes":[{"op":"set","path":["paths","projects"],"value":"projects"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !patched.Applied {
		t.Fatal("repair patch was not applied")
	}
	text := string(readCamp(t, root, "settings/jumps.yaml"))
	for _, want := range []string{"# keep-comment", "custom_unknown", "projects"} {
		if !strings.Contains(text, want) {
			t.Fatalf("repaired jumps missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "../outside") {
		t.Fatalf("traversal path remained:\n%s", text)
	}
	if b, a := strings.Index(text, "- b"), strings.Index(text, "- a"); b < 0 || a < 0 || b > a {
		t.Fatalf("list order lost:\n%s", text)
	}
	after, err := svc.Get(ctx, "settings/jumps.yaml")
	if err != nil || HasErrors(after.Diagnostics) {
		t.Fatalf("jumps after repair err=%v diags=%+v", err, after.Diagnostics)
	}
}

func TestDocumentFS_ManifestIdentityRefusesWithoutScaffolding(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		root := t.TempDir()
		camp := filepath.Join(root, ".campaign")
		if err := os.Mkdir(camp, 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := NewService(t.Context(), root, fixtureCampID)
		op := mustOp(t, err)
		if op.Code != CodeIdentityMismatch || op.Applied {
			t.Fatalf("missing manifest = %+v", op)
		}
		if names := dirNames(t, camp); len(names) != 0 {
			t.Fatalf("missing manifest scaffolded %v", names)
		}
		if names := dirNames(t, root); !slices.Equal(names, []string{".campaign"}) {
			t.Fatalf("root entries = %v", names)
		}
	})

	t.Run("mismatch", func(t *testing.T) {
		root := t.TempDir()
		body := []byte("id: other\nname: fixture\ntype: personal\nmission: Build useful tools\n")
		writeRel(t, root, "campaign.yaml", body)
		_, err := NewService(t.Context(), root, fixtureCampID)
		op := mustOp(t, err)
		if op.Code != CodeIdentityMismatch || op.Applied {
			t.Fatalf("mismatched manifest = %+v", op)
		}
		assertFile(t, root, "campaign.yaml", body)
		if names := dirNames(t, filepath.Join(root, ".campaign")); !slices.Equal(names, []string{"campaign.yaml"}) {
			t.Fatalf("mismatch scaffolded %v", names)
		}
	})
}

func TestDocumentFS_ReadAndCatalogAreSideEffectFree(t *testing.T) {
	svc, root := newDocumentFixture(t)
	ctx := t.Context()
	writeRel(t, root, "notes/plain.txt", []byte("plain\n"))
	if runtime.GOOS != "windows" {
		if err := os.Symlink("plain.txt", campFile(root, "notes/plain.link")); err != nil {
			t.Fatal(err)
		}
	}
	before := stampCampaign(t, root)

	if _, err := svc.Catalog(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(ctx, "notes/plain.txt")
	if err != nil || got.Applied || got.Content == nil || *got.Content != "plain\n" {
		t.Fatalf("get plain err=%v applied=%t", err, got.Applied)
	}
	missing, err := svc.Get(ctx, "settings/local.json")
	if err != nil || missing.Exists || missing.Revision != RevisionAbsent || missing.Content != nil || missing.Applied {
		t.Fatalf("missing local exists=%t revision=%s err=%v", missing.Exists, missing.Revision, err)
	}
	valid, err := svc.Validate(ctx, "notes/plain.txt", []byte("plain\n"))
	if err != nil || !valid.Valid {
		t.Fatalf("validate plain err=%v valid=%t diags=%+v", err, valid.Valid, valid.Diagnostics)
	}
	invalid, err := svc.Validate(ctx, "settings/fresh.yaml", []byte("completed_runs: nope\n"))
	if err != nil || invalid.Valid || !HasErrors(invalid.Diagnostics) {
		t.Fatalf("validate fresh err=%v valid=%t", err, invalid.Valid)
	}
	accepted, err := svc.Validate(ctx, "settings/fresh.yaml", []byte("completed_runs: off\n"))
	if err != nil || !accepted.Valid {
		t.Fatalf("validate fresh candidate err=%v valid=%t diags=%+v", err, accepted.Valid, accepted.Diagnostics)
	}
	assertStamp(t, root, before)
	assertAbsent(t, root, "settings/fresh.yaml")
	assertAbsent(t, root, "settings/local.json")
}

func TestDocumentFS_CancellationAndOwnerLockContention(t *testing.T) {
	svc, root := newDocumentFixture(t)
	manifest := readCamp(t, root, "campaign.yaml")
	before := stampCampaign(t, root)
	dead, cancel := context.WithCancel(t.Context())
	cancel()
	calls := []struct {
		name string
		call func() error
	}{
		{name: "new", call: func() error { _, err := NewService(dead, root, fixtureCampID); return err }},
		{name: "catalog", call: func() error { _, err := svc.Catalog(dead); return err }},
		{name: "get", call: func() error { _, err := svc.Get(dead, "campaign.yaml"); return err }},
		{name: "validate", call: func() error { _, err := svc.Validate(dead, "campaign.yaml", manifest); return err }},
		{name: "set", call: func() error {
			_, err := svc.Set(dead, "campaign.yaml", RevisionOf(manifest), []byte("cancelled\n"))
			return err
		}},
		{name: "create", call: func() error {
			_, err := svc.Create(dead, "notes/nope.txt", RevisionAbsent, []byte("x\n"))
			return err
		}},
		{name: "patch", call: func() error {
			_, err := svc.Patch(dead, "campaign.yaml", RevisionOf(manifest), []byte(`{"changes":[]}`))
			return err
		}},
		{name: "rename", call: func() error {
			_, err := svc.Rename(dead, "campaign.yaml", "notes/manifest.yaml", RevisionOf(manifest))
			return err
		}},
		{name: "delete", call: func() error { _, err := svc.Delete(dead, "campaign.yaml", RevisionOf(manifest)); return err }},
	}
	for _, call := range calls {
		op := mustOp(t, call.call())
		if op.Code != CodeCancelled || op.Applied {
			t.Fatalf("%s cancellation = %+v", call.name, op)
		}
		assertStamp(t, root, before)
	}

	seed := []byte("seed\n")
	writeRel(t, root, "notes/race.txt", seed)
	rev := RevisionOf(seed)
	const callers = 8
	payloads := make([][]byte, callers)
	snaps := make([]Snapshot, callers)
	errs := make([]error, callers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range callers {
		payloads[i] = []byte(fmt.Sprintf("writer-%d\n", i))
		wg.Go(func() {
			<-start
			snaps[i], errs[i] = svc.Set(t.Context(), "notes/race.txt", rev, payloads[i])
		})
	}
	close(start)
	wg.Wait()
	wins := 0
	winner := -1
	for i := range callers {
		if errs[i] != nil {
			requireConflict(t, errs[i])
			continue
		}
		wins++
		winner = i
		if !snaps[i].Applied || snaps[i].Revision != RevisionOf(payloads[i]) {
			t.Fatalf("winner snapshot applied=%t revision=%s", snaps[i].Applied, snaps[i].Revision)
		}
	}
	if wins != 1 || winner < 0 {
		t.Fatalf("concurrent sets produced %d winners", wins)
	}
	assertFile(t, root, "notes/race.txt", payloads[winner])
	assertFile(t, root, "campaign.yaml", manifest)
	writeRel(t, root, "notes/race.txt.lock", []byte("unrelated authored bytes\n"))
	lockedBefore := stampCampaign(t, root)
	_, err := svc.Set(t.Context(), "notes/race.txt", RevisionOf(payloads[winner]), []byte("refused\n"))
	if err == nil {
		t.Fatal("nonempty lock file was accepted")
	}
	assertStamp(t, root, lockedBefore)
}

func TestDocumentFS_BoundsDepthAndInvalidUTF8Suffix(t *testing.T) {
	svc, root := newDocumentFixture(t)
	ctx := t.Context()
	writeRel(t, root, "top.txt", []byte("top\n"))
	writeRel(t, root, "deep/mid.txt", []byte("mid\n"))
	writeRel(t, root, "deep/nested/file.txt", []byte("file\n"))
	svc.Limits.MaxDepth = 1
	cat, err := svc.Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !cat.Truncated || !hasWarning(cat.Diagnostics) {
		t.Fatalf("depth truncated=%t diags=%+v", cat.Truncated, cat.Diagnostics)
	}
	byPath := indexCatalog(t, cat)
	if _, ok := byPath["top.txt"]; !ok {
		t.Fatal("depth limit dropped a top-level file")
	}
	if _, ok := byPath["deep"]; !ok {
		t.Fatal("depth limit dropped the directory entry")
	}
	if _, ok := byPath["deep/mid.txt"]; ok {
		t.Fatal("catalog walked past the depth limit")
	}
	if _, ok := byPath["deep/nested/file.txt"]; ok {
		t.Fatal("catalog walked a nested directory past the depth limit")
	}

	svc.Limits = DefaultLimits()
	svc.Limits.MaxEntries = len(KnownPaths()) + 1
	writeRel(t, root, "aaa.txt", []byte("a\n"))
	writeRel(t, root, "bbb.txt", []byte("b\n"))
	writeRel(t, root, "ccc.txt", []byte("c\n"))
	limited, err := svc.Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !limited.Truncated || !hasWarning(limited.Diagnostics) {
		t.Fatalf("entry truncated=%t diags=%+v", limited.Truncated, limited.Diagnostics)
	}
	if len(limited.Documents) > limited.Limits.MaxEntries {
		t.Fatalf("catalog has %d documents, limit %d", len(limited.Documents), limited.Limits.MaxEntries)
	}
	limitedPaths := indexCatalog(t, limited)
	for _, rel := range KnownPaths() {
		if _, ok := limitedPaths[rel]; !ok {
			t.Fatalf("entry limit dropped known path %s", rel)
		}
	}

	svc.Limits = DefaultLimits()
	body := append([]byte("prefix-ok\n"), 0xc2)
	writeRel(t, root, "notes/cut.txt", body)
	cut, err := svc.Get(ctx, "notes/cut.txt")
	if err != nil {
		t.Fatal(err)
	}
	if cut.Content != nil || cut.Revision != RevisionOf(body) || cut.Format != DocumentBinary || cut.Capabilities.SourceEdit || !HasErrors(cut.Diagnostics) {
		t.Fatalf("utf8 suffix format=%s edit=%t revision=%s content-nil=%t diags=%+v", cut.Format, cut.Capabilities.SourceEdit, cut.Revision, cut.Content == nil, cut.Diagnostics)
	}
	for _, diag := range cut.Diagnostics {
		if diag.Severity == SeverityError && strings.Contains(diag.Message, "UTF-8") {
			assertFile(t, root, "notes/cut.txt", body)
			return
		}
	}
	t.Fatalf("utf8 suffix diagnostics = %+v", cut.Diagnostics)
}

func TestDocumentFS_TwoConcurrentSetsOneConflict(t *testing.T) {
	svc, root := newDocumentFixture(t)
	seed := []byte("seed\n")
	writeRel(t, root, "notes/pair.txt", seed)
	rev := RevisionOf(seed)
	payloads := [][]byte{[]byte("left\n"), []byte("right\n")}
	snaps := make([]Snapshot, 2)
	errs := make([]error, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range payloads {
		wg.Go(func() {
			<-start
			snaps[i], errs[i] = svc.Set(t.Context(), "notes/pair.txt", rev, payloads[i])
		})
	}
	close(start)
	wg.Wait()
	wins := 0
	winner := -1
	for i := range payloads {
		if errs[i] != nil {
			requireConflict(t, errs[i])
			continue
		}
		wins++
		winner = i
		if !snaps[i].Applied || snaps[i].Content == nil || *snaps[i].Content != string(payloads[i]) || snaps[i].Revision != RevisionOf(payloads[i]) {
			t.Fatalf("winner %d snapshot applied=%t revision=%s", i, snaps[i].Applied, snaps[i].Revision)
		}
	}
	if wins != 1 || winner < 0 {
		t.Fatalf("two sets produced %d successes", wins)
	}
	assertFile(t, root, "notes/pair.txt", payloads[winner])
}

func TestDocumentFS_ExecutableScriptModeSurvivesSetAndPatch(t *testing.T) {
	svc, root := newDocumentFixture(t)
	ctx := t.Context()
	original := []byte("# keep\nname: tool\n")
	writeRel(t, root, "custom/run.sh", original)
	path := campFile(root, "custom/run.sh")
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	updated := []byte("# keep\nname: next\n")
	if _, err := svc.Set(ctx, "custom/run.sh", RevisionOf(original), updated); err != nil {
		t.Fatal(err)
	}
	assertPerm(t, path, 0o755)
	assertFile(t, root, "custom/run.sh", updated)
	writeRel(t, root, "custom/tool.yaml", updated)
	path = campFile(root, "custom/tool.yaml")
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Patch(ctx, "custom/tool.yaml", RevisionOf(updated), []byte(`{"changes":[{"op":"set","path":["name"],"value":"patched"}]}`)); err != nil {
		t.Fatal(err)
	}
	assertPerm(t, path, 0o755)
	text := string(readCamp(t, root, "custom/tool.yaml"))
	if !strings.Contains(text, "# keep") || !strings.Contains(text, "patched") {
		t.Fatalf("patched script =\n%s", text)
	}
}

func TestDocumentFS_FailedCreateLeavesTreeUnchanged(t *testing.T) {
	svc, root := newDocumentFixture(t)
	ctx := t.Context()
	before := stampCampaign(t, root)

	nested, err := svc.Create(ctx, "missing/parent/child.yaml", RevisionAbsent, []byte("invalid: [\n"))
	assertUnapplied(t, nested, err)
	assertStamp(t, root, before)

	base, stop := context.WithCancel(ctx)
	defer stop()
	afterParents := cancelAfterPublication{Context: base, cancel: stop, path: campFile(root, "missing/parent/child.txt.lock"), content: []byte{}}
	nested, err = svc.Create(afterParents, "missing/parent/child.txt", RevisionAbsent, []byte("valid\n"))
	assertUnapplied(t, nested, err)
	assertStamp(t, root, before)

	_, err = svc.Create(ctx, "notes/other.txt", RevisionOf([]byte("seed\n")), []byte("x\n"))
	requireConflict(t, err)
	assertStamp(t, root, before)

	dead, cancel := context.WithCancel(ctx)
	cancel()
	_, err = svc.Create(dead, "notes/later.txt", RevisionAbsent, []byte("x\n"))
	op := mustOp(t, err)
	if op.Code != CodeCancelled || op.Applied {
		t.Fatalf("cancelled create = %+v", op)
	}
	assertStamp(t, root, before)
}

func TestDocumentFS_CreateNestedAuthoredFileAndMissingConfig(t *testing.T) {
	svc, root := newDocumentFixture(t)
	source := []byte("#!/bin/sh\necho authored\n")
	snap, err := svc.Create(t.Context(), "skills/new/scripts/run.sh", RevisionAbsent, source)
	if err != nil || !snap.Applied {
		t.Fatalf("nested create = %#v, %v", snap, err)
	}
	assertFile(t, root, "skills/new/scripts/run.sh", source)
	snap, err = svc.Create(t.Context(), "leverage/config.json", RevisionAbsent, []byte(`{}`))
	if err != nil || !snap.Applied || !snap.Capabilities.Form {
		t.Fatalf("missing config create = %#v, %v", snap, err)
	}
}

func TestDocumentFS_InvalidOwnerActionDoesNotScaffold(t *testing.T) {
	svc, root := newDocumentFixture(t)
	ctx := t.Context()
	before := stampCampaign(t, root)

	unsupported, err := svc.Action(ctx, "settings/pins.json", "not-an-action", RevisionAbsent, []byte(`{}`))
	assertUnapplied(t, unsupported, err)
	assertStamp(t, root, before)

	invalid, err := svc.Action(ctx, "missing/parent/pins.json", "pin", RevisionAbsent, []byte(`{"name":`))
	assertUnapplied(t, invalid, err)
	assertStamp(t, root, before)

	rejected, err := svc.Action(ctx, "settings/workitems.json", "workitem-priority", RevisionAbsent, []byte(`{"selector":"x","priority":"urgent"}`))
	assertUnapplied(t, rejected, err)
	assertStamp(t, root, before)
}

func TestDocumentFS_ConcurrentParentSwapNeverReadsExternalSentinel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("parent symlink swap is not portable on Windows")
	}
	svc, root := newDocumentFixture(t)
	ctx := t.Context()
	manifest := readCamp(t, root, "campaign.yaml")
	external := filepath.Join(root, "outside")
	if err := os.Mkdir(external, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := []byte(externalSentinel)
	sentinelPath := filepath.Join(external, "sentinel.txt")
	externalDoc := filepath.Join(external, "doc.txt")
	if err := os.WriteFile(sentinelPath, sentinel, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(externalDoc, sentinel, 0o644); err != nil {
		t.Fatal(err)
	}
	inside := []byte("inside-document\n")
	writeRel(t, root, "nest/doc.txt", inside)
	nest := campFile(root, "nest")
	hold := nest + ".hold"
	docPath := campFile(root, "nest/doc.txt")
	docHold := docPath + ".hold"

	stop := make(chan struct{})
	var saw atomic.Bool
	var wg sync.WaitGroup
	wg.Go(func() {
		parent := true
		for {
			select {
			case <-stop:
				return
			default:
			}
			if parent {
				if err := os.Rename(nest, hold); err == nil {
					if err := os.Symlink(external, nest); err != nil {
						_ = os.Rename(hold, nest)
					} else if err := os.Remove(nest); err == nil {
						_ = os.Rename(hold, nest)
					}
				}
			} else if info, err := os.Lstat(nest); err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
				if err := os.Rename(docPath, docHold); err == nil {
					if err := os.Symlink(sentinelPath, docPath); err != nil {
						_ = os.Rename(docHold, docPath)
					} else if err := os.Remove(docPath); err == nil {
						_ = os.Rename(docHold, docPath)
					}
				}
			}
			parent = !parent
		}
	})
	for range 2 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				snap, err := svc.Get(ctx, "nest/doc.txt")
				if err == nil && snap.Content != nil && strings.Contains(*snap.Content, "SENTINEL-EXTERNAL") {
					saw.Store(true)
				}
				cat, catErr := svc.Catalog(ctx)
				if catErr == nil {
					for _, doc := range cat.Documents {
						if strings.Contains(doc.Path, "sentinel.txt") {
							saw.Store(true)
						}
					}
				}
			}
		})
	}
	payload := []byte("writer-inside\n")
	for range 2 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				_, _ = svc.Set(ctx, "nest/doc.txt", RevisionOf(inside), payload)
			}
		})
	}
	time.Sleep(250 * time.Millisecond)
	close(stop)
	wg.Wait()
	restoreSwapped(nest, hold, docPath, docHold)

	if saw.Load() {
		t.Fatal("a read observed the external sentinel")
	}
	if got, err := os.ReadFile(sentinelPath); err != nil || !bytes.Equal(got, sentinel) {
		t.Fatalf("external sentinel = %q %v", got, err)
	}
	if got, err := os.ReadFile(externalDoc); err != nil || !bytes.Equal(got, sentinel) {
		t.Fatalf("external document = %q %v", got, err)
	}
	if names := dirNames(t, external); !slices.Equal(names, []string{"doc.txt", "sentinel.txt"}) {
		t.Fatalf("external entries = %v", names)
	}
	assertFile(t, root, "campaign.yaml", manifest)
}

func newDocumentFixture(t *testing.T) (Service, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".campaign"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRel(t, root, "campaign.yaml", manifestSource(fixtureCampID))
	svc, err := NewService(t.Context(), root, fixtureCampID)
	if err != nil {
		t.Fatal(err)
	}
	return svc, root
}

func manifestSource(id string) []byte {
	return []byte("id: " + id + "\nname: fixture\ntype: personal\nmission: Build useful tools\n")
}

func campFile(root, rel string) string {
	return filepath.Join(root, ".campaign", filepath.FromSlash(rel))
}

func writeRel(t *testing.T, root, rel string, body []byte) {
	t.Helper()
	path := campFile(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readCamp(t *testing.T, root, rel string) []byte {
	t.Helper()
	body, err := os.ReadFile(campFile(root, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return body
}

func assertFile(t *testing.T, root, rel string, body []byte) {
	t.Helper()
	if got := readCamp(t, root, rel); !bytes.Equal(got, body) {
		t.Fatalf("%s = %q, want %q", rel, got, body)
	}
}

func assertPerm(t *testing.T, path string, perm os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != perm {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func assertAbsent(t *testing.T, root, rel string) {
	t.Helper()
	if _, err := os.Lstat(campFile(root, rel)); !os.IsNotExist(err) {
		t.Fatalf("%s exists or %v", rel, err)
	}
}

func assertAppliedText(t *testing.T, snap Snapshot, rel string, body []byte) {
	t.Helper()
	if !snap.Applied || !snap.Exists || snap.Path != rel || snap.Revision != RevisionOf(body) || snap.Content == nil || *snap.Content != string(body) {
		got := ""
		if snap.Content != nil {
			got = *snap.Content
		}
		t.Fatalf("snapshot path=%s exists=%t applied=%t revision=%s content=%q", snap.Path, snap.Exists, snap.Applied, snap.Revision, got)
	}
}

func mustOp(t *testing.T, err error) *OpError {
	t.Helper()
	var op *OpError
	if !errors.As(err, &op) || op == nil {
		t.Fatalf("error = %v, want *OpError", err)
	}
	return op
}

func requireConflict(t *testing.T, err error) {
	t.Helper()
	op := mustOp(t, err)
	if op.Code != RevisionConflictCode || op.Applied {
		t.Fatalf("revision conflict = %+v", op)
	}
}

func requireRefused(t *testing.T, err error) {
	t.Helper()
	op := mustOp(t, err)
	if op.Applied {
		t.Fatalf("mutation applied: %+v", op)
	}
}

func assertUnapplied(t *testing.T, snap Snapshot, err error) {
	t.Helper()
	if err != nil {
		if mustOp(t, err).Applied {
			t.Fatal("invalid candidate applied")
		}
		return
	}
	if snap.Applied {
		t.Fatal("invalid candidate applied")
	}
}

func catalogEntry(t *testing.T, docs map[string]DocumentEntry, rel string) DocumentEntry {
	t.Helper()
	doc, ok := docs[rel]
	if !ok {
		t.Fatalf("catalog missing %s", rel)
	}
	return doc
}

func indexCatalog(t *testing.T, cat Catalog) map[string]DocumentEntry {
	t.Helper()
	byPath := make(map[string]DocumentEntry, len(cat.Documents))
	for _, doc := range cat.Documents {
		byPath[doc.Path] = doc
	}
	return byPath
}

func hasWarning(diags []Diagnostic) bool {
	for _, diag := range diags {
		if diag.Severity == SeverityWarning {
			return true
		}
	}
	return false
}

func dirNames(t *testing.T, path string) []string {
	t.Helper()
	ents, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(ents))
	for i, ent := range ents {
		names[i] = ent.Name()
	}
	slices.Sort(names)
	return names
}

type fsNode struct {
	mode os.FileMode
	dir  bool
	link string
	body []byte
}

func stampCampaign(t *testing.T, root string) map[string]fsNode {
	t.Helper()
	base := filepath.Join(root, ".campaign")
	out := map[string]fsNode{}
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		node := fsNode{mode: info.Mode(), dir: info.IsDir()}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			node.link = target
			node.dir = false
		case info.Mode().IsRegular():
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			node.body = bytes.Clone(body)
		}
		out[filepath.ToSlash(rel)] = node
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func assertStamp(t *testing.T, root string, before map[string]fsNode) {
	t.Helper()
	after := stampCampaign(t, root)
	if len(after) != len(before) {
		t.Fatalf("campaign entries = %d, want %d", len(after), len(before))
	}
	for rel, prev := range before {
		next, ok := after[rel]
		if !ok || prev.mode != next.mode || prev.dir != next.dir || prev.link != next.link || !bytes.Equal(prev.body, next.body) {
			t.Fatalf("campaign path %s changed", rel)
		}
	}
}

func restoreSwapped(nest, hold, docPath, docHold string) {
	if info, err := os.Lstat(nest); err == nil && info.Mode()&os.ModeSymlink != 0 {
		_ = os.Remove(nest)
	}
	if _, err := os.Lstat(hold); err == nil {
		if _, statErr := os.Lstat(nest); os.IsNotExist(statErr) {
			_ = os.Rename(hold, nest)
		}
	}
	if info, err := os.Lstat(docPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		_ = os.Remove(docPath)
	}
	if _, err := os.Lstat(docHold); err == nil {
		if _, statErr := os.Lstat(docPath); os.IsNotExist(statErr) {
			_ = os.Rename(docHold, docPath)
		}
	}
}
