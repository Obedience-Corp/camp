package jsoncontract

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

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
// structured error envelope as RunE failures. The flags are parsed by the
// time it runs, so the command's own answer decides (Requested).
func Args(schemaVersion string, jsonRequested func() bool, validate cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		err := validate(cmd, args)
		if err == nil || !Requested(cmd, jsonRequested) {
			return err
		}
		return RenderError(cmd, schemaVersion, err)
	}
}

// RunE wraps a command implementation and renders returned errors as JSON when
// --json was requested. Like Args, it runs on parsed flags.
func RunE(schemaVersion string, jsonRequested func() bool, run func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		err := run(cmd, args)
		if err == nil || !Requested(cmd, jsonRequested) {
			return err
		}
		return RenderError(cmd, schemaVersion, err)
	}
}

// FlagErrorFunc renders flag parse failures as JSON when the command line
// asked for JSON. Cobra raises these before Args/RunE, so this hook is the
// only chance to keep the machine-readable contract.
//
// pflag stops at the argument it refuses, so a --json after that one was
// never read. The hook reads the rest of the command line for it first
// (finishJSONFlag) and then asks the same question Args and RunE ask.
func FlagErrorFunc(schemaVersion string, jsonRequested func() bool) func(*cobra.Command, error) error {
	return func(cmd *cobra.Command, err error) error {
		if err == nil {
			return err
		}
		finishJSONFlag(cmd, os.Args[1:])
		if !Requested(cmd, jsonRequested) {
			cmd.SilenceUsage = false
			if root := cmd.Root(); root != nil {
				root.SilenceUsage = false
			}
			return camperrors.NewCommand(cmd.CommandPath(), 2, "", err)
		}
		return RenderError(cmd, schemaVersion, err)
	}
}

// jsonFlag is the flag every command with a JSON contract defines.
const jsonFlag = "json"

// Requested reports whether the parsed command asked for JSON output.
//
// jsonRequested is the command's own answer. It reads what cobra bound: the
// --json flag, and whatever else selects JSON for that command, such as a
// --format json alias or a pipe. A nil jsonRequested is the --json flag
// alone. argv is not consulted: a --json there can be another flag's value,
// can follow --, or can be overridden by a later --json=false, and cobra has
// already decided each of those.
func Requested(cmd *cobra.Command, jsonRequested func() bool) bool {
	if jsonRequested != nil {
		return jsonRequested()
	}
	if cmd == nil {
		return false
	}
	flag := cmd.Flags().Lookup(jsonFlag)
	if flag == nil {
		return false
	}
	asJSON, err := strconv.ParseBool(flag.Value.String())
	return err == nil && asJSON
}

// finishJSONFlag gives the command's --json flag the value the whole command
// line gives it. pflag stops at the first argument it refuses and leaves a
// later --json unread, so a caller who asked for JSON would get the refusal
// as text. The flag is set, not just reported, so the command's own answer
// (Requested) sees it beside everything else it weighs.
func finishJSONFlag(cmd *cobra.Command, argv []string) {
	flags := cmd.Flags()
	if flags.Lookup(jsonFlag) == nil {
		return
	}
	if asJSON, found := scanJSONFlag(flags, argv); found {
		_ = flags.Set(jsonFlag, strconv.FormatBool(asJSON))
	}
}

// scanJSONFlag reads argv for --json the way pflag would have, had it not
// stopped: it ends at --, steps over the value of a flag that takes one, and
// lets the last --json or --json=<bool> win. found is false when argv holds
// no --json flag at all.
//
// A value strconv.ParseBool refuses counts as a request: --json=pretty asks
// for JSON, and the refusal of that value is itself reported as JSON. A flag
// the command does not define is taken to have no value; it is the argument
// pflag refused, and what follows it is read as written.
func scanJSONFlag(flags *pflag.FlagSet, argv []string) (asJSON, found bool) {
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		switch {
		case arg == "--":
			return asJSON, found
		case strings.HasPrefix(arg, "--"):
			name, value, hasValue := strings.Cut(arg[2:], "=")
			if name == jsonFlag {
				asJSON, found = jsonValue(value, hasValue), true
				continue
			}
			if !hasValue && takesValue(flags.Lookup(name)) {
				i++
			}
		case len(arg) > 1 && arg[0] == '-':
			// A group of shorthands, -abc. One that takes a value ends the
			// group: the rest of the argument is its value, or the next
			// argument is when nothing is left.
			for rest := arg[1:]; rest != ""; rest = rest[1:] {
				flag := flags.ShorthandLookup(rest[:1])
				if flag == nil {
					break
				}
				value, hasValue := strings.CutPrefix(rest[1:], "=")
				if flag.Name == jsonFlag {
					asJSON, found = jsonValue(value, hasValue), true
				}
				if hasValue {
					break
				}
				if takesValue(flag) {
					if rest[1:] == "" {
						i++
					}
					break
				}
			}
		}
	}
	return asJSON, found
}

// jsonValue is what --json or --json=<value> asks for.
func jsonValue(value string, hasValue bool) bool {
	if !hasValue {
		return true
	}
	asJSON, err := strconv.ParseBool(value)
	return err != nil || asJSON
}

// takesValue reports whether the flag's value is its own argument. A boolean
// and a flag with an optional value take none.
func takesValue(flag *pflag.Flag) bool {
	return flag != nil && flag.NoOptDefVal == ""
}

// BoolArg reports whether the process argv enables the named boolean flag.
// The last occurrence wins, as it does when pflag parses repeated flags. An
// explicit value strconv.ParseBool reads as false (--flag=false, =0, =f) is
// false. An unparseable value is treated as enabled, so a later form such as
// --json=pretty still counts as opting in.
//
// A flag-parse hook that runs before the bool is bound uses this. Requested
// does not: once cobra has parsed the command, the command's own answer
// decides, and a --json in argv can be another flag's value.
func BoolArg(name string) bool {
	flag := "--" + name
	enabled := false
	for _, arg := range os.Args[1:] {
		if arg == flag {
			enabled = true
			continue
		}
		rest, ok := strings.CutPrefix(arg, flag+"=")
		if !ok {
			continue
		}
		v, err := strconv.ParseBool(rest)
		enabled = err != nil || v
	}
	return enabled
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

// Hint returns the recovery hint WithHint attached to err, or "" when there is
// none. Text output paths use it to print the same guidance the JSON envelope
// carries, so a refusal reads the same either way.
func Hint(err error) string { return hintFor(err) }

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
		strings.Contains(msg, "unknown shorthand flag") ||
		strings.Contains(msg, "flag needs an argument") ||
		strings.Contains(msg, "bad flag syntax") ||
		strings.Contains(msg, "invalid argument") ||
		strings.Contains(msg, "required flag")
}
