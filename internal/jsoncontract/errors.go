package jsoncontract

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// ErrorEnvelope is the stable refusal shape for commands that support --json.
type ErrorEnvelope struct {
	SchemaVersion string       `json:"schema_version"`
	Error         ErrorPayload `json:"error"`
}

// ErrorPayload contains machine-readable error metadata.
type ErrorPayload struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Hint     string `json:"hint"`
	ExitCode int    `json:"exit_code"`
}

type hintedError struct {
	err  error
	hint string
}

func (e hintedError) Error() string { return e.err.Error() }
func (e hintedError) Unwrap() error { return e.err }

// WithHint attaches a recovery hint to an existing error without changing its
// type for errors.As / errors.Is.
func WithHint(err error, hint string) error {
	if err == nil {
		return nil
	}
	return hintedError{err: err, hint: hint}
}

// Args wraps a cobra positional validator so --json callers receive the same
// structured error envelope as RunE failures.
func Args(schemaVersion string, jsonRequested func() bool, validate cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		err := validate(cmd, args)
		if err == nil || !Requested(jsonRequested) {
			return err
		}
		return RenderError(cmd, schemaVersion, err)
	}
}

// RunE wraps a command implementation and renders returned errors as JSON when
// --json was requested.
func RunE(schemaVersion string, jsonRequested func() bool, run func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		err := run(cmd, args)
		if err == nil || !Requested(jsonRequested) {
			return err
		}
		return RenderError(cmd, schemaVersion, err)
	}
}

// FlagErrorFunc renders flag parse failures as JSON when the original argv
// included --json. Cobra raises these before Args/RunE, so this hook is the
// only chance to keep the machine-readable contract.
func FlagErrorFunc(schemaVersion string, jsonRequested func() bool) func(*cobra.Command, error) error {
	return func(cmd *cobra.Command, err error) error {
		if err == nil || !Requested(jsonRequested) {
			return err
		}
		return RenderError(cmd, schemaVersion, err)
	}
}

// Requested reports whether the current invocation asked for JSON output.
//
// The argv fallback exists so flag-parse errors (which fire before cobra
// has populated the bound bool flag) still render through the JSON
// envelope. The fallback must respect explicit boolean values on the
// flag: `--json=false`, `--json=0`, `--json=no`, `--json=off`, and the
// uppercase variants all mean "do NOT render JSON", and must be
// honored even though argv contains a `--json=` token.
func Requested(jsonRequested func() bool) bool {
	if jsonRequested != nil && jsonRequested() {
		return true
	}
	for _, arg := range os.Args[1:] {
		if arg == "--json" {
			return true
		}
		if rest, ok := strings.CutPrefix(arg, "--json="); ok {
			// Parse the explicit value. Anything strconv.ParseBool
			// accepts is honored; an unparseable value falls back to
			// "treat as enabled" to preserve the prior behavior for
			// values like `--json=pretty` that may be added later.
			if v, err := strconv.ParseBool(rest); err == nil {
				return v
			}
			return true
		}
	}
	return false
}

// RenderError writes the envelope to stderr and returns a CommandError so the
// main package preserves the intended exit code without printing a second line.
func RenderError(cmd *cobra.Command, schemaVersion string, err error) error {
	if err == nil {
		return nil
	}
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	exitCode := exitCodeFor(err)
	payload := ErrorEnvelope{
		SchemaVersion: schemaVersion,
		Error: ErrorPayload{
			Code:     codeFor(err),
			Message:  messageFor(err),
			Hint:     hintFor(err),
			ExitCode: exitCode,
		},
	}
	enc := json.NewEncoder(cmd.ErrOrStderr())
	enc.SetIndent("", "  ")
	if encodeErr := enc.Encode(payload); encodeErr != nil {
		return encodeErr
	}
	return camperrors.NewCommand(cmd.CommandPath(), exitCode, "", err)
}

func exitCodeFor(err error) int {
	var cmdErr *camperrors.CommandError
	if errors.As(err, &cmdErr) && cmdErr.ExitCode != 0 {
		return cmdErr.ExitCode
	}
	if isUsageError(err) {
		return 2
	}
	return 1
}

func codeFor(err error) string {
	var validation *camperrors.ValidationError
	if errors.As(err, &validation) {
		return "validation_error"
	}
	var notFound *camperrors.NotFoundError
	if errors.As(err, &notFound) {
		return "not_found"
	}
	var config *camperrors.ConfigError
	if errors.As(err, &config) {
		return "config_error"
	}
	var ioErr *camperrors.IOError
	if errors.As(err, &ioErr) {
		return "io_error"
	}
	var permission *camperrors.PermissionError
	if errors.As(err, &permission) {
		return "permission_error"
	}
	var boundary *camperrors.BoundaryError
	if errors.As(err, &boundary) {
		return "boundary_error"
	}
	var gitErr *camperrors.GitError
	if errors.As(err, &gitErr) {
		return "git_error"
	}
	var cmdErr *camperrors.CommandError
	if errors.As(err, &cmdErr) {
		return "command_error"
	}
	if isCobraUsageError(err) {
		return "validation_error"
	}
	return "error"
}

func messageFor(err error) string {
	var validation *camperrors.ValidationError
	if errors.As(err, &validation) {
		return validation.Message
	}
	var notFound *camperrors.NotFoundError
	if errors.As(err, &notFound) {
		return notFound.Error()
	}
	var config *camperrors.ConfigError
	if errors.As(err, &config) {
		return config.Error()
	}
	var cmdErr *camperrors.CommandError
	if errors.As(err, &cmdErr) && cmdErr.Stderr != "" {
		return strings.TrimSpace(cmdErr.Stderr)
	}
	return err.Error()
}

func hintFor(err error) string {
	var hinted hintedError
	if errors.As(err, &hinted) {
		return hinted.hint
	}
	return ""
}

func isUsageError(err error) bool {
	var validation *camperrors.ValidationError
	if errors.As(err, &validation) {
		return true
	}
	var notFound *camperrors.NotFoundError
	if errors.As(err, &notFound) {
		return true
	}
	var boundary *camperrors.BoundaryError
	if errors.As(err, &boundary) {
		return true
	}
	return isCobraUsageError(err)
}

func isCobraUsageError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "arg(s)") ||
		strings.Contains(msg, "unknown flag") ||
		strings.Contains(msg, "invalid argument") ||
		strings.Contains(msg, "required flag")
}
