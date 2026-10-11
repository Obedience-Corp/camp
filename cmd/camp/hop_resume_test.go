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
				follow, err := acceptHopResume(context.Background(), server, "", func(_ context.Context, got hopResumeRequest) (string, error) {
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
				_, err := acceptHopResume(context.Background(), server, "", func(context.Context, hopResumeRequest) (string, error) {
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
		_, err := acceptHopResume(ctx, server, "", func(context.Context, hopResumeRequest) (string, error) {
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

func TestHopResumeLoopbackRequiresHopToken(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	request := hopResumeRequest{Selector: "thirdbox:notes", Host: "c.example"}
	for name, tc := range map[string]struct {
		sent   string
		accept bool
	}{
		"this hop's token": {token, true},
		"another token":    {"ffffffffffffffffffffffffffffffff", false},
		"no token":         {"", false},
	} {
		t.Run(name, func(t *testing.T) {
			client, server := net.Pipe()
			defer func() { _ = client.Close() }()
			defer func() { _ = server.Close() }()
			_ = client.SetDeadline(time.Now().Add(3 * time.Second))
			prepared := false
			done := make(chan error, 1)
			go func() {
				_, err := acceptHopResume(context.Background(), server, token, func(context.Context, hopResumeRequest) (string, error) {
					prepared = true
					return "ssh prepared-parent-route", nil
				})
				done <- err
			}()
			if err := json.NewEncoder(client).Encode(hopResumeEnvelope{Version: 1, Token: tc.sent, Request: request}); err != nil {
				t.Fatal(err)
			}
			var response hopResumeResponse
			if err := json.NewDecoder(client).Decode(&response); err != nil {
				t.Fatal(err)
			}
			err := <-done
			if response.Accepted != tc.accept || (err == nil) != tc.accept || prepared != tc.accept {
				t.Fatalf("accepted=%v err=%v route prepared=%v, want accepted=%v", response.Accepted, err, prepared, tc.accept)
			}
			if !tc.accept && !strings.Contains(response.Error, "token") {
				t.Fatalf("rejection must name the token: %+v", response)
			}
		})
	}
}

func TestHopResumeLoopbackAddrComesFromTheServer(t *testing.T) {
	if spec := loopForwardSpec("/tmp/camp-hr-t.sock"); spec != "127.0.0.1:0:/tmp/camp-hr-t.sock" {
		t.Fatalf("forward must bind loopback and leave the port to the server: %q", spec)
	}
	addr, err := allocatedLoopAddr("37979\n")
	if err != nil || addr != "127.0.0.1:37979" {
		t.Fatalf("allocated port: %q %v", addr, err)
	}
	for _, out := range []string{"", "0", "70000", "-1", "Allocated port 37979 for remote forward"} {
		if addr, err := allocatedLoopAddr(out); err == nil {
			t.Errorf("%q must not yield an address to advertise, got %q", out, addr)
		}
	}
}

// serveLoopback runs the hop's loopback listener and returns its address and
// the channel the accepted follow-up line arrives on.
func serveLoopback(t *testing.T, token string) (string, <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return ln.Addr().String(), serveRoute(t, ln, token, new(hopSwitch), func(context.Context, hopResumeRequest) (string, error) {
		return "ssh prepared-parent-route", nil
	})
}

// serveRoute serves one of a hop's listeners until the test ends.
func serveRoute(t *testing.T, ln net.Listener, token string, hop *hopSwitch, prepare func(context.Context, hopResumeRequest) (string, error)) <-chan string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); _ = ln.Close() })
	follow := make(chan string, 1)
	go func() {
		line, _ := serveHopResume(ctx, ln, token, hop, prepare)
		follow <- line
	}()
	return follow
}

func dialSilent(t *testing.T, addr string) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func requestSwitch(t *testing.T, addr, token string) (hopResumeResponse, error) {
	t.Helper()
	return requestSwitchTo(t, addr, token, "thirdbox:notes")
}

func requestSwitchTo(t *testing.T, addr, token, selector string) (hopResumeResponse, error) {
	t.Helper()
	conn := dialSilent(t, addr)
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	envelope := hopResumeEnvelope{Version: 1, Token: token, Request: hopResumeRequest{Selector: selector, Host: "c.example"}}
	if err := json.NewEncoder(conn).Encode(envelope); err != nil {
		return hopResumeResponse{}, err
	}
	var response hopResumeResponse
	err := json.NewDecoder(conn).Decode(&response)
	return response, err
}

func TestHopResumeSilentConnectionsCannotStarveTheHop(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	addr, follow := serveLoopback(t, token)
	// Connections that never send a request, as any far-side user can open:
	// enough to take every waiting place several times over.
	for range 3 * hopResumeMaxWaiting {
		dialSilent(t, addr)
	}
	response, err := requestSwitch(t, addr, token)
	if err != nil || !response.Accepted {
		t.Fatalf("the hop's own request must be answered while silent connections are open: %+v %v", response, err)
	}
	select {
	case line := <-follow:
		if line != "ssh prepared-parent-route" {
			t.Fatalf("follow %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("accepted request did not complete the hop")
	}
}

func TestHopResumeLongestWaitingConnectionGivesWay(t *testing.T) {
	addr, _ := serveLoopback(t, "0123456789abcdef0123456789abcdef")
	oldest := dialSilent(t, addr)
	// Let the listener take it before the rest arrive, so arrival order is fixed.
	time.Sleep(50 * time.Millisecond)
	var newest net.Conn
	for range hopResumeMaxWaiting {
		newest = dialSilent(t, addr)
	}
	_ = oldest.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := oldest.Read(make([]byte, 1)); err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("the longest-waiting connection must be closed to make room: %v", err)
	}
	_ = newest.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, err := newest.Read(make([]byte, 1)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("the newcomer must be admitted, not turned away: %v", err)
	}
}

func TestHopResumeAcceptsOneSwitchPerHop(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	addr, _ := serveLoopback(t, token)
	if response, err := requestSwitch(t, addr, token); err != nil || !response.Accepted {
		t.Fatalf("first switch: %+v %v", response, err)
	}
	response, err := requestSwitch(t, addr, token)
	if err != nil || response.Accepted || !strings.Contains(response.Error, "already switching") {
		t.Fatalf("a second switch must be refused, the hop is already unwinding: %+v %v", response, err)
	}
}

// heldCloseListener hands out connections whose Close blocks until released,
// holding a handler between its acknowledgement and its hand-off.
type heldCloseListener struct {
	net.Listener
	release chan struct{}
}

func (l heldCloseListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return heldCloseConn{Conn: conn, release: l.release}, nil
}

type heldCloseConn struct {
	net.Conn
	release chan struct{}
}

func (c heldCloseConn) Close() error {
	<-c.release
	return c.Conn.Close()
}

func TestHopResumeKeepsAnAcknowledgedSwitchWhenTheHopEndsFirst(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	held := heldCloseListener{Listener: ln, release: make(chan struct{})}
	follow := serveRoute(t, held, token, new(hopSwitch), func(context.Context, hopResumeRequest) (string, error) {
		return "ssh prepared-parent-route", nil
	})
	if response, err := requestSwitch(t, ln.Addr().String(), token); err != nil || !response.Accepted {
		t.Fatalf("switch: %+v %v", response, err)
	}
	// The far shell exits on that acknowledgement and the hop closes its
	// listener while the handler has not handed the line over yet.
	_ = ln.Close()
	select {
	case line := <-follow:
		t.Fatalf("serving ended before the acknowledged handler finished, returning %q", line)
	case <-time.After(200 * time.Millisecond):
	}
	close(held.release)
	select {
	case line := <-follow:
		if line != "ssh prepared-parent-route" {
			t.Fatalf("the acknowledged switch was lost: %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serving never returned")
	}
}

func TestHopResumeInterruptedSenderDoesNotHoldTheSwitch(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hop := new(hopSwitch)
	preparing := make(chan struct{}, 1)
	proceed := make(chan struct{})
	follow := serveRoute(t, ln, token, hop, func(_ context.Context, request hopResumeRequest) (string, error) {
		if request.Selector == "first:notes" {
			preparing <- struct{}{}
			<-proceed
		}
		return "ssh " + request.Selector, nil
	})

	// The sender is interrupted while this side is still preparing its route.
	first := dialSilent(t, ln.Addr().String())
	envelope := hopResumeEnvelope{Version: 1, Token: token, Request: hopResumeRequest{Selector: "first:notes", Host: "c.example"}}
	if err := json.NewEncoder(first).Encode(envelope); err != nil {
		t.Fatal(err)
	}
	<-preparing
	_ = first.Close()
	close(proceed)
	deadline := time.Now().Add(2 * time.Second)
	for !hop.reserve() {
		if time.Now().After(deadline) {
			t.Fatal("a switch whose sender hung up kept the hop reserved")
		}
		time.Sleep(5 * time.Millisecond)
	}
	hop.release()

	response, err := requestSwitchTo(t, ln.Addr().String(), token, "second:notes")
	if err != nil || !response.Accepted {
		t.Fatalf("the retry must be accepted: %+v %v", response, err)
	}
	select {
	case line := <-follow:
		if line != "ssh second:notes" {
			t.Fatalf("follow-up %q, want the retry's; the interrupted switch must not be queued", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the retry did not complete the hop")
	}
}

func TestHopResumeRoutesShareOneSwitch(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	hop := new(hopSwitch)
	var addrs []string
	for range 2 {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		serveRoute(t, ln, token, hop, func(_ context.Context, request hopResumeRequest) (string, error) {
			return "ssh " + request.Selector, nil
		})
		addrs = append(addrs, ln.Addr().String())
	}
	if response, err := requestSwitchTo(t, addrs[0], token, "first:notes"); err != nil || !response.Accepted {
		t.Fatalf("first route: %+v %v", response, err)
	}
	response, err := requestSwitchTo(t, addrs[1], token, "second:notes")
	if err != nil || response.Accepted || !strings.Contains(response.Error, "already switching") {
		t.Fatalf("the hop's other route must refuse a second destination: %+v %v", response, err)
	}
}
