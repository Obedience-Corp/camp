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
	rig := &hopRelayRig{t: t, conns: make(chan net.Conn), hop: new(hopSwitch), follow: make(chan string, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	toOpenerR, toOpenerW := io.Pipe()
	toRelayR, toRelayW := io.Pipe()
	ln := &pipeListener{conns: rig.conns, closed: make(chan struct{})}
	t.Cleanup(func() {
		cancel()
		_ = toRelayW.Close()
		_ = toOpenerW.Close()
	})
	go func() { _ = relayHopExchanges(ctx, ln, toRelayR, toOpenerW) }()
	go func() {
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

func (r *hopRelayRig) queued(want string) {
	r.t.Helper()
	select {
	case line := <-r.follow:
		if line != want {
			r.t.Fatalf("follow-up %q, want %q", line, want)
		}
	case <-time.After(2 * time.Second):
		r.t.Fatalf("no follow-up queued, want %q", want)
	}
}

func (r *hopRelayRig) nothingQueued() {
	r.t.Helper()
	select {
	case line := <-r.follow:
		r.t.Fatalf("a switch the far shell never confirmed was queued: %q", line)
	case <-time.After(100 * time.Millisecond):
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
	rig.queued("ssh thirdbox:notes")
}

func TestHopRelayRejectedRequestLeavesTheHopFree(t *testing.T) {
	rig := newHopRelayRig(t, prepareBySelector)
	err := rig.send("nowhere:notes")
	if err == nil || !strings.Contains(err.Error(), "parent route unavailable") {
		t.Fatalf("the far shell must hear why its route was refused: %v", err)
	}
	rig.freed()
	rig.nothingQueued()
	if err := rig.send("thirdbox:notes"); err != nil {
		t.Fatalf("a rejected request must not disable onward switching: %v", err)
	}
	rig.queued("ssh thirdbox:notes")
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
		t.Fatalf("the retry must be served at once: %v", err)
	}
	rig.queued("ssh thirdbox:notes")
}

func TestHopRelayUnconfirmedAcknowledgementQueuesNothing(t *testing.T) {
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
	rig.nothingQueued()
	if err := rig.send("second:notes"); err != nil {
		t.Fatalf("the retry must be accepted: %v", err)
	}
	rig.queued("ssh second:notes")
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
	rig.nothingQueued()
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
	// A confirmation for another exchange must not commit this one.
	if err := out.write(hopRelayFrame{Version: hopRelayVersion, Seq: 7, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-follow:
		t.Fatalf("a confirmation for another exchange queued %q", line)
	case <-time.After(100 * time.Millisecond):
	}
	if err := out.write(hopRelayFrame{Version: hopRelayVersion, Seq: 1, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-follow:
		if line != "ssh thirdbox:notes" {
			t.Fatalf("follow-up %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the confirmed switch was not queued")
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

// The far shell exits on its acknowledgement. Its confirmation travels a
// separate way and may never arrive.
func TestHopRelayAcknowledgedSwitchSurvivesALostLink(t *testing.T) {
	toOpenerR, toOpenerW := io.Pipe()
	toRelayR, toRelayW := io.Pipe()
	defer func() { _ = toRelayW.Close() }()
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
	if err := out.write(hopRelayFrame{Version: hopRelayVersion, Seq: 1, Request: request}); err != nil {
		t.Fatal(err)
	}
	var answer hopRelayFrame
	if err := json.NewDecoder(toRelayR).Decode(&answer); err != nil || answer.Response == nil {
		t.Fatalf("acknowledgement %+v %v", answer, err)
	}
	// The relay link dies before any confirmation.
	_ = toOpenerW.Close()
	select {
	case line := <-follow:
		if line != "ssh thirdbox:notes" {
			t.Fatalf("an acknowledged switch was lost with its relay link: %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serving did not end with the relay link")
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
