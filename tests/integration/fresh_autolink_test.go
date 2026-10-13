//go:build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// autoLinkFixture runs camp with its own home, registry, and camps directory,
// so each test controls exactly which camps are registered. The pooled
// container's ordinary registry is never read or written.
type autoLinkFixture struct {
	tc   *TestContainer
	name string
	home string
	env  string
}

func newAutoLinkFixture(t *testing.T, name string) autoLinkFixture {
	t.Helper()
	tc := GetSharedContainer(t)
	require.True(t, festAvailable, "auto-link tests create the starter camp, which needs the real fest binary")
	home := "/tmp/autolink-" + name
	tc.Shell(t, fmt.Sprintf(`rm -rf %[1]s /test/autolink-%[2]s
mkdir -p %[1]s/config/obey/campaign /test/autolink-%[2]s
printf '%%s' '{"campaigns_dir":"%[1]s/camps"}' > %[1]s/config/obey/campaign/config.json`, home, name))
	env := fmt.Sprintf("env -u CAMP_ROOT -u CAMP_REGISTRY_PATH HOME=%[1]s XDG_CONFIG_HOME=%[1]s/config "+
		"GIT_AUTHOR_NAME=Test GIT_AUTHOR_EMAIL=test@test.com GIT_COMMITTER_NAME=Test GIT_COMMITTER_EMAIL=test@test.com", home)
	return autoLinkFixture{tc: tc, name: name, home: home, env: env}
}

// camp runs the binary from dir and returns combined output and the exit code.
func (f autoLinkFixture) camp(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()
	out, code, err := f.tc.ExecCommand("sh", "-c",
		fmt.Sprintf("cd %s && %s /camp %s --no-color 2>&1", dir, f.env, strings.Join(args, " ")))
	require.NoError(t, err)
	return out, code
}

func (f autoLinkFixture) starterCamp() string { return f.home + "/camps/festival" }

// repo creates a clone left on feat/b with a stale main. Upstream, feat/a was
// merged with a merge commit and feat/b was squash-merged, and both remote
// branches were deleted: the state a developer is in after two PRs land.
func (f autoLinkFixture) repo(t *testing.T, parent, dirName string) string {
	t.Helper()
	base := fmt.Sprintf("/test/autolink-%s/%s", f.name, parent)
	dir := base + "/" + dirName
	f.tc.Shell(t, fmt.Sprintf(`set -e
export GIT_AUTHOR_NAME=Test GIT_AUTHOR_EMAIL=test@test.com GIT_COMMITTER_NAME=Test GIT_COMMITTER_EMAIL=test@test.com
mkdir -p %[1]s
git init -q --bare %[1]s/origin.git
git clone -q %[1]s/origin.git %[2]s 2>/dev/null
cd %[2]s
git checkout -q -b main
echo one > a.txt && git add a.txt && git commit -q -m init && git push -q -u origin main
git --git-dir %[1]s/origin.git symbolic-ref HEAD refs/heads/main
git checkout -q -b feat/a
echo a > fa.txt && git add fa.txt && git commit -q -m 'feat a' && git push -q -u origin feat/a
git checkout -q main && git checkout -q -b feat/b
echo b > fb.txt && git add fb.txt && git commit -q -m 'feat b 1'
echo b2 >> fb.txt && git commit -q -am 'feat b 2' && git push -q -u origin feat/b
git clone -q %[1]s/origin.git %[1]s/server
cd %[1]s/server
git merge -q --no-ff origin/feat/a -m 'Merge feat/a'
git merge -q --squash origin/feat/b && git commit -q -m 'feat b (squashed)'
git push -q origin main && git push -q origin --delete feat/a feat/b
rm -rf %[1]s/server`, base, dir))
	return dir
}

func (f autoLinkFixture) campCount(t *testing.T) int {
	t.Helper()
	out, code := f.camp(t, "/tmp", "list", "--format", "json")
	require.Equal(t, 0, code, out)
	return strings.Count(out, `"id":`)
}

func (f autoLinkFixture) projectNames(t *testing.T, campRoot string) []string {
	t.Helper()
	out := f.tc.Shell(t, fmt.Sprintf("find %s/projects -maxdepth 1 -type l -exec basename {} \\; | sort", campRoot))
	return strings.Fields(out)
}

func (f autoLinkFixture) assertFreshened(t *testing.T, repo string) {
	t.Helper()
	f.tc.Shell(t, fmt.Sprintf(`set -e
cd %s
test -z "$(git status --porcelain)"
test "$(git rev-parse --abbrev-ref HEAD)" = main
test "$(git rev-parse main)" = "$(git rev-parse origin/main)"
if git rev-parse --verify --quiet refs/heads/feat/a >/dev/null; then echo "feat/a still exists"; exit 1; fi
if git rev-parse --verify --quiet refs/heads/feat/b >/dev/null; then echo "feat/b still exists"; exit 1; fi
grep -qx .camp .git/info/exclude`, repo))
}

func (f autoLinkFixture) assertUntouched(t *testing.T, repo string) {
	t.Helper()
	f.tc.Shell(t, fmt.Sprintf(`set -e
cd %s
test ! -e .camp
test "$(git rev-parse --abbrev-ref HEAD)" = feat/b
git rev-parse --verify --quiet refs/heads/feat/a >/dev/null`, repo))
}

func TestFreshAutoLink_FirstRunCreatesStarterLinksAndFreshens(t *testing.T) {
	f := newAutoLinkFixture(t, "first")
	repo := f.repo(t, "w", "myapp")
	require.Equal(t, 0, f.campCount(t))

	out, code := f.camp(t, repo, "fresh")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "Linked myapp into your festival camp, created at "+f.starterCamp())
	assert.Contains(t, out, "camp project unlink")
	assert.Contains(t, out, "Fresh!")
	assert.Equal(t, 1, strings.Count(out, "camp project unlink"), "the link notice is one line:\n%s", out)

	assert.Equal(t, 1, f.campCount(t))
	assert.Equal(t, []string{"myapp"}, f.projectNames(t, f.starterCamp()))
	f.assertFreshened(t, repo)
	log := f.tc.GitOutput(t, f.starterCamp(), "log", "-1", "--pretty=%s")
	assert.Contains(t, log, "Link: myapp", "the link is recorded in the camp's history")

	// A second run is an ordinary linked-project run: no link, no notice.
	out, code = f.camp(t, repo, "fresh")
	require.Equal(t, 0, code, out)
	assert.NotContains(t, out, "camp project unlink")
	assert.NotContains(t, out, "Linked")
	assert.Equal(t, []string{"myapp"}, f.projectNames(t, f.starterCamp()))

	// The notice's undo is real: unlink leaves the repository as it was found.
	out, code = f.camp(t, repo, "project", "unlink")
	require.Equal(t, 0, code, out)
	f.tc.Shell(t, fmt.Sprintf(`test ! -e %[1]s/.camp
if grep -qx .camp %[1]s/.git/info/exclude; then echo ".camp still excluded"; exit 1; fi`, repo))
	assert.Empty(t, f.projectNames(t, f.starterCamp()))
}

func TestFreshAutoLink_DryRunLinksNothing(t *testing.T) {
	f := newAutoLinkFixture(t, "dryrun")
	repo := f.repo(t, "w", "myapp")

	out, code := f.camp(t, repo, "fresh", "--dry-run")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "Would link myapp into your festival camp, creating the camp at "+f.starterCamp())
	assert.Contains(t, out, "Would checkout main", "the preview still prints the plan")
	assert.NotContains(t, out, "camp project unlink")

	assert.Equal(t, 0, f.campCount(t), "a preview never creates a camp")
	f.tc.Shell(t, "test ! -e "+f.starterCamp())
	f.assertUntouched(t, repo)
}

func TestFreshAutoLink_SameFolderNameGetsDistinctName(t *testing.T) {
	f := newAutoLinkFixture(t, "collide")
	first := f.repo(t, "one", "myapp")
	second := f.repo(t, "two", "myapp")

	out, code := f.camp(t, first, "fresh")
	require.Equal(t, 0, code, out)

	out, code = f.camp(t, second, "fresh", "--dry-run")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "Would link myapp as myapp-2 into the festival camp at "+f.starterCamp())

	out, code = f.camp(t, second, "fresh")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "Linked myapp as myapp-2 into the festival camp at "+f.starterCamp())
	assert.NotContains(t, out, "boundary")
	assert.Equal(t, []string{"myapp", "myapp-2"}, f.projectNames(t, f.starterCamp()))
	f.assertFreshened(t, first)
	f.assertFreshened(t, second)

	// Both projects resolve from their own directories.
	for _, repo := range []string{first, second} {
		out, code = f.camp(t, repo, "fresh", "--dry-run")
		require.Equal(t, 0, code, out)
		assert.NotContains(t, out, "Would link")
	}
}

func TestFreshAutoLink_SecondaryWorktreeUsesMainCheckoutLink(t *testing.T) {
	f := newAutoLinkFixture(t, "worktree")
	repo := f.repo(t, "w", "myapp")
	worktree := "/test/autolink-worktree/w/myapp-wt"

	// Unlinked: the main checkout is what would be linked, under its own name.
	f.tc.Shell(t, fmt.Sprintf("git -C %s worktree add -q %s -b feat/c", repo, worktree))
	out, code := f.camp(t, worktree, "fresh", "--dry-run")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "Would link myapp into your festival camp")
	assert.NotContains(t, out, "myapp-wt")
	f.tc.Shell(t, fmt.Sprintf("test ! -e %s/.camp && test ! -e %s/.camp", worktree, repo))

	// A real run from the worktree links the main checkout, never the worktree.
	f.tc.Shell(t, fmt.Sprintf("git -C %s worktree remove --force %s && git -C %s branch -D feat/c", repo, worktree, repo))
	out, code = f.camp(t, repo, "fresh")
	require.Equal(t, 0, code, out)
	f.tc.Shell(t, fmt.Sprintf("git -C %s worktree add -q %s -b feat/c", repo, worktree))

	// Linked: the worktree uses the main checkout's link and says nothing.
	out, code = f.camp(t, worktree, "fresh", "--dry-run")
	require.Equal(t, 0, code, out)
	assert.NotContains(t, out, "Would link")
	assert.NotContains(t, out, "camp project unlink")
	assert.Contains(t, out, "myapp")
	f.tc.Shell(t, "test ! -e "+worktree+"/.camp")
	assert.Equal(t, []string{"myapp"}, f.projectNames(t, f.starterCamp()))
}

func TestFreshAutoLink_ExistingCampsNeedCampaignFlag(t *testing.T) {
	f := newAutoLinkFixture(t, "choose")
	for _, name := range []string{"alpha", "beta"} {
		out, code := f.camp(t, "/tmp", "create", name, "--no-skills", "-d", name, "-m", name)
		require.Equal(t, 0, code, out)
	}
	repo := f.repo(t, "w", "svc")

	out, code := f.camp(t, repo, "fresh")
	require.NotEqual(t, 0, code, out)
	assert.Contains(t, out, `no camp is named "festival"`)
	assert.Contains(t, out, "camp fresh --campaign <name>")
	assert.Contains(t, out, "alpha")
	assert.Contains(t, out, "beta")
	assert.Equal(t, 2, f.campCount(t), "the starter camp is only for users with no camps")
	f.assertUntouched(t, repo)

	out, code = f.camp(t, repo, "fresh", "--campaign", "alpha")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "Linked svc into the alpha camp at "+f.home+"/camps/alpha")
	assert.Equal(t, []string{"svc"}, f.projectNames(t, f.home+"/camps/alpha"))
	assert.Empty(t, f.projectNames(t, f.home+"/camps/beta"))
	f.assertFreshened(t, repo)

	// Once linked, --campaign is ignored: the repository stays where it is.
	out, code = f.camp(t, repo, "fresh", "--dry-run", "--campaign", "beta")
	require.Equal(t, 0, code, out)
	assert.NotContains(t, out, "Would link")
	assert.Empty(t, f.projectNames(t, f.home+"/camps/beta"))
}

func TestFreshAutoLink_OptOutsKeepNotInCampError(t *testing.T) {
	f := newAutoLinkFixture(t, "optout")
	repo := f.repo(t, "w", "myapp")
	f.tc.Shell(t, "mkdir -p /test/autolink-optout/plain")

	cases := []struct {
		name string
		dir  string
		args []string
	}{
		{"--no-link", repo, []string{"fresh", "--no-link"}},
		{"named project", repo, []string{"fresh", "myapp"}},
		{"--project", repo, []string{"fresh", "--project", "myapp"}},
		{"--list", repo, []string{"fresh", "--list", "myapp"}},
		{"fresh all", repo, []string{"fresh", "all"}},
		{"outside a git repository", "/test/autolink-optout/plain", []string{"fresh"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, code := f.camp(t, tc.dir, tc.args...)
			require.NotEqual(t, 0, code, out)
			assert.Contains(t, out, "not in a camp")
			assert.Equal(t, 0, f.campCount(t))
			f.assertUntouched(t, repo)
		})
	}
}

// A run that the cycle would refuse anyway must not leave a camp or a marker
// behind.
func TestFreshAutoLink_RefusedRunLinksNothing(t *testing.T) {
	f := newAutoLinkFixture(t, "refused")
	repo := f.repo(t, "w", "myapp")
	f.tc.Shell(t, "echo dirty >> "+repo+"/a.txt")

	out, code := f.camp(t, repo, "fresh")
	require.NotEqual(t, 0, code, out)
	assert.Contains(t, out, "uncommitted changes")
	assert.Equal(t, 0, f.campCount(t))
	f.tc.Shell(t, "test ! -e "+repo+"/.camp && test ! -e "+f.starterCamp())
}

// Two first runs at the same moment, as when several agents start at once,
// must both succeed and share one starter camp instead of the slower one
// failing because the faster one registered a camp first.
func TestFreshAutoLink_ConcurrentFirstRunsShareTheStarter(t *testing.T) {
	f := newAutoLinkFixture(t, "race")
	first := f.repo(t, "one", "api")
	second := f.repo(t, "two", "web")
	logs := "/test/autolink-race"

	f.tc.Shell(t, fmt.Sprintf(`
(cd %[1]s && %[3]s /camp fresh --no-color > %[4]s/first.log 2>&1; echo $? > %[4]s/first.code) &
(cd %[2]s && %[3]s /camp fresh --no-color > %[4]s/second.log 2>&1; echo $? > %[4]s/second.code) &
wait`, first, second, f.env, logs))

	for _, run := range []string{"first", "second"} {
		code, err := f.tc.ReadFile(logs + "/" + run + ".code")
		require.NoError(t, err)
		log, err := f.tc.ReadFile(logs + "/" + run + ".log")
		require.NoError(t, err)
		require.Equal(t, "0", strings.TrimSpace(code), "%s run failed:\n%s", run, log)
		assert.Contains(t, log, "camp project unlink", "%s run reports its link", run)
	}
	assert.Equal(t, 1, f.campCount(t))
	assert.Equal(t, []string{"api", "web"}, f.projectNames(t, f.starterCamp()))
	f.assertFreshened(t, first)
	f.assertFreshened(t, second)
}
