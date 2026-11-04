package shell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// These tests execute shell functions entirely in memory. They only navigate
// existing directories; no fixture files or host filesystem mutations occur.
func TestAppFollowPosixShells(t *testing.T) {
	for _, name := range []string{"sh", "bash", "zsh"} {
		t.Run(name, func(t *testing.T) {
			binary, err := exec.LookPath(name)
			if err != nil {
				t.Skip(name + " unavailable")
			}
			script, err := Generate(name)
			if err != nil {
				t.Fatal(err)
			}
			for _, test := range []struct{ name, setup, action, want string }{
				{"success", "export FESTIVAL_APP_TERMINAL_BINDING=binding", `csw target; printf 'RESULT:%s:%s:%s' "$?" "$PWD" "$OBEY_APP_TERMINAL_CONTEXT"`, `RESULT:0:/:{"campaignId":"destination"}`},
				{"delayed-ack", "export FESTIVAL_APP_TERMINAL_BINDING=binding; app_delay=1", `csw target; printf 'RESULT:%s:%s:%s' "$?" "$PWD" "$OBEY_APP_TERMINAL_CONTEXT"`, `RESULT:0:/:{"campaignId":"destination"}`},
				{"external", "unset FESTIVAL_APP_TERMINAL_BINDING; app_missing=1; export OBEY_APP_TERMINAL_CONTEXT=original", `csw target; printf 'RESULT:%s:%s' "$?" "$OBEY_APP_TERMINAL_CONTEXT"`, `RESULT:0:original`},
				{"failed-navigation", "export FESTIVAL_APP_TERMINAL_BINDING=binding; export OBEY_APP_TERMINAL_CONTEXT=original", `csw missing; printf 'RESULT:%s:%s' "$?" "$OBEY_APP_TERMINAL_CONTEXT"`, `RESULT:7:original`},
				{"failed-cd", "export FESTIVAL_APP_TERMINAL_BINDING=binding; export OBEY_APP_TERMINAL_CONTEXT=original", `csw badpath; code=$?; test "$code" -ne 0 || exit 9; printf 'RESULT:%s' "$OBEY_APP_TERMINAL_CONTEXT"`, `RESULT:original`},
				{"ui-failure", "export FESTIVAL_APP_TERMINAL_BINDING=binding; export OBEY_APP_TERMINAL_CONTEXT=original; app_failure=1", `csw target; printf 'RESULT:%s:%s' "$?" "$OBEY_APP_TERMINAL_CONTEXT"`, `RESULT:0:`},
				{"missing-plugin", "export FESTIVAL_APP_TERMINAL_BINDING=binding; export OBEY_APP_TERMINAL_CONTEXT=original; app_missing=1", `csw target; printf 'RESULT:%s:%s:%s' "$?" "$PWD" "$OBEY_APP_TERMINAL_CONTEXT"`, `RESULT:0:/:`},
				{"informational", "export FESTIVAL_APP_TERMINAL_BINDING=binding; export OBEY_APP_TERMINAL_CONTEXT=original", `csw target --print; printf 'RESULT:%s' "$OBEY_APP_TERMINAL_CONTEXT"`, `RESULT:original`},
			} {
				t.Run(test.name, func(t *testing.T) {
					stub := `command() {
 if [ "$1" = "-v" ]; then
  if [ "$2" = camp-app ] && [ "${app_missing:-0}" = 1 ]; then return 1; fi
  return 0
 fi
 shift
 case "$1" in
 switch)
  case "$2" in missing) return 7;; badpath) printf '%s\n' 'cd /__camp_missing_follow_directory__';return;; esac
  printf '%s\n' 'cd /';;
 app)
  if [ "${app_missing:-0}" = 1 ]; then echo UNEXPECTED_APP_DISPATCH >&2; return 127; fi
  if [ "${app_delay:-0}" = 1 ]; then
   CAMP_APP_FOLLOW_HELPER=1 "$CAMP_APP_FOLLOW_TEST_BINARY" -test.run=^TestAppFollowDelayedHelper$ -- "$@"
   return $?
  fi
  [ "${app_failure:-0}" = 0 ] || return 8
  printf '%s\n' '{"campaignId":"destination"}';;
 esac
}
`
					cmd := exec.Command(binary)
					cmd.Env = appFollowTestEnv(t)
					cmd.Stdin = strings.NewReader(stub + script + "\n" + test.setup + "\n" + test.action + "\n")
					out, err := cmd.CombinedOutput()
					if err != nil {
						t.Fatalf("%v: %s", err, out)
					}
					checkAppFollowDiagnostic(t, test.name, string(out))
					if !strings.HasSuffix(strings.TrimSpace(string(out)), test.want) {
						t.Fatalf("want %q, got %q", test.want, out)
					}
				})
			}
		})
	}
}

func TestAppFollowFish(t *testing.T) {
	binary, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("fish unavailable")
	}
	script := prepareFishExploreScript(t, generateFish())
	script = strings.ReplaceAll(script, "command -v camp-app", "app_available")
	for _, tc := range []struct{ name, setup, action, want string }{
		{"success", "set -gx FESTIVAL_APP_TERMINAL_BINDING binding", `csw target; printf 'RESULT:%s:%s:%s' $status $PWD $OBEY_APP_TERMINAL_CONTEXT`, `RESULT:0:/:{"campaignId":"destination"}`},
		{"delayed-ack", "set -gx FESTIVAL_APP_TERMINAL_BINDING binding; set -g app_delay 1", `csw target; printf 'RESULT:%s:%s:%s' $status $PWD "$OBEY_APP_TERMINAL_CONTEXT"`, `RESULT:0:/:{"campaignId":"destination"}`},
		{"external", "set -g app_missing 1; set -gx OBEY_APP_TERMINAL_CONTEXT original", `csw target; printf 'RESULT:%s:%s' $status $OBEY_APP_TERMINAL_CONTEXT`, `RESULT:0:original`},
		{"failed", "set -gx FESTIVAL_APP_TERMINAL_BINDING binding; set -gx OBEY_APP_TERMINAL_CONTEXT original", `csw missing; printf 'RESULT:%s:%s' $status $OBEY_APP_TERMINAL_CONTEXT`, `RESULT:7:original`},
		{"ui-failure", "set -gx FESTIVAL_APP_TERMINAL_BINDING binding; set -g app_failure 1", `csw target; printf 'RESULT:%s:%s' $status "$OBEY_APP_TERMINAL_CONTEXT"`, `RESULT:0:`},
		{"missing-plugin", "set -gx FESTIVAL_APP_TERMINAL_BINDING binding; set -gx OBEY_APP_TERMINAL_CONTEXT original; set -g app_missing 1", `csw target; printf 'RESULT:%s:%s:%s' $status $PWD "$OBEY_APP_TERMINAL_CONTEXT"`, `RESULT:0:/:`},
		{"print", "set -gx FESTIVAL_APP_TERMINAL_BINDING binding; set -gx OBEY_APP_TERMINAL_CONTEXT original", `csw target --print; printf 'RESULT:%s' $OBEY_APP_TERMINAL_CONTEXT`, `RESULT:original`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := `function app_available
 not set -q app_missing
end
function camp_stub
 switch $argv[1]
 case switch
  if test "$argv[2]" = missing; return 7; end
  echo 'cd /'
 case app
  if set -q app_missing; echo UNEXPECTED_APP_DISPATCH >&2; return 127; end
  if set -q app_delay
   set -lx CAMP_APP_FOLLOW_HELPER 1
   "$CAMP_APP_FOLLOW_TEST_BINARY" '-test.run=^TestAppFollowDelayedHelper$' -- $argv
   return $status
  end
  if set -q app_failure; return 8; end
  echo '{"campaignId":"destination"}'
 end
end
`
			cmd := exec.Command(binary, "--no-config")
			cmd.Env = appFollowTestEnv(t)
			cmd.Stdin = strings.NewReader(stub + script + "\n" + tc.setup + "\n" + tc.action + "\n")
			out, err := cmd.CombinedOutput()
			checkAppFollowDiagnostic(t, tc.name, string(out))
			if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), tc.want) {
				t.Fatalf("want %q, got %q (%v)", tc.want, out, err)
			}
		})
	}
}

func checkAppFollowDiagnostic(t *testing.T, name, output string) {
	t.Helper()
	switch name {
	case "missing-plugin":
		if !strings.Contains(output, "Install camp-app") || strings.Contains(output, "UNEXPECTED_APP_DISPATCH") || strings.Contains(output, "Run csw again to retry.") {
			t.Fatalf("missing plugin must explain installation without dispatch or retry-only advice: %s", output)
		}
	case "delayed-ack":
		if strings.Contains(output, "camp: terminal switched") {
			t.Fatalf("acknowledged slow transfer must not print a failure: %s", output)
		}
	case "ui-failure":
		if !strings.Contains(output, "Run csw again to retry.") || strings.Contains(output, "Install camp-app") || !strings.Contains(output, "app synchronization was not confirmed") {
			t.Fatalf("available plugin failures must keep retry advice: %s", output)
		}
	case "external":
		if strings.Contains(output, "camp: terminal switched") || strings.Contains(output, "UNEXPECTED_APP_DISPATCH") {
			t.Fatalf("external shells must not attempt app synchronization: %s", output)
		}
	}
}

// The child helper models a slow but successful app acknowledgement using the
// deadline actually passed by the generated shell function. It writes no files.
func TestAppFollowDelayedHelper(t *testing.T) {
	if os.Getenv("CAMP_APP_FOLLOW_HELPER") != "1" {
		return
	}
	var budget time.Duration
	for i, arg := range os.Args {
		if arg == "--timeout" && i+1 < len(os.Args) {
			var err error
			budget, err = time.ParseDuration(os.Args[i+1])
			if err != nil {
				os.Exit(2)
			}
		}
	}
	if budget <= 0 || budget > 5*time.Second {
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	timer := time.NewTimer(1200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, "App response was lost")
		os.Exit(1)
	case <-timer.C:
		fmt.Println(`{"campaignId":"destination"}`)
		os.Exit(0)
	}
}

func appFollowTestEnv(t *testing.T) []string {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return append(exploreShellEnv(""), "CAMP_APP_FOLLOW_TEST_BINARY="+binary)
}
