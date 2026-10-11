package shell

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/creack/pty"
)

func TestDungeonExploreValueFlagsMatchShells(t *testing.T) {
	flags := dungeonExploreValueFlags(t)
	shells := []struct {
		name, script, start, end, skip string
	}{
		{"bash", generateBash(), "    dungeon)", "    festivals)", "skip=1"},
		{"sh", generateSh(), "    dungeon)", "    festivals)", "skip=1"},
		{"zsh", generateZsh(), "    dungeon)", "    festivals)", "skip=1"},
		{"fish", generateFish(), "        case dungeon", "        case festivals", "set skip 1"},
	}
	for _, sh := range shells {
		section := shellWrapperSection(t, sh.script, sh.start, sh.end)
		arm := lineBeforeMarker(t, section, sh.skip)
		for _, flag := range flags {
			if !strings.Contains(arm, "--"+flag) {
				t.Errorf("%s does not consume a separate --%s value (arm %q)", sh.name, flag, strings.TrimSpace(arm))
			}
			if !strings.Contains(section, "--"+flag+"=*") {
				t.Errorf("%s does not accept --%s=value", sh.name, flag)
			}
		}
	}
}

func lineBeforeMarker(t *testing.T, section, marker string) string {
	t.Helper()
	lines := strings.Split(section, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == marker && i > 0 {
			return lines[i-1]
		}
	}
	t.Fatalf("missing %q", marker)
	return ""
}

func dungeonExploreValueFlags(t *testing.T) []string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	path := filepath.Join(filepath.Dir(here), "..", "..", "cmd", "camp", "dungeon", "explore", "explore.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var flags []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		recv, ok := sel.X.(*ast.Ident)
		if !ok || recv.Name != "flags" {
			return true
		}
		switch sel.Sel.Name {
		case "Bool", "BoolP", "BoolVar", "BoolVarP", "MarkHidden":
			return true
		case "String", "StringP":
			name, found := callString(call, 0)
			if !found {
				t.Errorf("flags.%s has no name", sel.Sel.Name)
				return true
			}
			flags = append(flags, name)
		case "StringVar", "StringVarP":
			name, found := callString(call, 1)
			if !found {
				t.Errorf("flags.%s has no name", sel.Sel.Name)
				return true
			}
			flags = append(flags, name)
		default:
			t.Errorf("flags.%s is not classified as bool or value-taking", sel.Sel.Name)
		}
		return true
	})
	seen := map[string]bool{}
	for _, flag := range flags {
		seen[flag] = true
	}
	if !seen["query"] || !seen["path-output"] || seen["json"] {
		t.Fatalf("parsed %v from %s", flags, path)
	}
	return flags
}

func callString(call *ast.CallExpr, n int) (string, bool) {
	if n >= len(call.Args) {
		return "", false
	}
	lit, ok := call.Args[n].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	return value, err == nil
}

func runExplorePassthroughCases(t *testing.T, shellName, script string) {
	t.Helper()
	cases := []struct {
		name    string
		args    []string
		handoff bool
	}{
		{name: "query value is not json", args: []string{"--query", "--json"}, handoff: true},
		{name: "last json false keeps handoff", args: []string{"--json", "--json=false"}, handoff: true},
		{name: "last json true passes through", args: []string{"--json=false", "--json"}, handoff: false},
		{name: "help passes through", args: []string{"--help"}, handoff: false},
		{name: "short help passes through", args: []string{"-h"}, handoff: false},
		{name: "end of options stops json", args: []string{"--", "--json"}, handoff: true},
		{name: "status value is not json", args: []string{"--status", "--json"}, handoff: true},
		{name: "equals query is not json", args: []string{"--query=--json"}, handoff: true},
		{name: "json alone passes through", args: []string{"--json"}, handoff: false},
		{name: "json false keeps handoff", args: []string{"--json=false"}, handoff: true},
		{name: "query value is not short help", args: []string{"--query", "-h"}, handoff: true},
		{name: "dungeon value is not help", args: []string{"--dungeon", "--help"}, handoff: true},
		{name: "help after a value flag passes through", args: []string{"--query", "x", "--help"}, handoff: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			invoked := invokeExploreWrapper(t, shellName, script, append([]string{"dungeon", "explore"}, tc.args...))
			got := strings.Contains(invoked, "--path-output")
			if got != tc.handoff {
				t.Fatalf("handoff=%v, wrapper ran %q", got, invoked)
			}
		})
	}
	runExploreRootFlagCases(t, shellName, script)
}

// A root flag is valid before or between the command words. The wrapper
// matched `dungeon explore` only as the first two words, so
// `camp --no-color dungeon explore` opened the feed without its handoff file
// and g reported that shell integration was missing.
func runExploreRootFlagCases(t *testing.T, shellName, script string) {
	t.Helper()
	const handoff = "dungeon explore --path-output /no/such/camp-explore"
	cases := []struct {
		name string
		line []string
		want string
	}{
		{"root flag first", []string{"--no-color", "dungeon", "explore"}, handoff + " --no-color"},
		{"root flag with a value first", []string{"--no-color=false", "dungeon", "explore"}, handoff + " --no-color=false"},
		{"root flag between the words", []string{"dungeon", "--no-color", "explore"}, handoff + " --no-color"},
		{"root flag before and between", []string{"--no-color", "dungeon", "--no-color=true", "explore", "--query", "a b"}, handoff + " --no-color --no-color=true --query a b"},
		{"root flag first, value flag eats json", []string{"--no-color", "dungeon", "explore", "--query", "--json"}, handoff + " --no-color --query --json"},
		{"root flag first, json passes through", []string{"--no-color", "dungeon", "explore", "--json"}, "dungeon explore --no-color --json"},
		{"root flag first, help passes through", []string{"--no-color", "dungeon", "explore", "-h"}, "dungeon explore --no-color -h"},
		// Everything else is left exactly as typed.
		{"root flag after the words", []string{"dungeon", "explore", "--no-color"}, handoff + " --no-color"},
		{"another dungeon command", []string{"--no-color", "dungeon", "list"}, "--no-color dungeon list"},
		{"dungeon alone", []string{"dungeon", "--no-color"}, "dungeon --no-color"},
		{"a word between the flags and the command", []string{"--no-color", "false", "dungeon", "explore"}, "--no-color false dungeon explore"},
		{"not a root flag", []string{"--no-colors", "dungeon", "explore"}, "--no-colors dungeon explore"},
		{"end of options first", []string{"--", "dungeon", "explore"}, "-- dungeon explore"},
		{"root flag alone", []string{"--no-color"}, "--no-color"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The bash and zsh stubs print the command name; the fish one
			// stands in for it.
			got := strings.TrimPrefix(invokeExploreWrapper(t, shellName, script, tc.line), "camp ")
			if got != tc.want {
				t.Fatalf("camp %s ran camp %q, want camp %q", strings.Join(tc.line, " "), got, tc.want)
			}
		})
	}
}

// The wrappers read past the root flags by name. A flag added to the root
// command, or one that takes a value, has to be added there too.
func TestWrapperRootFlagsMatchTheRootCommand(t *testing.T) {
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	dir := filepath.Join(filepath.Dir(here), "..", "..", "cmd", "camp")
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no root command sources in %s: %v", dir, err)
	}
	var declared []string
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			method, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			set, ok := method.X.(*ast.CallExpr)
			if !ok {
				return true
			}
			owner, ok := set.Fun.(*ast.SelectorExpr)
			if !ok || owner.Sel.Name != "PersistentFlags" {
				return true
			}
			if root, ok := owner.X.(*ast.Ident); !ok || root.Name != "rootCmd" {
				return true
			}
			switch method.Sel.Name {
			case "Bool", "BoolP":
				if name, found := callString(call, 0); found {
					declared = append(declared, name)
				}
			case "BoolVar", "BoolVarP":
				if name, found := callString(call, 1); found {
					declared = append(declared, name)
				}
			case "Lookup", "VisitAll", "Changed", "GetBool":
			default:
				t.Errorf("%s: rootCmd.PersistentFlags().%s is not a boolean flag; the wrappers skip only boolean root flags", filepath.Base(path), method.Sel.Name)
			}
			return true
		})
	}
	slices.Sort(declared)
	want := slices.Clone(rootBoolFlags)
	slices.Sort(want)
	if !slices.Equal(declared, want) {
		t.Fatalf("the root command declares persistent flags %v; the wrappers skip %v", declared, want)
	}
	for _, sh := range []struct{ name, script string }{
		{"bash", generateBash()}, {"sh", generateSh()}, {"zsh", generateZsh()}, {"fish", generateFish()},
	} {
		for _, name := range rootBoolFlags {
			if !strings.Contains(sh.script, "--"+name+"=*") {
				t.Errorf("%s does not read past --%s", sh.name, name)
			}
		}
	}
}

// invokeExploreWrapper runs `camp words...` through the wrapper and returns
// the one command the wrapper ran.
func invokeExploreWrapper(t *testing.T, shellName, script string, words []string) string {
	t.Helper()
	bin, initScript := exploreWrapperCommand(t, shellName, script, words)
	if _, err := exec.LookPath(bin[0]); err != nil {
		t.Skip(bin[0] + " not available")
	}
	cmd := exec.Command(bin[0], bin[1:]...)
	cmd.Env = exploreShellEnv(initScript)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ptmx.Close() }()
	var buf bytes.Buffer
	readDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, ptmx)
		close(readDone)
	}()
	waitErr := cmd.Wait()
	_ = ptmx.Close()
	<-readDone
	out := strings.ReplaceAll(buf.String(), "\r", "")
	if waitErr != nil {
		t.Fatalf("%s: %v\n%s", shellName, waitErr, out)
	}
	var invoked []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "INVOKE "); ok {
			invoked = append(invoked, rest)
		} else if line != "" {
			// zsh prints a variable that one function declares twice.
			t.Errorf("%s %v: the wrapper printed %q", shellName, words, line)
		}
	}
	if len(invoked) != 1 {
		t.Fatalf("%s %v invoked camp %d times:\n%s", shellName, words, len(invoked), out)
	}
	return invoked[0]
}

// exploreShellHome is where a spawned shell is told its home and XDG
// directories are. Nothing can be created under /dev/null, by any user, so a
// shell that writes first-run files (fish creates ~/.config/fish, its cache,
// and its data directory) cannot put them in the developer's home.
const exploreShellHome = "/dev/null/camp-shell-test"

func exploreShellEnv(initScript string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + exploreShellHome,
		"XDG_CONFIG_HOME=" + exploreShellHome + "/config",
		"XDG_CACHE_HOME=" + exploreShellHome + "/cache",
		"XDG_DATA_HOME=" + exploreShellHome + "/data",
		"TERM=xterm-256color",
		"CAMP_INIT=" + initScript,
	}
}

// The wrapper tests run real shells. One handed the developer's HOME wrote
// there: fish created its config, cache, and data directories on first run.
func TestExploreWrapperShellsCannotWriteHome(t *testing.T) {
	seen := map[string]string{}
	for _, entry := range exploreShellEnv("") {
		name, value, _ := strings.Cut(entry, "=")
		seen[name] = value
	}
	for _, name := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME"} {
		value, ok := seen[name]
		if !ok || !strings.HasPrefix(value, "/dev/null/") {
			t.Errorf("%s = %q (set: %v); want a path under /dev/null, where nothing can be created", name, value, ok)
		}
		if home := os.Getenv("HOME"); home != "" && strings.HasPrefix(value, home) {
			t.Errorf("%s = %q is inside the developer's home", name, value)
		}
	}
}

func exploreWrapperCommand(t *testing.T, shellName, script string, args []string) ([]string, string) {
	t.Helper()
	quoted := quoteShellArgs(args)
	switch shellName {
	case "bash":
		return []string{"bash", "--noprofile", "--norc", "-c", bashExploreRunner(quoted)}, script
	case "zsh":
		return []string{"zsh", "-f", "-c", zshExploreRunner(quoted)}, script
	case "fish":
		return []string{"fish", "--no-config", "-c", fishExploreRunner(quoted)}, prepareFishExploreScript(t, script)
	default:
		t.Fatalf("unknown shell %s", shellName)
		return nil, ""
	}
}

func bashExploreRunner(line string) string {
	return `
camp() { :; }
eval "$CAMP_INIT"
command() { printf 'INVOKE %s\n' "$*"; }
enable -n command || exit 1
mktemp() { printf '%s\n' /no/such/camp-explore; }
rm() { return 0; }
cd() { return 0; }
camp ` + line + `
exit 0
`
}

func zshExploreRunner(line string) string {
	return `
camp() { :; }
eval "$CAMP_INIT"
command() { printf 'INVOKE %s\n' "$*"; }
disable command || exit 1
mktemp() { printf '%s\n' /no/such/camp-explore; }
rm() { return 0; }
cd() { return 0; }
camp ` + line + `
exit 0
`
}

func fishExploreRunner(line string) string {
	return `
function camp_stub
    printf 'INVOKE %s\n' "$argv"
end
function mktemp
    echo /no/such/camp-explore
end
function rm
end
function cd
end
eval $CAMP_INIT
camp ` + line + `
exit 0
`
}

func prepareFishExploreScript(t *testing.T, script string) string {
	t.Helper()
	const guard = "if not command -v camp &>/dev/null\n    exit 0\nend\n"
	if !strings.Contains(script, guard) {
		t.Fatal("fish shell-init guard changed")
	}
	script = strings.Replace(script, guard, "", 1)
	return strings.ReplaceAll(script, "command camp", "camp_stub")
}

func quoteShellArgs(args []string) string {
	parts := make([]string, len(args))
	for i, arg := range args {
		parts[i] = "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
	}
	return strings.Join(parts, " ")
}
