package ui

import (
	"errors"
	"os/exec"
	"reflect"
	"testing"
)

func stubNativeClipboard(t *testing.T, goos string, installed []string) *[]clipboardCommand {
	t.Helper()
	origLookPath := clipboardLookPath
	origRun := clipboardRun
	origGOOS := clipboardGOOS
	t.Cleanup(func() {
		clipboardLookPath = origLookPath
		clipboardRun = origRun
		clipboardGOOS = origGOOS
	})
	clipboardGOOS = goos

	present := make(map[string]bool, len(installed))
	for _, name := range installed {
		present[name] = true
	}
	clipboardLookPath = func(name string) (string, error) {
		if present[name] {
			return "/usr/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}

	var attempted []clipboardCommand
	clipboardRun = func(candidate clipboardCommand, _ string) error {
		attempted = append(attempted, candidate)
		return nil
	}
	return &attempted
}

func commandNames(cmds []clipboardCommand) []string {
	names := make([]string, 0, len(cmds))
	for _, c := range cmds {
		names = append(names, c.name)
	}
	return names
}

func TestClipboardCandidatesByPlatform(t *testing.T) {
	tests := []struct {
		name    string
		goos    string
		env     map[string]string
		want    []string
		wantArg map[string][]string
	}{
		{
			name: "darwin uses pbcopy",
			goos: "darwin",
			want: []string{"pbcopy"},
		},
		{
			name:    "windows uses clip",
			goos:    "windows",
			want:    []string{"cmd"},
			wantArg: map[string][]string{"cmd": {"/c", "clip"}},
		},
		{
			name: "wayland session prefers wl-copy",
			goos: "linux",
			env:  map[string]string{"WAYLAND_DISPLAY": "wayland-0"},
			want: []string{"wl-copy", "xclip", "xsel"},
		},
		{
			name: "x11 session prefers xclip then xsel",
			goos: "linux",
			env:  map[string]string{"DISPLAY": ":0"},
			want: []string{"xclip", "xsel", "wl-copy"},
		},
		{
			name: "xwayland session still prefers wl-copy",
			goos: "linux",
			env:  map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"},
			want: []string{"wl-copy", "xclip", "xsel"},
		},
		{
			name: "no display variables still offers every candidate",
			goos: "linux",
			want: []string{"wl-copy", "xclip", "xsel"},
		},
		{
			name: "bsd follows the unix chain",
			goos: "freebsd",
			env:  map[string]string{"DISPLAY": ":0"},
			want: []string{"xclip", "xsel", "wl-copy"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("WAYLAND_DISPLAY", "")
			t.Setenv("DISPLAY", "")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			got := clipboardCandidates(tt.goos)
			if diff := commandNames(got); !reflect.DeepEqual(diff, tt.want) {
				t.Fatalf("candidates = %v, want %v", diff, tt.want)
			}
			for _, c := range got {
				want, ok := tt.wantArg[c.name]
				if !ok {
					continue
				}
				if !reflect.DeepEqual(c.args, want) {
					t.Errorf("%s args = %v, want %v", c.name, c.args, want)
				}
			}
		})
	}
}

func TestWriteNativeClipboardUsesFirstInstalledCandidate(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", ":0")
	attempted := stubNativeClipboard(t, "linux", []string{"xsel"})

	if err := writeNativeClipboard("WI-309dae"); err != nil {
		t.Fatalf("writeNativeClipboard: %v", err)
	}
	if got := commandNames(*attempted); !reflect.DeepEqual(got, []string{"xsel"}) {
		t.Fatalf("attempted = %v, want only xsel", got)
	}
}

func TestWriteNativeClipboardFallsThroughAFailingCandidate(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("DISPLAY", "")
	stubNativeClipboard(t, "linux", []string{"wl-copy", "xclip"})

	var attempted []string
	clipboardRun = func(candidate clipboardCommand, _ string) error {
		attempted = append(attempted, candidate.name)
		if candidate.name == "wl-copy" {
			return errors.New("compositor refused")
		}
		return nil
	}

	if err := writeNativeClipboard("WI-309dae"); err != nil {
		t.Fatalf("writeNativeClipboard: %v", err)
	}
	if !reflect.DeepEqual(attempted, []string{"wl-copy", "xclip"}) {
		t.Fatalf("attempted = %v, want wl-copy then xclip", attempted)
	}
}

func TestWriteNativeClipboardReportsMissingCommands(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")
	attempted := stubNativeClipboard(t, "linux", nil)

	err := writeNativeClipboard("WI-309dae")
	if !errors.Is(err, errNoClipboardCommand) {
		t.Fatalf("err = %v, want errNoClipboardCommand", err)
	}
	if len(*attempted) != 0 {
		t.Fatalf("attempted %v with nothing installed", commandNames(*attempted))
	}
}

func TestWriteClipboardFallsBackToTerminalWhenNoCommandInstalled(t *testing.T) {
	output := stubClipboard(t)
	stubNativeClipboard(t, "linux", nil)
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")
	t.Setenv("STY", "")
	remoteClipboardSession = func() bool { return false }
	nativeClipboardWrite = writeNativeClipboard

	if err := writeClipboard("WI-309dae"); err != nil {
		t.Fatalf("writeClipboard: %v", err)
	}
	if output.Len() == 0 {
		t.Fatal("OSC 52 fallback did not run when no clipboard command was installed")
	}
}
