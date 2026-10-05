package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/remote"
	"github.com/spf13/cobra"
)

func fleetReviewEnv(t *testing.T, withLocal bool) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CAMP_REGISTRY_PATH", filepath.Join(dir, "registry.json"))
	t.Setenv("CAMP_MACHINES_PATH", filepath.Join(dir, "machines.yaml"))
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv(HopOriginEnvVar, "")
	clearSSHSessionEnv(t)
	if withLocal {
		reg := config.NewRegistry()
		if err := reg.Register("alpha-id", "alpha", dir, config.CampaignTypeProduct); err != nil {
			t.Fatal(err)
		}
		if err := config.SaveRegistry(context.Background(), reg); err != nil {
			t.Fatal(err)
		}
	}
}

func fleetReviewCmd(scope cmdutil.CampaignScope, connect bool) (*cobra.Command, *bytes.Buffer) {
	c := &cobra.Command{}
	c.SetContext(context.Background())
	out := &bytes.Buffer{}
	c.SetOut(out)
	c.SetErr(&bytes.Buffer{})
	c.Flags().Bool("print", false, "")
	c.Flags().Bool("json", false, "")
	c.Flags().Bool("shell-connect", connect, "")
	c.Flags().String("org", scope.Org, "")
	c.Flags().String("status", scope.Status, "")
	c.Flags().Bool("all", scope.All, "")
	return c, out
}

func stubFleet(t *testing.T, enumerate enumerateFunc) {
	t.Helper()
	prev := fleetEnumerator
	fleetEnumerator = func(listFilter) enumerateFunc { return enumerate }
	t.Cleanup(func() { fleetEnumerator = prev })
}

func writeReviewMachines(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(os.Getenv("CAMP_MACHINES_PATH"), []byte("version: 1\nmachines:\n  - id: remote-a\n    host: remote-a.example\n  - id: remote-b\n    host: remote-b.example\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRunSwitchFleetErrorsReachUser(t *testing.T) {
	for _, withLocal := range []bool{false, true} {
		for _, kind := range []string{"ambiguous", "unreachable", "cancelled"} {
			t.Run(kind+map[bool]string{true: "-local", false: "-empty"}[withLocal], func(t *testing.T) {
				fleetReviewEnv(t, withLocal)
				writeReviewMachines(t)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				stubFleet(t, func(context.Context, *machines.Machine) ([]campaignEntry, error) {
					switch kind {
					case "unreachable":
						return nil, errors.New("offline")
					case "cancelled":
						cancel()
						return nil, context.Canceled
					default:
						return []campaignEntry{{ID: "notes-id", Name: "notes", Status: config.StatusActive}}, nil
					}
				})
				cmd, _ := fleetReviewCmd(cmdutil.CampaignScope{}, false)
				cmd.SetContext(ctx)
				err := runSwitch(cmd, []string{"notes"})
				if err == nil {
					t.Fatal("expected fleet error")
				}
				want := map[string]string{"ambiguous": "more than one remote camp", "unreachable": "every remote machine failed", "cancelled": "context canceled"}[kind]
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error = %v, want %q", err, want)
				}
				if kind == "cancelled" && !errors.Is(err, context.Canceled) {
					t.Fatalf("lost cancellation: %v", err)
				}
			})
		}
	}
}

func TestRunSwitchExplicitLocalNeverSearchesFleet(t *testing.T) {
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	for _, withLocal := range []bool{false, true} {
		for _, machine := range []string{"local", host} {
			t.Run(machine+map[bool]string{true: "-local", false: "-empty"}[withLocal], func(t *testing.T) {
				fleetReviewEnv(t, withLocal)
				writeReviewMachines(t)
				stubFleet(t, func(context.Context, *machines.Machine) ([]campaignEntry, error) {
					t.Error("explicit local selector searched fleet")
					return nil, nil
				})
				cmd, _ := fleetReviewCmd(cmdutil.CampaignScope{}, false)
				if err := runSwitch(cmd, []string{machine + ":notes"}); err == nil {
					t.Fatal("expected local miss")
				}
			})
		}
	}
}

func TestRunSwitchOriginOnlyPreservesScope(t *testing.T) {
	cases := []struct {
		name, query string
		scope       cmdutil.CampaignScope
		want        bool
	}{
		{"default-active", "notes", cmdutil.CampaignScope{}, false},
		{"active-origin", "notes", cmdutil.CampaignScope{}, true},
		{"wrong-org", "notes", cmdutil.CampaignScope{Org: "other", All: true}, false},
		{"wrong-status", "notes", cmdutil.CampaignScope{Status: "reference"}, false},
		{"inactive", "notes", cmdutil.CampaignScope{Org: "work", Status: "inactive"}, true},
		{"all", "work/notes@p", cmdutil.CampaignScope{All: true}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fleetReviewEnv(t, false)
			t.Setenv("SSH_TTY", "/dev/pts/3")
			t.Setenv(HopOriginEnvVar, "v1;host=origin.example;user=alex;id=origin;campaign=notes")
			t.Setenv(hopResumeSockEnv, "/tmp/review-resume.sock")
			stubFleet(t, func(_ context.Context, m *machines.Machine) ([]campaignEntry, error) {
				if m.Host != "origin.example" {
					t.Errorf("wrong origin: %+v", m)
				}
				status := "inactive"
				if tc.name == "active-origin" {
					status = "active"
				}
				return []campaignEntry{{ID: "notes-id", Name: "notes", Org: "work", Status: status}}, nil
			})
			cmd, out := fleetReviewCmd(tc.scope, true)
			err := runSwitch(cmd, []string{tc.query})
			if !tc.want {
				if err == nil || out.Len() != 0 {
					t.Fatalf("out-of-scope origin selected: %v %s", err, out)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			selector := "local:work/notes-id"
			if strings.Contains(tc.query, "@p") {
				selector += "@p"
			}
			if !strings.Contains(out.String(), remote.ShellQuote(selector)) || !strings.Contains(out.String(), "hop-resume send") {
				t.Fatalf("lost scoped identity: %s", out)
			}
			if tc.scope.Status != "" && !strings.Contains(out.String(), "--status 'inactive'") {
				t.Fatalf("status lost: %s", out)
			}
			if tc.scope.All && !strings.Contains(out.String(), "--all") {
				t.Fatalf("all lost: %s", out)
			}
		})
	}
}

func TestFleetRemoteResolutionKeepsSelectedIdentityAndScope(t *testing.T) {
	fleetReviewEnv(t, false)
	writeReviewMachines(t)
	// A cache from list --all must not make an inactive camp eligible by default.
	writeMachineCacheCampaigns("remote-a", []string{"notes"})
	stubFleet(t, func(_ context.Context, m *machines.Machine) ([]campaignEntry, error) {
		if m.ID != "remote-a" {
			return nil, nil
		}
		return []campaignEntry{
			{ID: "first-id", Name: "notes", Org: "personal", Status: "active"},
			{ID: "second-id", Name: "notes", Org: "work", Status: "inactive"},
		}, nil
	})
	prev := resolveRemoteRootScoped
	t.Cleanup(func() { resolveRemoteRootScoped = prev })
	called := false
	resolveRemoteRootScoped = func(_ context.Context, m *machines.Machine, sel string, scope remote.SwitchScope) (string, error) {
		called = true
		if m.ID != "remote-a" || sel != "work/second-id@p" || scope.Org != "work" || scope.Status != "inactive" {
			t.Errorf("resolution lost identity/scope: %s %s %+v", m.ID, sel, scope)
		}
		return "/remote/projects", nil
	}
	cmd, _ := fleetReviewCmd(cmdutil.CampaignScope{Org: "work", Status: "inactive"}, false)
	err := runSwitch(cmd, []string{"notes@p"})
	if !called || err == nil || !strings.Contains(err.Error(), "resolved remote-a:work/second-id@p") {
		t.Fatalf("resolution = %v, called=%v", err, called)
	}
	cmd, _ = fleetReviewCmd(cmdutil.CampaignScope{Org: "work"}, false)
	called = false
	if err := runSwitch(cmd, []string{"notes"}); err == nil || called {
		t.Fatalf("cache bypassed active scope: %v", err)
	}
}

func TestFleetSameNameDifferentOrgsIsAmbiguous(t *testing.T) {
	_, err := matchFleetCamps("notes", []fleetCamp{
		{Machine: "remote", ID: "a", Name: "notes", Org: "personal"},
		{Machine: "remote", ID: "b", Name: "notes", Org: "work"},
	})
	if err == nil || !strings.Contains(err.Error(), "remote:personal/notes") || !strings.Contains(err.Error(), "remote:work/notes") {
		t.Fatalf("ambiguity lost orgs: %v", err)
	}
}

func TestResumeSwitchArgsKeepsScope(t *testing.T) {
	req := hopResumeRequest{Selector: "local:work/notes-id@p", Scope: cmdutil.CampaignScope{Org: "work", Status: "inactive", All: true}}
	want := []string{"switch", "--shell-connect", "--org", "work", "--status", "inactive", "--all", "--", req.Selector}
	if got := resumeSwitchArgs(req); !reflect.DeepEqual(got, want) {
		t.Fatalf("args=%q, want %q", got, want)
	}
}
