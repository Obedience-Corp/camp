//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// These fixtures mutate only the integration container, never the host checkout.
func TestLeverageRepositoryViews(t *testing.T) {
	require.True(t, sccAvailable, "scc must be available")
	tc := GetSharedContainer(t)
	dir := "/tmp/leverage-standalone-views"
	tc.Shell(t, fmt.Sprintf(`set -eu
 mkdir -p %[1]s/.repos
 cd %[1]s
 git init -q
 git config user.name Alice
 git config user.email alice@example.test
 printf 'package main\n\nfunc main() {\n println("hello")\n}\n' > main.go
 git add main.go
 git commit -qm initial
 printf 'package dependency\nconst Value = 123\n' > .repos/import.go
 git add .repos
 git -c user.name=Vendor -c user.email=vendor@example.test commit -qm vendor
 printf '.repos/\n' > .sccignore
 printf 'package main\nconst Added = 42\n' > added.go
 printf '\n// uncommitted edit\n' >> main.go
 git status --porcelain=v1 > /tmp/leverage-before
 `, dir))
	run := func(args ...string) map[string]any {
		out, stderr, code, err := tc.RunCampSplitInDir("/tmp", append([]string{"leverage", dir, "--json"}, args...)...)
		require.NoError(t, err)
		require.Equal(t, 0, code, "%s\n%s", out, stderr)
		var obj map[string]any
		require.NoError(t, json.Unmarshal([]byte(out), &obj), out)
		return obj
	}
	summary := run()
	require.Equal(t, "summary", summary["view"])
	scope := summary["report"].(map[string]any)["scope"].(map[string]any)
	require.Equal(t, float64(2), scope["files"])
	report := run("--by-author", "--author-threshold", "0")
	rows := report["contributors"].([]any)
	require.Len(t, rows, 1)
	require.Equal(t, "alice@example.test", rows[0].(map[string]any)["id"])
	buckets := report["summary"].(map[string]any)["buckets"].(map[string]any)
	require.Greater(t, buckets["uncommitted"].(map[string]any)["lines"].(float64), float64(0))
	personal := run("--author", "alice@example.test")
	require.Equal(t, rows[0].(map[string]any)["leverage"], personal["campaign"].(map[string]any)["full_leverage"])
	manual := run("--people", "2")
	require.Equal(t, float64(2), manual["campaign"].(map[string]any)["actual_people"])
	// Outside-camp fallback and --dir use the identical scope.
	for _, args := range [][]string{{"leverage", "--json"}, {"leverage", "--dir", dir, "--json"}} {
		out, stderr, code, err := tc.RunCampSplitInDir(dir, args...)
		require.NoError(t, err)
		require.Equal(t, 0, code, stderr)
		var obj map[string]any
		require.NoError(t, json.Unmarshal([]byte(out), &obj))
		require.Equal(t, scope["manifest_digest"], obj["report"].(map[string]any)["scope"].(map[string]any)["manifest_digest"])
	}
	tc.Shell(t, fmt.Sprintf(`set -eu; cd %s; git status --porcelain=v1 > /tmp/leverage-after; cmp /tmp/leverage-before /tmp/leverage-after; test ! -e .campaign`, dir))
	// Same HEAD, changed ignore rules: no stale cache or denominator.
	tc.Shell(t, fmt.Sprintf(`printf '' > %s/.sccignore`, dir))
	after := run("--by-author", "--author-threshold", "0")
	require.Len(t, after["contributors"].([]any), 2)
	require.NotEqual(t, scope["manifest_digest"], after["projects"].([]any)[0].(map[string]any)["manifest_digest"])
}

func TestLeverageRepositoryPartialAndFlags(t *testing.T) {
	require.True(t, sccAvailable)
	tc := GetSharedContainer(t)
	dir := "/tmp/leverage-shallow-source"
	tc.Shell(t, fmt.Sprintf(`set -eu; mkdir -p %[1]s; cd %[1]s; git init -q; git config user.name Alice; git config user.email alice@example.test; printf 'package main\n' > main.go; git add .; git commit -qm first; printf '// second\n' >> main.go; git commit -qam second; git clone -q --depth 1 file://%[1]s /tmp/leverage-shallow`, dir))
	out, stderr, code, err := tc.RunCampSplitInDir("/tmp", "leverage", "/tmp/leverage-shallow", "--by-author", "--json")
	require.NoError(t, err)
	require.NotZero(t, code, stderr)
	tc.Shell(t, `set +e; /camp leverage /tmp/leverage-shallow --by-author --json >/tmp/partial-report 2>/tmp/partial-stderr; result=$?; test "$result" -eq 2`)
	var report map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &report), out)
	require.Equal(t, "partial", report["status"])
	require.Nil(t, report["contributors"].([]any)[0].(map[string]any)["leverage"])
	for _, args := range [][]string{{"--author-threshold", "0"}, {"--by-author", "--author", "alice"}, {"--by-author", "--author-threshold", "NaN"}, {"--people", "-1"}} {
		_, stderr, code, err = tc.RunCampSplitInDir("/tmp", append([]string{"leverage", dir}, args...)...)
		require.NoError(t, err)
		require.NotZero(t, code)
		require.NotEmpty(t, strings.TrimSpace(stderr))
	}
}

func TestLeverageRepositoryMailmapSmallOwnerAndCampReadOnly(t *testing.T) {
	require.True(t, sccAvailable)
	tc := GetSharedContainer(t)
	dir := "/tmp/leverage-mailmap"
	tc.Shell(t, fmt.Sprintf(`set -eu
 mkdir -p %[1]s; cd %[1]s; git init -q; git config user.name Alice; git config user.email old@example.test
 printf 'package main\n' > main.go
 i=0; while test "$i" -lt 300; do printf '// line %%s\n' "$i" >> main.go; i=$((i+1)); done
 git add main.go; git commit -qm first
 printf 'package small\n' > small.go; git add small.go; git -c user.name=Bob -c user.email=bob@example.test commit -qm small
 printf 'Alice <alice@example.test> <old@example.test>\n' > .mailmap
 `, dir))
	out, stderr, code, err := tc.RunCampSplitInDir("/tmp", "leverage", dir, "--by-author", "--author-threshold", "0", "--json")
	require.NoError(t, err)
	require.Zero(t, code, stderr)
	var report map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &report))
	rows := report["contributors"].([]any)
	require.Len(t, rows, 2)
	ids := []string{}
	for _, row := range rows {
		ids = append(ids, row.(map[string]any)["id"].(string))
	}
	require.Contains(t, ids, "alice@example.test")
	require.Contains(t, ids, "bob@example.test")
	out, stderr, code, err = tc.RunCampSplitInDir("/tmp", "leverage", dir, "--author", "bob@example.test", "--json")
	require.NoError(t, err)
	require.Zero(t, code, stderr)
	var personal map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &personal))
	require.Greater(t, personal["campaign"].(map[string]any)["full_leverage"].(float64), float64(0))
	// The camp breakdown shares this path without creating leverage state.
	root := setupLeverageCampaign(t, tc, "leverage-report-readonly")
	out, stderr, code, err = tc.RunCampSplitInDir(root, "leverage", "--by-author", "--json")
	require.NoError(t, err)
	require.Zero(t, code, stderr)
	require.NoError(t, json.Unmarshal([]byte(out), &report))
	require.Len(t, report["projects"].([]any), 2)
	tc.Shell(t, fmt.Sprintf(`test ! -e %s/.campaign/leverage`, root))
}
