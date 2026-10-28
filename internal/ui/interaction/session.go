// Package interaction presents CLI-owned questions to an external UI without
// moving workflow decisions or filesystem operations into that UI.
package interaction

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/Obedience-Corp/camp/internal/ui/theme"
	"github.com/charmbracelet/huh"
)

const Version = 1

type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type Prompt struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Options     []Option `json:"options"`
	Default     string   `json:"default"`
}

type Event struct {
	Version int     `json:"version"`
	Type    string  `json:"type"`
	Prompt  *Prompt `json:"prompt,omitempty"`
	Text    string  `json:"text,omitempty"`
	Error   string  `json:"error,omitempty"`
}

type Response struct {
	ID     string `json:"id"`
	Value  string `json:"value"`
	Cancel bool   `json:"cancel,omitempty"`
}

type contextKey struct{}

type Session struct {
	mu        sync.Mutex
	encoder   *json.Encoder
	responses chan Response
	cancel    context.CancelFunc
	next      uint64
}

func New(ctx context.Context, input io.Reader, output io.Writer) (context.Context, *Session) {
	ctx, cancel := context.WithCancel(ctx)
	s := &Session{encoder: json.NewEncoder(output), responses: make(chan Response), cancel: cancel}
	go func() {
		defer cancel()
		scanner := bufio.NewScanner(input)
		scanner.Buffer(make([]byte, 4096), 64*1024)
		for scanner.Scan() {
			var answer Response
			if err := json.Unmarshal(scanner.Bytes(), &answer); err != nil {
				_ = s.Emit(Event{Type: "input_error", Error: "Expected a JSON prompt response"})
				continue
			}
			select {
			case s.responses <- answer:
			case <-ctx.Done():
				return
			}
		}
	}()
	return context.WithValue(ctx, contextKey{}, s), s
}

func Available(ctx context.Context) bool { return ctx.Value(contextKey{}) != nil }
func (s *Session) Close()                { s.cancel() }
func (s *Session) Emit(event Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	event.Version = Version
	if err := s.encoder.Encode(event); err != nil {
		s.cancel()
		return err
	}
	return nil
}

func (s *Session) Write(data []byte) (int, error) {
	err := s.Emit(Event{Type: "output", Text: string(data)})
	if err != nil {
		return 0, err
	}
	return len(data), nil
}

// Choose uses the very same question, options and default in a terminal and
// in the native app. Only presentation changes; callers still apply answers.
func Choose(ctx context.Context, prompt Prompt) (string, error) {
	if s, ok := ctx.Value(contextKey{}).(*Session); ok {
		s.next++
		prompt.ID = fmt.Sprintf("prompt-%d", s.next)
		if err := s.Emit(Event{Type: "prompt", Prompt: &prompt}); err != nil {
			return "", err
		}
		for {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case answer := <-s.responses:
				if answer.ID != prompt.ID {
					_ = s.Emit(Event{Type: "input_error", Error: "That question is no longer active"})
					continue
				}
				if answer.Cancel {
					return "", huh.ErrUserAborted
				}
				for _, option := range prompt.Options {
					if answer.Value == option.Value {
						return answer.Value, nil
					}
				}
				_ = s.Emit(Event{Type: "input_error", Error: "Choose one of the offered answers"})
			}
		}
	}
	value := prompt.Default
	options := make([]huh.Option[string], 0, len(prompt.Options))
	for _, option := range prompt.Options {
		options = append(options, huh.NewOption(option.Label, option.Value))
	}
	err := theme.RunForm(ctx, huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title(prompt.Title).Description(prompt.Description).Options(options...).Value(&value))))
	return value, err
}

// Confirm retains the CLI's yes/no keys and cancellation semantics.
func Confirm(ctx context.Context, title, description, yes, no string) (bool, error) {
	if Available(ctx) {
		value, err := Choose(ctx, Prompt{Title: title, Description: description, Default: "no", Options: []Option{{"yes", yes}, {"no", no}}})
		return value == "yes", err
	}
	var value bool
	err := theme.RunForm(ctx, huh.NewForm(huh.NewGroup(huh.NewConfirm().Title(title).Description(description).Affirmative(yes).Negative(no).Value(&value))))
	return value, err
}

func Cancelled(err error) bool { return errors.Is(err, huh.ErrUserAborted) }
