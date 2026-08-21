package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestScan_HelperCallerIsAHit(t *testing.T) {
	ctx := context.Background()
	report, err := Scan(ctx, "testdata/helper_caller", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report.New, []string{"./caller_test.go", "./helpers_test.go"}) {
		t.Fatalf("New = %v, want helper definition and helper-only caller", report.New)
	}
	for _, hit := range report.Hits {
		switch hit {
		case "./tagged_test.go", "./pure_test.go", "./tests/integration/int_test.go":
			t.Fatalf("compliant file listed as hit: %s", hit)
		}
	}
}

func TestScan_AllowlistExemptsHelperCaller(t *testing.T) {
	ctx := context.Background()
	report, err := Scan(ctx, "testdata/helper_caller", []string{"./caller_test.go", "./helpers_test.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.New) != 0 {
		t.Fatalf("New = %v, want none when helper files are allowlisted", report.New)
	}
	if len(report.Hits) != 2 {
		t.Fatalf("Hits = %v, want the two helper files", report.Hits)
	}
}

func TestScan_Args0HelperIsDirectGit(t *testing.T) {
	ctx := context.Background()
	report, err := Scan(ctx, "testdata/args0", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report.New, []string{"./wrap_test.go"}) {
		t.Fatalf("New = %v, want args[0] wrapper", report.New)
	}
}

func TestScan_MustPrefixDoesNotMatchRunGit(t *testing.T) {
	ctx := context.Background()
	report, err := Scan(ctx, "testdata/boundary", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report.New, []string{"./helpers_test.go"}) {
		t.Fatalf("New = %v, want only the runGit definition file", report.New)
	}
}

func TestScan_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Scan(ctx, "testdata/helper_caller", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestScan_RepoHasNoNewViolators(t *testing.T) {
	ctx := context.Background()
	root := moduleRoot(t)
	report, err := Scan(ctx, root, defaultAllowlist)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.New) != 0 {
		t.Fatalf("new host-fs violators (detect helper usage): %v", report.New)
	}
	if len(report.Hits) == 0 {
		t.Fatal("expected legacy hits on the allowlist")
	}
	hits := make(map[string]struct{}, len(report.Hits))
	for _, h := range report.Hits {
		hits[h] = struct{}{}
	}
	for _, p := range defaultAllowlist {
		if _, ok := hits[p]; !ok {
			t.Errorf("allowlist entry is not a current hit: %s", p)
		}
	}
}

func TestRun_FailsOnNewViolator(t *testing.T) {
	ctx := context.Background()
	err := run(ctx, "testdata/helper_caller", nil, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected failure for helper-only caller")
	}
}

func TestRun_CleanOnAllowlist(t *testing.T) {
	ctx := context.Background()
	err := run(ctx, "testdata/helper_caller", []string{"./caller_test.go", "./helpers_test.go"}, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
