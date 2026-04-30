package crawl

import (
	"context"
	"errors"

	"github.com/charmbracelet/huh"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

// Prompt drives the user-facing interactions of a crawl session.
//
// The interface is the test seam that lets domain packages drive a
// real terminal in production while injecting fake prompts in unit
// tests. All methods receive the current item so prompts can render
// item-specific titles and descriptions.
//
// Methods may return ErrAborted to signal that the user aborted
// the entire crawl session. Methods may return a nil target/value
// with a non-nil error only when the error is ErrAborted or a real
// failure; ordinary "back up" gestures are signalled by returning
// the empty value with a nil error.
type Prompt interface {
	// SelectAction presents the first-step menu for an item and
	// returns the chosen Action. The returned Action must be one of
	// the actions listed in options. ErrAborted means the user
	// aborted the crawl (ctrl+c).
	SelectAction(ctx context.Context, item Item, options []Option) (Action, error)

	// SelectDestination presents the second-step destination picker.
	// Returns the chosen Option. An empty Target with nil error
	// means the user backed out (esc) and wants to return to the
	// first-step menu for the same item. ErrAborted means abort.
	SelectDestination(ctx context.Context, item Item, options []Option) (Option, error)

	// Reason prompts for a free-text reason when the chosen option
	// has RequiresReason set. Returns the trimmed reason. Empty
	// reason with nil error means cancel; ErrAborted means abort.
	Reason(ctx context.Context, item Item, choice Option) (string, error)
}

// DefaultPrompt is the production prompt implementation. It uses
// huh for first-step selection and reason input and a bubbletea
// picker for destination selection.
type DefaultPrompt struct{}

// NewDefaultPrompt returns a ready-to-use production prompt.
func NewDefaultPrompt() DefaultPrompt {
	return DefaultPrompt{}
}

// SelectAction renders a huh select form built from options.
// The form's title is the item title; description is item.Description.
func (p DefaultPrompt) SelectAction(ctx context.Context, item Item, options []Option) (Action, error) {
	if len(options) == 0 {
		return "", camperrors.Wrap(camperrors.ErrInvalidInput, "no options provided")
	}

	huhOptions := make([]huh.Option[string], 0, len(options))
	for _, opt := range options {
		huhOptions = append(huhOptions, huh.NewOption(opt.Label, string(opt.Action)))
	}

	var chosen string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(item.Title).
				Description(item.Description).
				Options(huhOptions...).
				Value(&chosen),
		),
	)

	if err := theme.RunForm(ctx, form); err != nil {
		if theme.IsCancelled(err) {
			return "", ErrAborted
		}
		return "", camperrors.Wrap(err, "first-step prompt")
	}

	return Action(chosen), nil
}

// SelectDestination runs the generic destination picker.
func (p DefaultPrompt) SelectDestination(ctx context.Context, item Item, options []Option) (Option, error) {
	if len(options) == 0 {
		return Option{}, camperrors.Wrap(camperrors.ErrInvalidInput, "no destination options provided")
	}
	return runDestinationPicker(ctx, item, options)
}

// Reason renders a huh input form for a free-text reason.
// An empty input is treated as cancel (returns "" with nil error).
func (p DefaultPrompt) Reason(ctx context.Context, item Item, choice Option) (string, error) {
	var reason string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(reasonPromptTitle(item, choice)).
				Description("Provide a short reason. Leave empty to cancel.").
				Value(&reason),
		),
	)

	if err := theme.RunForm(ctx, form); err != nil {
		if theme.IsCancelled(err) {
			return "", ErrAborted
		}
		return "", camperrors.Wrap(err, "reason prompt")
	}

	return trimReason(reason), nil
}

func reasonPromptTitle(item Item, choice Option) string {
	if choice.Target == "" {
		return "Reason for moving " + item.Title
	}
	return "Reason for moving " + item.Title + " to " + choice.Target
}

func trimReason(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n') {
		s = s[:len(s)-1]
	}
	return s
}

// IsAborted reports whether err signals that the user aborted the
// crawl. It is a convenience wrapper around errors.Is(err, ErrAborted).
func IsAborted(err error) bool {
	return errors.Is(err, ErrAborted)
}
