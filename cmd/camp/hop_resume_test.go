package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/machines"
)

func TestMapHopResumeHostIdentity(t *testing.T) {
	scope := cmdutil.CampaignScope{Org: "work", Status: "inactive", All: true}
	request := hopResumeRequest{Selector: "thirdbox:work/notes@p", Host: "C.EXAMPLE.", Scope: scope}
	for _, tc := range []struct {
		name    string
		entries []machines.Machine
		want    string
		wantErr string
	}{
		{"colliding alias", []machines.Machine{{ID: "thirdbox", Host: "d.example"}, {ID: "correct", Host: "c.example"}}, "correct:work/notes@p", ""},
		{"alternate alias", []machines.Machine{{ID: "other", Host: "c.example", SSHUser: "parent", AuthMethod: machines.AuthSSHAgent, IdentityFile: "parent.key"}}, "other:work/notes@p", ""},
		{"no route", []machines.Machine{{ID: "thirdbox", Host: "d.example"}}, "", "no route"},
		{"absent alias and route", nil, "", "no route"},
		{"ambiguous routes", []machines.Machine{{ID: "one", Host: "c.example"}, {ID: "two", Host: "C.EXAMPLE."}}, "", "multiple routes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := mapHopResume(request, &machines.File{Machines: tc.entries})
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Selector != tc.want || got.Scope != scope || got.Host != request.Host {
				t.Fatalf("mapped request: %+v", got)
			}
		})
	}
	for _, request := range []hopResumeRequest{{Selector: "notes"}, {Selector: "thirdbox:notes"}, {Selector: "local:notes", Host: "c.example"}} {
		if _, err := mapHopResume(request, &machines.File{}); err == nil {
			t.Fatalf("accepted malformed identity %+v", request)
		}
	}
	local := hopResumeRequest{Selector: "local:work/notes", Scope: scope}
	if got, err := mapHopResume(local, &machines.File{}); err != nil || got != local {
		t.Fatalf("local: %+v %v", got, err)
	}
}

func TestHopResumeAcknowledgesOnlyPreparedRoute(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "rejected"}[reject], func(t *testing.T) {
			client, server := net.Pipe()
			defer func() { _ = client.Close() }()
			defer func() { _ = server.Close() }()
			_ = client.SetDeadline(time.Now().Add(3 * time.Second))
			request := hopResumeRequest{Selector: "thirdbox:notes", Host: "c.example", Scope: cmdutil.CampaignScope{Org: "work"}}
			prepared := make(chan struct{})
			release := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				follow, err := acceptHopResume(context.Background(), server, func(_ context.Context, got hopResumeRequest) (string, error) {
					if got != request {
						return "", errors.New("request changed")
					}
					close(prepared)
					<-release
					if reject {
						return "", errors.New("parent route unavailable")
					}
					return "ssh prepared-parent-route", nil
				})
				if !reject && follow != "ssh prepared-parent-route" {
					err = errors.New("prepared line lost")
				}
				done <- err
			}()
			if err := json.NewEncoder(client).Encode(hopResumeEnvelope{Version: 1, Request: request}); err != nil {
				t.Fatal(err)
			}
			<-prepared
			// The server is inside preparation: no ACK can have been written yet.
			close(release)
			var response hopResumeResponse
			if err := json.NewDecoder(client).Decode(&response); err != nil {
				t.Fatal(err)
			}
			if response.Version != 1 || response.Accepted == reject {
				t.Fatalf("response %+v", response)
			}
			if reject && !strings.Contains(response.Error, "unavailable") {
				t.Fatalf("response %+v", response)
			}
			if err := <-done; (err != nil) != reject {
				t.Fatalf("completion %v", err)
			}
		})
	}
}

func TestHopResumeProtocolFailsClosed(t *testing.T) {
	wire, _ := json.Marshal(hopResumeEnvelope{Version: 1, Request: hopResumeRequest{Selector: "thirdbox:notes", Host: "c.example"}})
	var legacy hopResumeRequest
	if err := json.Unmarshal(wire, &legacy); err != nil || legacy.Selector != "" {
		t.Fatalf("legacy would act: %+v %v", legacy, err)
	}
	for _, payload := range []string{`{"selector":"thirdbox:notes"}`, `{"version":2,"request":{"selector":"local:notes"}}`, `{"version":1,"request":{"selector":""}}`, `{"version":1,"request":{"selector":"bad\nselector"}}`, `{broken}`} {
		t.Run(payload, func(t *testing.T) {
			client, server := net.Pipe()
			defer func() { _ = client.Close() }()
			defer func() { _ = server.Close() }()
			_ = client.SetDeadline(time.Now().Add(3 * time.Second))
			done := make(chan error, 1)
			go func() {
				_, err := acceptHopResume(context.Background(), server, func(context.Context, hopResumeRequest) (string, error) {
					t.Error("invalid request reached preparation")
					return "", nil
				})
				done <- err
			}()
			if _, err := client.Write([]byte(payload + "\n")); err != nil {
				t.Fatal(err)
			}
			var response hopResumeResponse
			if err := json.NewDecoder(client).Decode(&response); err != nil {
				t.Fatal(err)
			}
			if response.Accepted || response.Error == "" {
				t.Fatalf("response %+v", response)
			}
			if err := <-done; err == nil {
				t.Fatal("accepted invalid request")
			}
		})
	}
}

func TestHopResumeCancellationClosesPendingRequest(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := acceptHopResume(ctx, server, func(context.Context, hopResumeRequest) (string, error) {
			t.Error("unexpected preparation")
			return "", nil
		})
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("missing cancellation error")
		}
	case <-time.After(time.Second):
		t.Fatal("pending request survived SSH cancellation")
	}
}

func TestHopTerminalNeverCrossesToControlMasterAsDevTTY(t *testing.T) {
	in, out, ok := inheritedHopTerminal(os.Stdin, os.Stderr, func(*os.File) bool { return true })
	if !ok || in != os.Stdin || out != os.Stderr {
		t.Fatalf("terminal stdio must be handed to ssh as-is: in=%v out=%v ok=%v", in, out, ok)
	}
	for name, isTerminal := range map[string]func(*os.File) bool{
		"stdin redirected":  func(f *os.File) bool { return f != os.Stdin },
		"stderr redirected": func(f *os.File) bool { return f != os.Stderr },
	} {
		if _, _, ok := inheritedHopTerminal(os.Stdin, os.Stderr, isTerminal); ok {
			t.Errorf("%s: redirected stdio must not be treated as the terminal", name)
		}
	}

	argv := withoutControlMaster([]string{"ssh", "-t", "-o", "ControlPath=/run/camp.sock", "devbox", "exec $SHELL -l"})
	none, own := slices.Index(argv, "ControlPath=none"), slices.Index(argv, "ControlPath=/run/camp.sock")
	if none < 0 || own < 0 || none > own {
		t.Fatalf("ControlPath=none must precede the line's own ControlPath, ssh keeps the first: %q", argv)
	}
	if argv[0] != "ssh" || argv[1] != "-t" || argv[len(argv)-2] != "devbox" || argv[len(argv)-1] != "exec $SHELL -l" {
		t.Fatalf("fallback rewrote the hop itself: %q", argv)
	}
}
