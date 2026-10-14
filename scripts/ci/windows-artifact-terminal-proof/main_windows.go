//go:build windows

// This proof runs the installed camp.exe in a real Windows ConPTY. Its git.exe
// fixture supplies only the two untracked artifact names because Server Core
// does not include Git; Camp's inventory, TUI, and Windows Shell are real.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/x/conpty"
	"github.com/charmbracelet/x/vt"
	"golang.org/x/sys/windows"
)

const (
	firstName  = "a open [artifact].cmd"
	secondName = "z no-handler.cf0006-unassociated"
)

type inventory struct {
	Files []struct {
		Path string `json:"path"`
	} `json:"files"`
}

type terminal struct {
	pty    *conpty.ConPty
	screen *vt.Emulator
	chunks chan []byte
	raw    bytes.Buffer
}

func main() {
	if strings.EqualFold(filepath.Base(os.Args[0]), "git.exe") {
		gitFixture()
		return
	}
	workRoot := flag.String("work-root", "", "isolated Windows container directory")
	flag.Parse()
	if *workRoot == "" {
		fmt.Fprintln(os.Stderr, "-work-root is required")
		os.Exit(2)
	}
	if err := run(*workRoot); err != nil {
		fmt.Fprintf(os.Stderr, "Windows installed artifact ConPTY proof: FAIL: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Windows installed artifact ConPTY proof: PASS")
}

func gitFixture() {
	args := strings.Join(os.Args[1:], " ")
	if !strings.Contains(args, " ls-files -z --others -- ") || !strings.HasSuffix(args, " :(literal)media") {
		fmt.Fprintf(os.Stderr, "unexpected git fixture request: %q\n", args)
		os.Exit(2)
	}
	if _, err := os.Stdout.Write([]byte("media/" + firstName + "\x00media/" + secondName + "\x00")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(workRoot string) error {
	bin := filepath.Join(workRoot, "bin")
	camp := filepath.Join(bin, "camp.exe")
	root := filepath.Join(workRoot, "artifact-terminal-camp")
	media := filepath.Join(root, "media")
	if err := os.MkdirAll(filepath.Join(root, ".campaign"), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(media, 0o700); err != nil {
		return err
	}
	for name, content := range map[string]string{
		".campaign/campaign.yaml":  "id: aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee\nname: artifact-terminal\ntype: product\n",
		".campaign/artifacts.yaml": "version: 1\nroots:\n  - path: media\n    policy: always\n",
		"media/" + firstName:       "@echo off\r\necho opened>\"" + filepath.Join(workRoot, "artifact-opened.txt") + "\"\r\n",
		"media/" + secondName:      "no file association",
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(content), 0o600); err != nil {
			return fmt.Errorf("write fixture %s: %w", name, err)
		}
	}
	if err := os.Setenv("CAMP_ROOT", root); err != nil {
		return err
	}
	if err := os.Setenv("CAMP_NO_DEFER", "1"); err != nil {
		return err
	}
	if err := os.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH")); err != nil {
		return err
	}
	if err := checkInventory(camp, root); err != nil {
		return err
	}

	const width, height = 100, 30
	pty, err := conpty.New(width, height, 0)
	if err != nil {
		return fmt.Errorf("ConPTY unavailable: %w", err)
	}
	defer pty.Close()
	term := &terminal{pty: pty, screen: vt.NewEmulator(width, height), chunks: make(chan []byte, 64)}
	go term.readLoop()
	pathOutput := filepath.Join(workRoot, "artifact-selected-path.txt")
	pid, processHandle, err := pty.Spawn(camp, []string{camp, "artifacts", "--path-output", pathOutput}, &syscall.ProcAttr{
		Dir: root, Env: os.Environ(),
	})
	if err != nil {
		return fmt.Errorf("start camp artifacts in ConPTY: %w", err)
	}
	process := windows.Handle(processHandle)
	defer windows.CloseHandle(process)
	finished := false
	defer func() {
		if !finished {
			_ = windows.TerminateProcess(process, 1)
		}
	}()
	fmt.Printf("ConPTY spawned installed camp.exe PID %d\n", pid)

	if err := term.waitFor("artifact inventory", 15*time.Second, func(screen string) bool {
		return strings.Contains(screen, firstName) && strings.Contains(screen, secondName)
	}); err != nil {
		return err
	}
	if err := term.press("o"); err != nil {
		return err
	}
	if err := term.waitFor("opened status", 12*time.Second, func(screen string) bool {
		return strings.Contains(screen, "Opened "+firstName)
	}); err != nil {
		return err
	}
	term.record("opened")
	if err := waitForFile(filepath.Join(workRoot, "artifact-opened.txt"), 12*time.Second); err != nil {
		return fmt.Errorf("Shell handler did not run: %w", err)
	}
	marker, err := os.ReadFile(filepath.Join(workRoot, "artifact-opened.txt"))
	if err != nil || strings.TrimSpace(string(marker)) != "opened" {
		return fmt.Errorf("Shell handler marker = %q, error %v", marker, err)
	}
	beforeMove := term.raw.Len()
	if err := term.press("j"); err != nil {
		return err
	}
	if err := term.waitFor("next artifact selection", 12*time.Second, func(string) bool {
		return term.raw.Len() > beforeMove
	}); err != nil {
		return err
	}
	if err := term.press("o"); err != nil {
		return err
	}
	if err := term.waitFor("missing association error", 12*time.Second, func(screen string) bool {
		return strings.Contains(screen, "no application is associated") && strings.Contains(screen, secondName)
	}); err != nil {
		return err
	}
	term.record("missing association")
	beforeMove = term.raw.Len()
	if err := term.press("k"); err != nil {
		return err
	}
	if err := term.waitFor("navigation selection after error", 12*time.Second, func(string) bool {
		return term.raw.Len() > beforeMove
	}); err != nil {
		return err
	}
	if err := term.press("g"); err != nil {
		return err
	}
	if err := waitForFile(pathOutput, 12*time.Second); err != nil {
		return fmt.Errorf("navigation unavailable after open/error: %w", err)
	}
	if result, err := windows.WaitForSingleObject(process, 12_000); err != nil || result != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("camp artifacts did not exit after g: wait=%d error=%v", result, err)
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(process, &exitCode); err != nil || exitCode != 0 {
		return fmt.Errorf("camp artifacts exit code %d: %v", exitCode, err)
	}
	finished = true
	selected, err := os.ReadFile(pathOutput)
	if err != nil || !strings.EqualFold(strings.TrimSpace(string(selected)), media) {
		return fmt.Errorf("selected directory = %q, error %v; want %q", selected, err, media)
	}
	if !bytes.Contains(term.raw.Bytes(), []byte("\x1b[")) {
		return errors.New("ConPTY capture contained no VT control sequences")
	}
	if err := checkInventory(camp, root); err != nil {
		return fmt.Errorf("fresh-process inventory readback: %w", err)
	}
	fmt.Printf("Shell marker and selected directory readback: %s\n", media)
	return nil
}

func checkInventory(camp, root string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, camp, "artifacts", "--json")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("camp artifacts --json: %w: %s", err, out)
	}
	var result inventory
	if err := json.Unmarshal(out, &result); err != nil {
		return fmt.Errorf("parse camp artifacts --json: %w: %s", err, out)
	}
	if len(result.Files) != 2 || result.Files[0].Path != "media/"+firstName || result.Files[1].Path != "media/"+secondName {
		return fmt.Errorf("unexpected artifact inventory: %+v", result.Files)
	}
	return nil
}

func (t *terminal) readLoop() {
	defer close(t.chunks)
	buf := make([]byte, 32*1024)
	for {
		n, err := t.pty.Read(buf)
		if n > 0 {
			t.chunks <- append([]byte(nil), buf[:n]...)
		}
		if err != nil {
			return
		}
	}
}

func (t *terminal) waitFor(label string, timeout time.Duration, matches func(string) bool) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		if matches(t.screen.String()) {
			return nil
		}
		select {
		case chunk, ok := <-t.chunks:
			if !ok {
				return fmt.Errorf("ConPTY closed before %s; screen:\n%s", label, t.screen.String())
			}
			if _, err := t.raw.Write(chunk); err != nil {
				return err
			}
			if _, err := t.screen.Write(chunk); err != nil {
				return fmt.Errorf("render VT stream: %w", err)
			}
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for %s; screen:\n%s", label, t.screen.String())
		}
	}
}

func (t *terminal) press(key string) error {
	if _, err := t.pty.Write([]byte(key)); err != nil {
		return fmt.Errorf("send %q to artifact explorer: %w", key, err)
	}
	return nil
}

func (t *terminal) record(label string) {
	fmt.Printf("--- artifact %s, %d VT bytes ---\n%s\n", label, t.raw.Len(), t.screen.String())
}

func waitForFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s", path)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
