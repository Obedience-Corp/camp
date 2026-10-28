package interaction

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"
)

func TestQuestionResponses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	input, answers := io.Pipe()
	events, output := io.Pipe()
	ctx, session := New(ctx, input, output)
	t.Cleanup(func() {
		session.Close()
		_ = input.Close()
		_ = answers.Close()
		_ = output.Close()
		_ = events.Close()
	})
	result := make(chan string, 1)
	go func() {
		value, err := Choose(ctx, Prompt{Title: "Where?", Default: "keep", Options: []Option{{Value: "keep", Label: "Keep active"}, {Value: "docs", Label: "Route to docs"}}})
		if err != nil {
			result <- err.Error()
			return
		}
		result <- value
	}()
	decoder := json.NewDecoder(events)
	var event Event
	if err := decoder.Decode(&event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "prompt" || event.Version != 1 || event.Prompt.Default != "keep" {
		t.Fatalf("unexpected prompt: %+v", event)
	}
	id := event.Prompt.ID
	encoder := json.NewEncoder(answers)
	for _, response := range []Response{{ID: "stale", Value: "docs"}, {ID: id, Value: "arbitrary-command"}} {
		if err := encoder.Encode(response); err != nil {
			t.Fatal(err)
		}
		if err := decoder.Decode(&event); err != nil {
			t.Fatal(err)
		}
		if event.Type != "input_error" {
			t.Fatalf("invalid answer was accepted: %+v", event)
		}
	}
	if err := encoder.Encode(Response{ID: id, Value: "docs"}); err != nil {
		t.Fatal(err)
	}
	if value := <-result; value != "docs" {
		t.Fatalf("got %q", value)
	}
}

func TestQuestionCancelAndDisconnect(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "disconnect"}[disconnect], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			input, answers := io.Pipe()
			events, output := io.Pipe()
			ctx, session := New(ctx, input, output)
			defer session.Close()
			defer func() { _ = input.Close() }()
			defer func() { _ = answers.Close() }()
			defer func() { _ = events.Close() }()
			defer func() { _ = output.Close() }()
			result := make(chan error, 1)
			go func() {
				_, err := Choose(ctx, Prompt{Title: "Choose", Options: []Option{{Value: "yes", Label: "Yes"}}})
				result <- err
			}()
			var event Event
			if err := json.NewDecoder(events).Decode(&event); err != nil {
				t.Fatal(err)
			}
			if disconnect {
				_ = answers.Close()
			} else if err := json.NewEncoder(answers).Encode(Response{ID: event.Prompt.ID, Cancel: true}); err != nil {
				t.Fatal(err)
			}
			err := <-result
			if disconnect && err != context.Canceled {
				t.Fatalf("disconnect: %v", err)
			}
			if !disconnect && !Cancelled(err) {
				t.Fatalf("cancel: %v", err)
			}
		})
	}
}
