package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
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

// hopRelayRig wires the three parties of the relay route in memory: far shells
// (sendOverHopRelay over net.Pipe), the relay loop, and the opening shell's
// serveHopRelay.
type hopRelayRig struct {
	t      *testing.T
	conns  chan net.Conn
	hop    *hopSwitch
	follow chan string
	endHop context.CancelFunc
}

// pipeListener hands the relay connections the test dials.
type pipeListener struct {
	conns  chan net.Conn
	closed chan struct{}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.conns:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	select {
	case <-l.closed:
	default:
		close(l.closed)
	}
	return nil
}

func (l *pipeListener) Addr() net.Addr { return &net.UnixAddr{Name: "pipe", Net: "unix"} }

func newHopRelayRig(t *testing.T, prepare func(context.Context, hopResumeRequest) (string, error)) *hopRelayRig {
	t.Helper()
	return newSpacedHopRelayRig(t, 0, prepare)
}

// newSpacedHopRelayRig is the rig with the given hopPrepareSpacing, which the
// other tests do without so that none of them waits it out.
func newSpacedHopRelayRig(t *testing.T, spacing time.Duration, prepare func(context.Context, hopResumeRequest) (string, error)) *hopRelayRig {
	t.Helper()
	before := hopPrepareSpacing
	hopPrepareSpacing = spacing
	ctx, cancel := context.WithCancel(context.Background())
	rig := &hopRelayRig{t: t, conns: make(chan net.Conn), hop: new(hopSwitch), follow: make(chan string, 1), endHop: cancel}
	toOpenerR, toOpenerW := io.Pipe()
	toRelayR, toRelayW := io.Pipe()
	ln := &pipeListener{conns: rig.conns, closed: make(chan struct{})}
	served := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		_ = toRelayW.Close()
		_ = toOpenerW.Close()
		<-served
		hopPrepareSpacing = before
	})
	go func() { _ = relayHopExchanges(ctx, ln, toRelayR, toOpenerW) }()
	go func() {
		defer close(served)
		line, _ := serveHopRelay(ctx, toOpenerR, toRelayW, rig.hop, prepare)
		rig.follow <- line
	}()
	return rig
}

// dial opens one far-shell connection to the relay.
func (r *hopRelayRig) dial() net.Conn {
	r.t.Helper()
	far, near := net.Pipe()
	r.t.Cleanup(func() { _ = far.Close() })
	_ = far.SetDeadline(time.Now().Add(3 * time.Second))
	select {
	case r.conns <- near:
	case <-time.After(3 * time.Second):
		r.t.Fatal("the relay is not accepting connections")
	}
	return far
}

func (r *hopRelayRig) send(selector string) error {
	r.t.Helper()
	conn := r.dial()
	defer func() { _ = conn.Close() }()
	return sendOverHopRelay(conn, hopResumeRequest{Selector: selector, Host: "c.example"})
}

// freed waits until the hop can be reserved again and leaves it free.
func (r *hopRelayRig) freed() {
	r.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !r.hop.reserve() {
		if time.Now().After(deadline) {
			r.t.Fatal("the hop stayed reserved for a switch that never completed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.hop.release()
}

// endsWith ends the hop and checks the follow-up line it leaves the opening
// shell with; "" for none.
func (r *hopRelayRig) endsWith(want string) {
	r.t.Helper()
	r.endHop()
	select {
	case line := <-r.follow:
		if line != want {
			r.t.Fatalf("the hop ended with follow-up %q, want %q", line, want)
		}
	case <-time.After(2 * time.Second):
		r.t.Fatal("serving did not end with the hop")
	}
}

func prepareBySelector(_ context.Context, request hopResumeRequest) (string, error) {
	if strings.HasPrefix(request.Selector, "nowhere:") {
		return "", errors.New("parent route unavailable")
	}
	return "ssh " + request.Selector, nil
}

func TestHopRelayCarriesASwitchToTheOpeningShell(t *testing.T) {
	rig := newHopRelayRig(t, prepareBySelector)
	if err := rig.send("thirdbox:notes"); err != nil {
		t.Fatalf("switch over the relay: %v", err)
	}
	// A shell that does not exit at once (running jobs, a nested shell) can
	// ask again, and must be told where it stands.
	err := rig.send("fourthbox:notes")
	if err == nil || !strings.Contains(err.Error(), "already switching; exit this shell") {
		t.Fatalf("after a confirmed switch the relay must keep answering: %v", err)
	}
	rig.endsWith("ssh thirdbox:notes")
}

func TestHopRelayRejectedRequestLeavesTheHopFree(t *testing.T) {
	rig := newHopRelayRig(t, prepareBySelector)
	err := rig.send("nowhere:notes")
	if err == nil || !strings.Contains(err.Error(), "parent route unavailable") {
		t.Fatalf("the far shell must hear why its route was refused: %v", err)
	}
	rig.freed()
	if err := rig.send("thirdbox:notes"); err != nil {
		t.Fatalf("a rejected request must not disable onward switching: %v", err)
	}
	rig.endsWith("ssh thirdbox:notes")
}

func TestHopRelayHangUpWhilePreparingCancelsAndFrees(t *testing.T) {
	preparing := make(chan struct{}, 1)
	cancelled := make(chan struct{}, 1)
	rig := newHopRelayRig(t, func(ctx context.Context, request hopResumeRequest) (string, error) {
		if request.Selector != "slow:notes" {
			return "ssh " + request.Selector, nil
		}
		preparing <- struct{}{}
		<-ctx.Done()
		cancelled <- struct{}{}
		return "", ctx.Err()
	})
	// The far shell is interrupted while the opening shell prepares its route.
	conn := rig.dial()
	out := hopLineWriter{w: conn}
	if err := out.write(hopResumeEnvelope{Version: hopRelayVersion, Request: hopResumeRequest{Selector: "slow:notes", Host: "c.example"}}); err != nil {
		t.Fatal(err)
	}
	<-preparing
	_ = conn.Close()
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("a preparation nobody is waiting for kept running")
	}
	rig.freed()
	if err := rig.send("thirdbox:notes"); err != nil {
		t.Fatalf("the retry must be served: %v", err)
	}
	rig.endsWith("ssh thirdbox:notes")
}

func TestHopRelayTakesBackASwitchTheFarShellNeverConfirmed(t *testing.T) {
	rig := newHopRelayRig(t, prepareBySelector)
	// The far shell reads its acknowledgement and dies before confirming.
	conn := rig.dial()
	out := hopLineWriter{w: conn}
	if err := out.write(hopResumeEnvelope{Version: hopRelayVersion, Request: hopResumeRequest{Selector: "first:notes", Host: "c.example"}}); err != nil {
		t.Fatal(err)
	}
	var response hopResumeResponse
	if err := json.NewDecoder(conn).Decode(&response); err != nil || !response.Accepted {
		t.Fatalf("acknowledgement: %+v %v", response, err)
	}
	_ = conn.Close()
	rig.freed()
	if err := rig.send("second:notes"); err != nil {
		t.Fatalf("the retry must be accepted: %v", err)
	}
	rig.endsWith("ssh second:notes")
}

func TestHopRelayAdmitsOneSwitchPerHop(t *testing.T) {
	rig := newHopRelayRig(t, prepareBySelector)
	// The hop's other route already holds the switch.
	if !rig.hop.reserve() {
		t.Fatal("fresh hop already reserved")
	}
	err := rig.send("thirdbox:notes")
	if err == nil || !strings.Contains(err.Error(), "already switching") {
		t.Fatalf("a second destination must be refused while the hop is switching: %v", err)
	}
	if rig.hop.reserve() {
		t.Fatal("a refused request released a reservation it did not hold")
	}
	rig.endsWith("")
}

func TestHopRelayRefusesMalformedRequests(t *testing.T) {
	rig := newHopRelayRig(t, func(context.Context, hopResumeRequest) (string, error) {
		t.Error("a malformed request reached preparation")
		return "", nil
	})
	for name, line := range map[string]string{
		"the socket route's version": `{"version":1,"request":{"selector":"thirdbox:notes"}}`,
		"an empty selector":          `{"version":2,"request":{"selector":""}}`,
		"hop-back as a selector":     `{"version":2,"request":{"selector":"-"}}`,
		"a bare request":             `{"selector":"thirdbox:notes"}`,
	} {
		conn := rig.dial()
		if _, err := conn.Write([]byte(line + "\n")); err != nil {
			t.Fatal(err)
		}
		var response hopResumeResponse
		if err := json.NewDecoder(conn).Decode(&response); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if response.Accepted || response.Error == "" {
			t.Errorf("%s: response %+v", name, response)
		}
		_ = conn.Close()
		rig.freed()
	}
}

func TestHopRelayIgnoresWhatIsNotAFrame(t *testing.T) {
	// A login shell can print before camp starts, and a line can be longer
	// than any frame.
	toOpenerR, toOpenerW := io.Pipe()
	toRelayR, toRelayW := io.Pipe()
	defer func() { _ = toOpenerW.Close(); _ = toRelayW.Close() }()
	hop := new(hopSwitch)
	follow := make(chan string, 1)
	go func() {
		line, _ := serveHopRelay(context.Background(), toOpenerR, toRelayW, hop, prepareBySelector)
		follow <- line
	}()
	request, err := json.Marshal(hopResumeEnvelope{Version: hopRelayVersion, Request: hopResumeRequest{Selector: "thirdbox:notes", Host: "c.example"}})
	if err != nil {
		t.Fatal(err)
	}
	out := hopLineWriter{w: toOpenerW}
	for _, noise := range []string{"Welcome to devbox", `{"motd":true}`, strings.Repeat("x", 3*hopRelayMaxLine)} {
		if _, err := io.WriteString(toOpenerW, noise+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	if err := out.write(hopRelayFrame{Version: hopRelayVersion, Seq: 1, Request: request}); err != nil {
		t.Fatal(err)
	}
	// The answer is proof the request after the noise was heard.
	answer := make(chan hopRelayFrame, 1)
	go func() {
		var frame hopRelayFrame
		_ = json.NewDecoder(toRelayR).Decode(&frame)
		answer <- frame
	}()
	select {
	case frame := <-answer:
		if frame.Seq != 1 || frame.Response == nil {
			t.Fatalf("answer %+v", frame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the request after the noise was never heard")
	}
	// A confirmation for another exchange must not make this one final: the
	// far shell hanging up afterwards still takes the switch back.
	for _, frame := range []hopRelayFrame{
		{Version: hopRelayVersion, Seq: 7, Confirm: true},
		{Version: hopRelayVersion, Seq: 1, Gone: true},
	} {
		if err := out.write(frame); err != nil {
			t.Fatal(err)
		}
	}
	_ = toOpenerW.Close()
	select {
	case line := <-follow:
		if line != "" {
			t.Fatalf("a confirmation for another exchange kept %q queued", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serving did not end with the relay link")
	}
	if !hop.reserve() {
		t.Fatal("the hop stayed reserved")
	}
}

func TestHopRelayDropsAnAnswerToAnAbandonedExchange(t *testing.T) {
	far, near := net.Pipe()
	defer func() { _ = far.Close(); _ = near.Close() }()
	_ = far.SetDeadline(time.Now().Add(3 * time.Second))
	toOpenerR, toOpenerW := io.Pipe()
	defer func() { _ = toOpenerW.Close() }()
	forwarded := make(chan struct{}, 1)
	go func() {
		reader := bufio.NewReader(toOpenerR)
		if _, err := reader.ReadBytes('\n'); err == nil {
			forwarded <- struct{}{}
		}
		_, _ = io.Copy(io.Discard, reader)
	}()
	var answers hopRelayAnswers
	toOpener := hopLineWriter{w: toOpenerW}
	done := make(chan error, 1)
	go func() { done <- relayHopExchange(context.Background(), near, 2, &toOpener, &answers) }()
	if _, err := far.Write([]byte(`{"version":2,"request":{"selector":"thirdbox:notes"}}` + "\n")); err != nil {
		t.Fatal(err)
	}
	<-forwarded
	// The opening shell answers exchange 1, which its far shell abandoned,
	// and then this one.
	answers.deliver(hopRelayFrame{Version: hopRelayVersion, Seq: 1, Response: json.RawMessage(`{"version":2,"accepted":true,"error":"stale"}`)})
	answers.deliver(hopRelayFrame{Version: hopRelayVersion, Seq: 2, Response: json.RawMessage(`{"version":2,"accepted":false,"error":"current"}`)})
	var response hopResumeResponse
	if err := json.NewDecoder(far).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "current" {
		t.Fatalf("the far shell received another exchange's answer: %+v", response)
	}
	_ = far.Close()
	if err := <-done; err != nil {
		t.Fatalf("exchange: %v", err)
	}
}

// The opening shell closing the relay's input is how the relay learns its hop
// is over. An answer that arrives after its exchange was abandoned must not
// keep the relay from noticing.
func TestHopRelayEndsWithItsHopAfterALateAnswer(t *testing.T) {
	conns := make(chan net.Conn)
	ln := &pipeListener{conns: conns, closed: make(chan struct{})}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	defer func() { _ = outW.Close() }()
	frames := make(chan hopRelayFrame, 4)
	go func() {
		reader := bufio.NewReader(outR)
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				return
			}
			var frame hopRelayFrame
			if json.Unmarshal(line, &frame) == nil && frame.Version == hopRelayVersion {
				frames <- frame
			}
		}
	}()
	done := make(chan error, 1)
	go func() { done <- relayHopExchanges(context.Background(), ln, inR, outW) }()

	if frame := <-frames; !frame.Ready {
		t.Fatalf("a relay's first frame must say it is listening: %+v", frame)
	}
	// A far shell asks and is interrupted before it is answered.
	far, near := net.Pipe()
	conns <- near
	if _, err := far.Write([]byte(`{"version":2,"request":{"selector":"thirdbox:notes"}}` + "\n")); err != nil {
		t.Fatal(err)
	}
	if frame := <-frames; frame.Request == nil || frame.Seq != 1 {
		t.Fatalf("request frame %+v", frame)
	}
	_ = far.Close()
	if frame := <-frames; !frame.Gone || frame.Seq != 1 {
		t.Fatalf("hang-up frame %+v", frame)
	}
	// The opening shell's answer was already on its way.
	late := hopLineWriter{w: inW}
	for range 3 {
		if err := late.write(hopRelayFrame{Version: hopRelayVersion, Seq: 1, Response: json.RawMessage(`{"version":2,"accepted":true}`)}); err != nil {
			t.Fatal(err)
		}
	}
	_ = inW.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("relay: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the relay outlived its hop: a late answer kept it from seeing its input end")
	}
}

// The relay link can die between an acknowledgement and its confirmation.
// Whether the far shell acted on the acknowledgement shows in the hop itself:
// it exits at once if it did.
func TestHopRelayUnconfirmedSwitchCountsOnlyIfTheHopEnds(t *testing.T) {
	grace := hopAckGrace
	t.Cleanup(func() { hopAckGrace = grace })
	hopAckGrace = 150 * time.Millisecond

	for name, tc := range map[string]struct {
		hopEnds bool
		want    string
	}{
		"the far shell got its acknowledgement and exited": {true, "ssh thirdbox:notes"},
		"the far shell never got it and stayed":            {false, ""},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, endHop := context.WithCancel(context.Background())
			defer endHop()
			toOpenerR, toOpenerW := io.Pipe()
			toRelayR, toRelayW := io.Pipe()
			defer func() { _ = toRelayW.Close() }()
			hop := new(hopSwitch)
			follow := make(chan string, 1)
			go func() {
				line, _ := serveHopRelay(ctx, toOpenerR, toRelayW, hop, prepareBySelector)
				follow <- line
			}()
			request, err := json.Marshal(hopResumeEnvelope{Version: hopRelayVersion, Request: hopResumeRequest{Selector: "thirdbox:notes", Host: "c.example"}})
			if err != nil {
				t.Fatal(err)
			}
			out := hopLineWriter{w: toOpenerW}
			if err := out.write(hopRelayFrame{Version: hopRelayVersion, Seq: 1, Request: request}); err != nil {
				t.Fatal(err)
			}
			var answer hopRelayFrame
			if err := json.NewDecoder(toRelayR).Decode(&answer); err != nil || answer.Response == nil {
				t.Fatalf("acknowledgement %+v %v", answer, err)
			}
			// The relay link dies before any confirmation.
			_ = toOpenerW.Close()
			if tc.hopEnds {
				endHop()
			}
			select {
			case line := <-follow:
				if line != tc.want {
					t.Fatalf("follow-up %q, want %q", line, tc.want)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("serving did not settle")
			}
			if !tc.hopEnds && !hop.reserve() {
				t.Fatal("a switch the far shell never acted on left the hop reserved")
			}
		})
	}
}

func TestHopRelayIgnoresFramesNoRelayWouldNumber(t *testing.T) {
	toOpenerR, toOpenerW := io.Pipe()
	toRelayR, toRelayW := io.Pipe()
	defer func() { _ = toOpenerW.Close(); _ = toRelayW.Close() }()
	answered := make(chan hopRelayFrame, 1)
	go func() {
		var frame hopRelayFrame
		if json.NewDecoder(toRelayR).Decode(&frame) == nil {
			answered <- frame
		}
	}()
	hop := new(hopSwitch)
	go func() { _, _ = serveHopRelay(context.Background(), toOpenerR, toRelayW, hop, prepareBySelector) }()
	request, err := json.Marshal(hopResumeEnvelope{Version: hopRelayVersion, Request: hopResumeRequest{Selector: "thirdbox:notes", Host: "c.example"}})
	if err != nil {
		t.Fatal(err)
	}
	out := hopLineWriter{w: toOpenerW}
	for _, seq := range []int{0, -3} {
		if err := out.write(hopRelayFrame{Version: hopRelayVersion, Seq: seq, Request: request}); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case frame := <-answered:
		t.Fatalf("a frame with no exchange number was acted on: %+v", frame)
	case <-time.After(100 * time.Millisecond):
	}
	if !hop.reserve() {
		t.Fatal("a frame with no exchange number reserved the hop")
	}
}

// Both routes of one hop answer to the same reservation.
func TestHopResumeSocketRouteRefusesWhileTheRelayHoldsTheSwitch(t *testing.T) {
	conns := make(chan net.Conn)
	ln := &pipeListener{conns: conns, closed: make(chan struct{})}
	defer func() { _ = ln.Close() }()
	hop := new(hopSwitch)
	if !hop.reserve() {
		t.Fatal("fresh hop already reserved")
	}
	go func() {
		_, _ = serveHopResume(context.Background(), ln, hop, func(context.Context, hopResumeRequest) (string, error) {
			t.Error("a second destination reached preparation")
			return "", nil
		})
	}()
	far, near := net.Pipe()
	defer func() { _ = far.Close() }()
	_ = far.SetDeadline(time.Now().Add(3 * time.Second))
	conns <- near
	envelope := hopResumeEnvelope{Version: 1, Request: hopResumeRequest{Selector: "thirdbox:notes", Host: "c.example"}}
	if err := json.NewEncoder(far).Encode(envelope); err != nil {
		t.Fatal(err)
	}
	var response hopResumeResponse
	if err := json.NewDecoder(far).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Accepted || !strings.Contains(response.Error, "already switching") {
		t.Fatalf("the socket route must refuse while the hop is switching: %+v", response)
	}
	if hop.reserve() {
		t.Fatal("a refused request released a reservation it did not hold")
	}
}

func TestHopTextFromTheOtherSideCannotDriveTheTerminal(t *testing.T) {
	if got := printableHopText("no route\x1b[2J\x07 to \u009bhost\r\n"); strings.ContainsAny(got, "\x1b\x07\u009b\r\n") || !strings.Contains(got, "no route") {
		t.Fatalf("control characters survived: %q", got)
	}
	far, near := net.Pipe()
	defer func() { _ = far.Close(); _ = near.Close() }()
	_ = far.SetDeadline(time.Now().Add(3 * time.Second))
	go func() {
		reader := bufio.NewReader(near)
		_, _ = reader.ReadBytes('\n')
		_, _ = near.Write([]byte(`{"version":2,"accepted":false,"error":"\u001b]0;owned\u0007refused"}` + "\n"))
	}()
	err := sendOverHopRelay(far, hopResumeRequest{Selector: "thirdbox:notes", Host: "c.example"})
	if err == nil || strings.ContainsAny(err.Error(), "\x1b\x07") || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("an error from the other side must be shown without its control characters: %q", err)
	}
}

func TestHopRelayIDIsOnlyEverAFileName(t *testing.T) {
	t.Setenv("HOME", "/home/alex")
	path, err := hopRelaySocketPath("0123456789abcdef")
	if err != nil || path != "/home/alex/.obey/hop-resume/0123456789abcdef.sock" {
		t.Fatalf("relay socket path: %q %v", path, err)
	}
	// A home directory so deep that no socket fits beneath it: said, rather
	// than left to fail as a relay that never starts.
	longest := "/home/" + strings.Repeat("a", hopSocketPathMax-len("/home//.obey/hop-resume/0123456789abcdef.sock")-1)
	t.Setenv("HOME", longest)
	if path, err := hopRelaySocketPath("0123456789abcdef"); err != nil || len(path) != hopSocketPathMax-1 {
		t.Fatalf("the longest path a socket can have must be allowed: %d %v", len(path), err)
	}
	t.Setenv("HOME", longest+"a")
	if _, err := hopRelaySocketPath("0123456789abcdef"); err == nil || !strings.Contains(err.Error(), "too long a path") {
		t.Fatalf("a path no socket can have must be refused with the reason: %v", err)
	}
	t.Setenv("HOME", "/home/alex")
	for _, id := range []string{"", "../../tmp/evil", "0123456789abcde", "0123456789abcdef00", "0123456789abcdeg", "/tmp/0123456789abcdef"} {
		if path, err := hopRelaySocketPath(id); err == nil {
			t.Errorf("id %q must be refused, got %q", id, path)
		}
	}
	id, err := newHopRelayID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hopRelaySocketPath(id); err != nil {
		t.Fatalf("a fresh id must be usable: %q %v", id, err)
	}
}

func TestHopRelayAnswersARequestItCannotServe(t *testing.T) {
	rig := newHopRelayRig(t, func(context.Context, hopResumeRequest) (string, error) {
		t.Error("an unserviceable request reached preparation")
		return "", nil
	})
	// A request longer than any frame can carry must fail at once, not leave
	// the far shell waiting out its deadline.
	conn := rig.dial()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	long := `{"version":2,"request":{"selector":"` + strings.Repeat("x", 2*hopRelayMaxRequest) + `"}}`
	go func() { _, _ = conn.Write([]byte(long + "\n")) }()
	if _, err := conn.Read(make([]byte, 1)); err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("an over-long request must be turned away at once: %v", err)
	}
	_ = conn.Close()
}

func TestHopRelayOpenerAnswersAnotherVersion(t *testing.T) {
	toOpenerR, toOpenerW := io.Pipe()
	toRelayR, toRelayW := io.Pipe()
	defer func() { _ = toOpenerW.Close(); _ = toRelayW.Close() }()
	hop := new(hopSwitch)
	go func() { _, _ = serveHopRelay(context.Background(), toOpenerR, toRelayW, hop, prepareBySelector) }()
	out := hopLineWriter{w: toOpenerW}
	if err := out.write(hopRelayFrame{Version: hopRelayVersion + 1, Seq: 4, Request: json.RawMessage(`{"version":3,"request":{"selector":"thirdbox:notes"}}`)}); err != nil {
		t.Fatal(err)
	}
	answered := make(chan hopRelayFrame, 1)
	go func() {
		var frame hopRelayFrame
		if json.NewDecoder(toRelayR).Decode(&frame) == nil {
			answered <- frame
		}
	}()
	select {
	case frame := <-answered:
		var response hopResumeResponse
		if frame.Seq != 4 || json.Unmarshal(frame.Response, &response) != nil || response.Accepted || !strings.Contains(response.Error, "unsupported request version") {
			t.Fatalf("answer %+v %+v", frame, response)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a request in another version was dropped, leaving the far shell to wait out its deadline")
	}
	if !hop.reserve() {
		t.Fatal("an unsupported request reserved the hop")
	}
}

func TestHopResumeRefusesRequestsThatCarryControlCharacters(t *testing.T) {
	for name, request := range map[string]hopResumeRequest{
		"in the org":      {Selector: "thirdbox:notes", Host: "c.example", Scope: cmdutil.CampaignScope{Org: "work\x1b[2J"}},
		"in the status":   {Selector: "thirdbox:notes", Host: "c.example", Scope: cmdutil.CampaignScope{Status: "act\x07ive"}},
		"in the selector": {Selector: "thirdbox:no\x1btes", Host: "c.example"},
		"in the host":     {Selector: "thirdbox:notes", Host: "c.exa\u009bmple"},
	} {
		if _, err := prepareHopResume(context.Background(), request); err == nil || !strings.Contains(err.Error(), "control characters") {
			t.Errorf("%s: the request must be refused before it reaches this shell's own messages: %v", name, err)
		}
	}
	if err := printableHopRequest(hopResumeRequest{Selector: "thirdbox:Festival Method Kit", Host: "c.example", Scope: cmdutil.CampaignScope{Org: "r&d"}}); err != nil {
		t.Fatalf("an ordinary request must pass: %v", err)
	}
}

// While its link is up the relay always reports how an exchange ended, so a
// slow link must not cost a switch the far shell is still completing.
func TestHopRelayLateConfirmationStillCounts(t *testing.T) {
	grace := hopAckGrace
	t.Cleanup(func() { hopAckGrace = grace })
	hopAckGrace = 20 * time.Millisecond

	ctx, endHop := context.WithCancel(context.Background())
	defer endHop()
	toOpenerR, toOpenerW := io.Pipe()
	toRelayR, toRelayW := io.Pipe()
	defer func() { _ = toOpenerW.Close(); _ = toRelayW.Close() }()
	hop := new(hopSwitch)
	follow := make(chan string, 1)
	go func() {
		line, _ := serveHopRelay(ctx, toOpenerR, toRelayW, hop, prepareBySelector)
		follow <- line
	}()
	request, err := json.Marshal(hopResumeEnvelope{Version: hopRelayVersion, Request: hopResumeRequest{Selector: "thirdbox:notes", Host: "c.example"}})
	if err != nil {
		t.Fatal(err)
	}
	out := hopLineWriter{w: toOpenerW}
	if err := out.write(hopRelayFrame{Version: hopRelayVersion, Seq: 1, Request: request}); err != nil {
		t.Fatal(err)
	}
	var answer hopRelayFrame
	if err := json.NewDecoder(toRelayR).Decode(&answer); err != nil || answer.Response == nil {
		t.Fatalf("acknowledgement %+v %v", answer, err)
	}
	// The acknowledgement is slow to reach the far shell; its confirmation
	// arrives long after the grace that applies to a lost link.
	time.Sleep(10 * hopAckGrace)
	if hop.reserve() {
		t.Fatal("the switch was taken back while its relay link was still up")
	}
	if err := out.write(hopRelayFrame{Version: hopRelayVersion, Seq: 1, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	// Long after that, the far shell is gone and the hop ends.
	time.Sleep(10 * hopAckGrace)
	endHop()
	select {
	case line := <-follow:
		if line != "ssh thirdbox:notes" {
			t.Fatalf("a confirmed switch was dropped: %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serving did not end with the hop")
	}
}

func TestHopRelayTellsALostLinkFromARelayThatNeverListened(t *testing.T) {
	for name, tc := range map[string]struct {
		frames []hopRelayFrame
		lost   bool
	}{
		"listening, then gone":                   {[]hopRelayFrame{{Version: hopRelayVersion, Ready: true}}, true},
		"never listened (stepped aside, or old)": {nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			toOpenerR, toOpenerW := io.Pipe()
			_, toRelayW := io.Pipe()
			defer func() { _ = toRelayW.Close() }()
			done := make(chan error, 1)
			go func() {
				_, err := serveHopRelay(context.Background(), toOpenerR, toRelayW, new(hopSwitch), prepareBySelector)
				done <- err
			}()
			out := hopLineWriter{w: toOpenerW}
			for _, frame := range tc.frames {
				if err := out.write(frame); err != nil {
					t.Fatal(err)
				}
			}
			_ = toOpenerW.Close()
			select {
			case err := <-done:
				if errors.Is(err, errHopRelayLost) != tc.lost {
					t.Fatalf("lost=%v, want %v (%v)", errors.Is(err, errHopRelayLost), tc.lost, err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("serving did not end with the relay link")
			}
		})
	}
}

// A far process that asks and hangs up over and over must not make the
// opening shell run more than one preparation at a time.
func TestHopRelayRunsOnePreparationAtATime(t *testing.T) {
	var mu sync.Mutex
	running, most := 0, 0
	release := make(chan struct{})
	rig := newHopRelayRig(t, func(_ context.Context, request hopResumeRequest) (string, error) {
		mu.Lock()
		running++
		most = max(most, running)
		mu.Unlock()
		defer func() {
			mu.Lock()
			running--
			mu.Unlock()
		}()
		if request.Selector == "stubborn:notes" {
			// A preparation that takes its time to notice it was cancelled.
			<-release
			return "", errors.New("cancelled")
		}
		return "ssh " + request.Selector, nil
	})
	for range 3 {
		conn := rig.dial()
		out := hopLineWriter{w: conn}
		if err := out.write(hopResumeEnvelope{Version: hopRelayVersion, Request: hopResumeRequest{Selector: "stubborn:notes", Host: "c.example"}}); err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
	}
	time.AfterFunc(100*time.Millisecond, func() { close(release) })
	if err := rig.send("thirdbox:notes"); err != nil {
		t.Fatalf("the request after the abandoned ones must be served: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if most != 1 {
		t.Fatalf("%d preparations ran at once", most)
	}
}

// A request whose names need escaping in HTML must not grow past what a frame
// carries on its way through the relay.
func TestHopRelayCarriesARequestAtItsFullLength(t *testing.T) {
	rig := newHopRelayRig(t, prepareBySelector)
	selector := "thirdbox:" + strings.Repeat("<&>", (hopRelayMaxRequest-200)/3)
	if err := rig.send(selector); err != nil {
		t.Fatalf("a request within the limit must be carried: %v", err)
	}
	rig.endsWith("ssh " + selector)
}

// A far process that asks and is refused, or hangs up, over and over must not
// start preparations on the opening machine as fast as it can ask.
func TestHopRelaySpacesPreparationsAfterAnExchangeThatDidNotSwitch(t *testing.T) {
	var mu sync.Mutex
	var started []time.Time
	const spacing = 300 * time.Millisecond
	rig := newSpacedHopRelayRig(t, spacing, func(ctx context.Context, request hopResumeRequest) (string, error) {
		mu.Lock()
		started = append(started, time.Now())
		mu.Unlock()
		return prepareBySelector(ctx, request)
	})
	if err := rig.send("nowhere:notes"); err == nil {
		t.Fatal("the first request must be refused")
	}
	refused := time.Now()
	if err := rig.send("thirdbox:notes"); err != nil {
		t.Fatalf("the request after a refused one must be served: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(started) != 2 {
		t.Fatalf("%d preparations ran, want 2", len(started))
	}
	// Measured from when the far shell had its refusal, which is after the
	// opening shell ended that exchange.
	if gap := started[1].Sub(refused); gap < spacing-50*time.Millisecond {
		t.Fatalf("the next preparation started %v after a refused one, want at least %v", gap, spacing)
	}
}

// The far shell acts on having sent its confirmation. One that arrives as the
// relay's wait for it runs out must not be reported as a hang-up.
func TestHopRelayLineThatArrivesWithTheDeadlineStillCounts(t *testing.T) {
	passed := make(chan time.Time, 1)
	for range 200 {
		// Both the line and the deadline are there before the wait looks.
		next := make(chan []byte, 1)
		next <- []byte("confirmation")
		passed <- time.Now()
		line, ok := awaitHopLine(context.Background(), next, passed)
		if !ok || string(line) != "confirmation" {
			t.Fatalf("a line that was already there was dropped: %q ok=%v", line, ok)
		}
		select {
		case <-passed:
		default:
		}
	}
	passed <- time.Now()
	if line, ok := awaitHopLine(context.Background(), make(chan []byte), passed); line != nil || !ok {
		t.Fatalf("no line by the deadline must be reported as none: %q ok=%v", line, ok)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := awaitHopLine(ctx, make(chan []byte), nil); ok {
		t.Fatal("the end of the hop must end the wait")
	}
}

// A request held back by the spacing is still only a request: if its sender
// hangs up meanwhile, nothing is prepared for it.
func TestHopRelayPreparesNothingForASenderGoneDuringTheSpacing(t *testing.T) {
	var mu sync.Mutex
	var prepared []string
	const spacing = 300 * time.Millisecond
	rig := newSpacedHopRelayRig(t, spacing, func(ctx context.Context, request hopResumeRequest) (string, error) {
		mu.Lock()
		prepared = append(prepared, request.Selector)
		mu.Unlock()
		return prepareBySelector(ctx, request)
	})
	if err := rig.send("nowhere:notes"); err == nil {
		t.Fatal("the first request must be refused")
	}
	conn := rig.dial()
	out := hopLineWriter{w: conn}
	if err := out.write(hopResumeEnvelope{Version: hopRelayVersion, Request: hopResumeRequest{Selector: "gone:notes", Host: "c.example"}}); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	time.Sleep(2 * spacing)
	if err := rig.send("thirdbox:notes"); err != nil {
		t.Fatalf("the request after one that was withdrawn must be served: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := []string{"nowhere:notes", "thirdbox:notes"}; !slices.Equal(prepared, want) {
		t.Fatalf("prepared %q, want %q", prepared, want)
	}
}

// A switch the far shell confirmed stands when the relay can no longer be
// written to, as when its link breaks while a later request is being refused.
func TestHopRelayConfirmedSwitchSurvivesALinkItCannotAnswer(t *testing.T) {
	toOpenerR, toOpenerW := io.Pipe()
	toRelayR, toRelayW := io.Pipe()
	defer func() { _ = toOpenerW.Close(); _ = toRelayW.Close() }()
	type served struct {
		follow string
		err    error
	}
	done := make(chan served, 1)
	go func() {
		follow, err := serveHopRelay(context.Background(), toOpenerR, toRelayW, new(hopSwitch), prepareBySelector)
		done <- served{follow, err}
	}()
	request := func(selector string) json.RawMessage {
		raw, err := json.Marshal(hopResumeEnvelope{Version: hopRelayVersion, Request: hopResumeRequest{Selector: selector, Host: "c.example"}})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	out := hopLineWriter{w: toOpenerW}
	if err := out.write(hopRelayFrame{Version: hopRelayVersion, Seq: 1, Request: request("thirdbox:notes")}); err != nil {
		t.Fatal(err)
	}
	var answer hopRelayFrame
	if err := json.NewDecoder(toRelayR).Decode(&answer); err != nil || answer.Response == nil {
		t.Fatalf("acknowledgement %+v %v", answer, err)
	}
	if err := out.write(hopRelayFrame{Version: hopRelayVersion, Seq: 1, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	// The far shell asks again while the link is going: the refusal cannot be
	// written.
	_ = toRelayR.Close()
	if err := out.write(hopRelayFrame{Version: hopRelayVersion, Seq: 2, Request: request("fourthbox:notes")}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.follow != "ssh thirdbox:notes" || got.err != nil {
			t.Fatalf("a confirmed switch was dropped: %q %v", got.follow, got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serving did not end with the link")
	}
}

// A cancelled preparation that will not end must not hold the next request
// until the far shell gives up on it without a word.
func TestHopRelayAnswersARequestHeldBehindAPreparationThatWillNotEnd(t *testing.T) {
	preparing := make(chan struct{}, 1)
	release := make(chan struct{})
	defer close(release)
	rig := newHopRelayRig(t, func(_ context.Context, request hopResumeRequest) (string, error) {
		if request.Selector != "stubborn:notes" {
			return "ssh " + request.Selector, nil
		}
		preparing <- struct{}{}
		<-release
		return "", errors.New("cancelled")
	})
	conn := rig.dial()
	out := hopLineWriter{w: conn}
	if err := out.write(hopResumeEnvelope{Version: hopRelayVersion, Request: hopResumeRequest{Selector: "stubborn:notes", Host: "c.example"}}); err != nil {
		t.Fatal(err)
	}
	<-preparing
	_ = conn.Close()

	next := rig.dial()
	_ = next.SetDeadline(time.Now().Add(hopRelayRequestWait + 3*time.Second))
	asked := time.Now()
	err := sendOverHopRelay(next, hopResumeRequest{Selector: "thirdbox:notes", Host: "c.example"})
	if err == nil || !strings.Contains(err.Error(), "still being cancelled; try again") {
		t.Fatalf("the far shell must be told why it is not being served: %v", err)
	}
	if waited := time.Since(asked); waited < hopRelayRequestWait-time.Second {
		t.Fatalf("the request was given up on after %v, before the cancelled preparation had its time", waited)
	}
	rig.freed()
}
