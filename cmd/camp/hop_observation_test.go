package main

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
)

func TestHopObservationValidation(t *testing.T) {
	good := hopObservation{"0123456789abcdef", 1, "camp-id", "/camp", "/camp/projects"}
	if err := good.validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*hopObservation){
		func(r *hopObservation) { r.Sequence = 0 }, func(r *hopObservation) { r.Sequence = maxHopSequence },
		func(r *hopObservation) { r.CampID = "other:camp" }, func(r *hopObservation) { r.CWD = "relative" },
		func(r *hopObservation) { r.Root = "/camp\x1b" }, func(r *hopObservation) { r.CWD = strings.Repeat("/", 4097) },
	} {
		r := good
		mutate(&r)
		if r.validate() == nil {
			t.Fatalf("accepted invalid report: %+v", r)
		}
	}
}
func TestHopObservationDoesNotBecomeAnOnwardSelector(t *testing.T) {
	report := &hopObservation{"0123456789abcdef", 1, "camp-id", "/camp", "/camp"}
	raw, err := json.Marshal(hopResumeEnvelope{Version: hopRelayVersion, Request: hopResumeRequest{Observation: report}})
	if err != nil {
		t.Fatal(err)
	}
	request, err := hopRelayRequest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if request.Observation == nil || request.Selector != "" {
		t.Fatalf("wrong report: %+v", request)
	}
	if _, err := prepareHopResume(context.Background(), request); err == nil {
		t.Fatal("unregistered parent accepted report")
	}
	server, client := net.Pipe()
	defer func() { _ = server.Close() }()
	defer func() { _ = client.Close() }()
	done := make(chan error, 1)
	go func() {
		_, err := acceptHopResume(context.Background(), server, func(_ context.Context, r hopResumeRequest) (string, error) {
			if r.Observation.HopID != report.HopID {
				t.Error("scope changed")
			}
			return "", nil
		})
		done <- err
	}()
	raw, err = json.Marshal(hopResumeEnvelope{Version: 1, Request: request})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Write(append(raw, '\n')); err != nil {
		t.Fatal(err)
	}
	var response hopResumeResponse
	if err := json.NewDecoder(client).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if !response.Accepted {
		t.Fatalf("rejected: %+v", response)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestHopObservationMachineComesFromLocalCommand(t *testing.T) {
	for _, tt := range []struct{ line, want string }{
		{"export 'CAMP_HOP_MACHINE_ID=lance-arch' && cd '/camp' && exec $SHELL -l", "lance-arch"},
		{"export CAMP_HOP_ORIGIN='{}' && cd '/camp' && exec $SHELL -l", ""},
		{"cd '/camp' && exec $SHELL -l", ""},
	} {
		if got := (hopLine{remoteCmd: tt.line}).machineID(); got != tt.want {
			t.Fatalf("machineID=%q want=%q", got, tt.want)
		}
	}
}

func TestHopObservationsLeaveRelayAvailableForOnwardSwitch(t *testing.T) {
	rig := newHopRelayRig(t, func(_ context.Context, r hopResumeRequest) (string, error) {
		if r.Observation != nil {
			return "", r.Observation.validate()
		}
		return "ssh " + r.Selector, nil
	})
	for seq := uint64(1); seq < 4; seq++ {
		conn := rig.dial()
		if err := sendOverHopRelay(conn, hopResumeRequest{Observation: &hopObservation{"0123456789abcdef", seq, "camp", "/camp", "/camp/projects"}}); err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
		rig.freed()
	}
	if err := rig.send("next:camp"); err != nil {
		t.Fatal(err)
	}
	rig.endsWith("ssh next:camp")
}
