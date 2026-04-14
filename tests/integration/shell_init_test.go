//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"
)

// ---------- Syntax validation tests ----------

func TestShellInit_BashSyntaxValidation(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	initScript := shellInitScript(t, tc, "bash")
	if err := tc.WriteFile("/test/syntax-check.sh", initScript); err != nil {
		t.Fatalf("write script: %v", err)
	}

	output, exitCode, err := tc.ExecCommand("bash", "-n", "/test/syntax-check.sh")
	if err != nil {
		t.Fatalf("exec bash -n: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("bash syntax error (exit %d):\n%s", exitCode, output)
	}
}

func TestShellInit_ZshSyntaxValidation(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	initScript := shellInitScript(t, tc, "zsh")
	if err := tc.WriteFile("/test/syntax-check.zsh", initScript); err != nil {
		t.Fatalf("write script: %v", err)
	}

	output, exitCode, err := tc.ExecCommand("zsh", "-n", "/test/syntax-check.zsh")
	if err != nil {
		t.Fatalf("exec zsh -n: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("zsh syntax error (exit %d):\n%s", exitCode, output)
	}
}

func TestShellInit_FishSyntaxValidation(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	initScript := shellInitScript(t, tc, "fish")
	if err := tc.WriteFile("/test/syntax-check.fish", initScript); err != nil {
		t.Fatalf("write script: %v", err)
	}

	output, exitCode, err := tc.ExecCommand("fish", "-n", "/test/syntax-check.fish")
	if err != nil {
		t.Fatalf("exec fish -n: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("fish syntax error (exit %d):\n%s", exitCode, output)
	}
}

// ---------- Bash behavior tests ----------

func TestShellInit_BashCampWrapperGo(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	const targetDir = "/test/nav-target"
	_, _, err := tc.ExecCommand("mkdir", "-p", targetDir)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	initScript := shellInitScript(t, tc, "bash")
	stub := stubCampScriptPosix(targetDir)

	t.Run("camp_go_invokes_print_and_cds", func(t *testing.T) {
		stdout, exitCode := runBashScript(t, tc, initScript, stub, `
camp go foo
echo "PWD=$PWD"
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "PWD="+targetDir) {
			t.Errorf("expected PWD=%s, got:\n%s", targetDir, stdout)
		}
	})

	t.Run("camp_go_no_args_invokes_print_and_cds", func(t *testing.T) {
		stdout, exitCode := runBashScript(t, tc, initScript, stub, `
camp go
echo "PWD=$PWD"
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "PWD="+targetDir) {
			t.Errorf("expected PWD=%s, got:\n%s", targetDir, stdout)
		}
	})

	t.Run("camp_non_nav_subcommand_passes_through", func(t *testing.T) {
		stdout, exitCode := runBashScript(t, tc, initScript, stub, `
camp version
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
			t.Errorf("expected passthrough, got:\n%s", stdout)
		}
	})
}

func TestShellInit_BashCgoWrapper(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	const targetDir = "/test/cgo-target"
	_, _, err := tc.ExecCommand("mkdir", "-p", targetDir)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	initScript := shellInitScript(t, tc, "bash")
	stub := stubCampScriptPosix(targetDir)

	stdout, exitCode := runBashScript(t, tc, initScript, stub, `
cgo foo
echo "PWD=$PWD"
`)
	if exitCode != 0 {
		t.Fatalf("exit code %d, output: %s", exitCode, stdout)
	}
	if !strings.Contains(stdout, "PWD="+targetDir) {
		t.Errorf("expected PWD=%s, got:\n%s", targetDir, stdout)
	}
}

func TestShellInit_BashAliasWrappers(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	const targetDir = "/test/alias-target"
	_, _, err := tc.ExecCommand("mkdir", "-p", targetDir)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	initScript := shellInitScript(t, tc, "bash")
	stub := stubCampScriptPosix(targetDir)

	t.Run("cr_delegates_to_camp_run", func(t *testing.T) {
		stdout, exitCode := runBashScript(t, tc, initScript, stub, `
cr echo hello
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
			t.Errorf("expected passthrough for cr, got:\n%s", stdout)
		}
	})

	t.Run("csw_delegates_to_camp_switch", func(t *testing.T) {
		stdout, exitCode := runBashScript(t, tc, initScript, stub, `
csw mycamp
echo "PWD=$PWD"
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "PWD="+targetDir) {
			t.Errorf("expected PWD=%s after csw, got:\n%s", targetDir, stdout)
		}
	})

	t.Run("cint_delegates_to_camp_intent_add", func(t *testing.T) {
		stdout, exitCode := runBashScript(t, tc, initScript, stub, `
cint "my idea"
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
			t.Errorf("expected passthrough for cint, got:\n%s", stdout)
		}
	})

	t.Run("cie_delegates_to_camp_intent_explore", func(t *testing.T) {
		stdout, exitCode := runBashScript(t, tc, initScript, stub, `
cie
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
			t.Errorf("expected passthrough for cie, got:\n%s", stdout)
		}
	})
}

func TestShellInit_BashCompletionFunction(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)
	ensureCampInPath(t, tc)

	initScript := shellInitScript(t, tc, "bash")

	t.Run("cgo_completion_registered", func(t *testing.T) {
		stdout, _ := runBashScriptNoStub(t, tc, initScript, `
complete -p cgo 2>/dev/null && echo "CGO_COMPLETION_REGISTERED"
`)
		if !strings.Contains(stdout, "CGO_COMPLETION_REGISTERED") {
			t.Errorf("cgo completion not registered, output:\n%s", stdout)
		}
	})

	t.Run("camp_completion_registered", func(t *testing.T) {
		stdout, _ := runBashScriptNoStub(t, tc, initScript, `
complete -p camp 2>/dev/null && echo "CAMP_COMPLETION_REGISTERED"
`)
		if !strings.Contains(stdout, "CAMP_COMPLETION_REGISTERED") {
			t.Errorf("camp completion not registered, output:\n%s", stdout)
		}
	})
}

// ---------- Zsh behavior tests ----------

func TestShellInit_ZshCampWrapperGo(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	const targetDir = "/test/zsh-nav-target"
	_, _, err := tc.ExecCommand("mkdir", "-p", targetDir)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	initScript := shellInitScript(t, tc, "zsh")
	stub := stubCampScriptPosix(targetDir)

	t.Run("camp_go_invokes_print_and_cds", func(t *testing.T) {
		stdout, exitCode := runZshScript(t, tc, initScript, stub, `
camp go foo
echo "PWD=$PWD"
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "PWD="+targetDir) {
			t.Errorf("expected PWD=%s, got:\n%s", targetDir, stdout)
		}
	})

	t.Run("camp_go_no_args_invokes_print_and_cds", func(t *testing.T) {
		stdout, exitCode := runZshScript(t, tc, initScript, stub, `
camp go
echo "PWD=$PWD"
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "PWD="+targetDir) {
			t.Errorf("expected PWD=%s, got:\n%s", targetDir, stdout)
		}
	})

	t.Run("camp_non_nav_subcommand_passes_through", func(t *testing.T) {
		stdout, exitCode := runZshScript(t, tc, initScript, stub, `
camp version
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
			t.Errorf("expected passthrough, got:\n%s", stdout)
		}
	})
}

func TestShellInit_ZshCgoWrapper(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	const targetDir = "/test/zsh-cgo-target"
	_, _, err := tc.ExecCommand("mkdir", "-p", targetDir)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	initScript := shellInitScript(t, tc, "zsh")
	stub := stubCampScriptPosix(targetDir)

	stdout, exitCode := runZshScript(t, tc, initScript, stub, `
cgo foo
echo "PWD=$PWD"
`)
	if exitCode != 0 {
		t.Fatalf("exit code %d, output: %s", exitCode, stdout)
	}
	if !strings.Contains(stdout, "PWD="+targetDir) {
		t.Errorf("expected PWD=%s, got:\n%s", targetDir, stdout)
	}
}

func TestShellInit_ZshAliasWrappers(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	const targetDir = "/test/zsh-alias-target"
	_, _, err := tc.ExecCommand("mkdir", "-p", targetDir)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	initScript := shellInitScript(t, tc, "zsh")
	stub := stubCampScriptPosix(targetDir)

	t.Run("cr_delegates_to_camp_run", func(t *testing.T) {
		stdout, exitCode := runZshScript(t, tc, initScript, stub, `
cr echo hello
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
			t.Errorf("expected passthrough for cr, got:\n%s", stdout)
		}
	})

	t.Run("csw_delegates_to_camp_switch", func(t *testing.T) {
		stdout, exitCode := runZshScript(t, tc, initScript, stub, `
csw mycamp
echo "PWD=$PWD"
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "PWD="+targetDir) {
			t.Errorf("expected PWD=%s after csw, got:\n%s", targetDir, stdout)
		}
	})

	t.Run("cint_delegates_to_camp_intent_add", func(t *testing.T) {
		stdout, exitCode := runZshScript(t, tc, initScript, stub, `
cint "my idea"
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
			t.Errorf("expected passthrough for cint, got:\n%s", stdout)
		}
	})

	t.Run("cie_delegates_to_camp_intent_explore", func(t *testing.T) {
		stdout, exitCode := runZshScript(t, tc, initScript, stub, `
cie
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
			t.Errorf("expected passthrough for cie, got:\n%s", stdout)
		}
	})
}

func TestShellInit_ZshCompdefRegistered(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	// Verify the generated script contains the compdef directives
	initScript := shellInitScript(t, tc, "zsh")

	checks := []string{
		"compdef _cgo cgo",
		"compdef _camp camp",
		"compdef _csw csw",
	}
	for _, check := range checks {
		if !strings.Contains(initScript, check) {
			t.Errorf("zsh init missing: %s", check)
		}
	}
}

// ---------- Fish behavior tests ----------

func TestShellInit_FishCampWrapperGo(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	const targetDir = "/test/fish-nav-target"
	_, _, err := tc.ExecCommand("mkdir", "-p", targetDir)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	initScript := shellInitScript(t, tc, "fish")
	stub := stubCampScriptFish(targetDir)

	t.Run("camp_go_invokes_print_and_cds", func(t *testing.T) {
		stdout, exitCode := runFishScript(t, tc, initScript, stub, `
camp go foo
echo "PWD=$PWD"
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "PWD="+targetDir) {
			t.Errorf("expected PWD=%s, got:\n%s", targetDir, stdout)
		}
	})

	t.Run("camp_non_nav_subcommand_passes_through", func(t *testing.T) {
		stdout, exitCode := runFishScript(t, tc, initScript, stub, `
camp version
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
			t.Errorf("expected passthrough, got:\n%s", stdout)
		}
	})
}

func TestShellInit_FishCgoWrapper(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	const targetDir = "/test/fish-cgo-target"
	_, _, err := tc.ExecCommand("mkdir", "-p", targetDir)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	initScript := shellInitScript(t, tc, "fish")
	stub := stubCampScriptFish(targetDir)

	stdout, exitCode := runFishScript(t, tc, initScript, stub, `
cgo foo
echo "PWD=$PWD"
`)
	if exitCode != 0 {
		t.Fatalf("exit code %d, output: %s", exitCode, stdout)
	}
	if !strings.Contains(stdout, "PWD="+targetDir) {
		t.Errorf("expected PWD=%s, got:\n%s", targetDir, stdout)
	}
}

func TestShellInit_FishAliasWrappers(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)

	const targetDir = "/test/fish-alias-target"
	_, _, err := tc.ExecCommand("mkdir", "-p", targetDir)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	initScript := shellInitScript(t, tc, "fish")
	stub := stubCampScriptFish(targetDir)

	t.Run("cr_delegates_to_camp_run", func(t *testing.T) {
		stdout, exitCode := runFishScript(t, tc, initScript, stub, `
cr echo hello
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
			t.Errorf("expected passthrough for cr, got:\n%s", stdout)
		}
	})

	t.Run("cint_delegates_to_camp_intent_add", func(t *testing.T) {
		stdout, exitCode := runFishScript(t, tc, initScript, stub, `
cint "my idea"
`)
		if exitCode != 0 {
			t.Fatalf("exit code %d, output: %s", exitCode, stdout)
		}
		if !strings.Contains(stdout, "STUB_PASSTHROUGH") {
			t.Errorf("expected passthrough for cint, got:\n%s", stdout)
		}
	})
}
