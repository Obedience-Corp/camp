package main

import (
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/settings"
)

func init() {
	settingsCmd.AddCommand(newSettingsCatalogCmd())
	settingsCmd.AddCommand(newSettingsDocumentCmd())
}

func newSettingsDocumentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "document",
		Short: "Read and edit camp documents",
		Long: `Read, validate, and edit documents under the selected camp's .campaign directory.

Paths are relative to .campaign. Known catalog IDs are accepted wherever a path is.
Reads and validation do not create or rewrite files. Mutations require
--expected-revision and return the persisted snapshot.`,
	}
	cmd.AddCommand(newSettingsDocumentGetCmd())
	cmd.AddCommand(newSettingsDocumentValidateCmd())
	cmd.AddCommand(newSettingsDocumentSetCmd())
	cmd.AddCommand(newSettingsDocumentCreateCmd())
	cmd.AddCommand(newSettingsDocumentPatchCmd())
	cmd.AddCommand(newSettingsDocumentRenameCmd())
	cmd.AddCommand(newSettingsDocumentDeleteCmd())
	cmd.AddCommand(newSettingsDocumentActionCmd())
	return cmd
}

func newSettingsCatalogCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "List camp documents",
		Long: `List documents under the selected camp's .campaign directory.

Prints the settings-documents/v1alpha1 catalog, including known schemas that
are absent. Nothing is created or rewritten.`,
		Example: `  camp settings catalog
  camp settings catalog --json
  camp settings catalog --campaign my-camp --json`,
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Read-only document catalog with --json support",
		},
	}
	bindSettingsDocumentCmd(cmd, &jsonOut, cobra.NoArgs, runSettingsCatalog)
	return cmd
}

func newSettingsDocumentGetCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "get <path>",
		Short: "Print one camp document",
		Long: `Print one document under the selected camp's .campaign directory.

<path> is a relative path or a known catalog ID. Missing files stay absent.
Nothing is created or rewritten.`,
		Example: `  camp settings document get campaign.yaml
  camp settings document get settings/local.json --json
  camp settings document get campaign.yaml --campaign my-camp --json`,
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Read-only document read with --json support",
		},
	}
	bindSettingsDocumentCmd(cmd, &jsonOut, cobra.ExactArgs(1), runSettingsDocumentGet)
	return cmd
}

func newSettingsDocumentValidateCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "validate <path>",
		Short: "Validate camp document source from stdin",
		Long: `Validate candidate document bytes without writing them.

<path> is a relative path or a known catalog ID. --stdin is required and reads
raw UTF-8 source. An invalid candidate is a result with valid:false.
Nothing is created or rewritten.`,
		Example: `  camp settings document validate campaign.yaml --stdin
  camp settings document validate settings/local.json --stdin --json
  camp settings document validate campaign.yaml --campaign my-camp --stdin --json`,
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Read-only document validation with --json support",
		},
	}
	bindSettingsDocumentCmd(cmd, &jsonOut, cobra.ExactArgs(1), runSettingsDocumentValidate)
	cmd.Flags().Bool("stdin", false, "read document source from stdin")
	return cmd
}

func newSettingsDocumentSetCmd() *cobra.Command {
	return newSettingsDocumentEditCmd(
		"set <path>",
		"Replace a camp document from stdin",
		`Replace one document with the raw UTF-8 bytes from stdin.

--stdin and a non-empty --expected-revision are required. The write is refused
when the current revision differs. The response is the persisted snapshot.`,
		`  camp settings document set settings/jumps.yaml --expected-revision sha256:abc --stdin
  camp settings document set settings/jumps.yaml --expected-revision sha256:abc --stdin --json
  camp settings document set settings/jumps.yaml --campaign my-camp --expected-revision sha256:abc --stdin --json`,
		"Document source replace with --json support",
		runSettingsDocumentSet,
	)
}

func newSettingsDocumentCreateCmd() *cobra.Command {
	return newSettingsDocumentEditCmd(
		"create <path>",
		"Create a camp document from stdin",
		`Create one absent document from the raw UTF-8 bytes on stdin.

--stdin and --expected-revision absent are required. An existing path is refused.
The response is the persisted snapshot.`,
		`  camp settings document create notes/readme.md --expected-revision absent --stdin
  camp settings document create notes/readme.md --expected-revision absent --stdin --json
  camp settings document create notes/readme.md --campaign my-camp --expected-revision absent --stdin --json`,
		"Document create with --json support",
		runSettingsDocumentCreate,
	)
}

func newSettingsDocumentPatchCmd() *cobra.Command {
	return newSettingsDocumentEditCmd(
		"patch <path>",
		"Patch a camp document from stdin",
		`Apply a JSON patch from stdin to one document.

The body is {"changes":[{"op":"set"|"remove","path":["key"],"value":...}]}.
--stdin and a non-empty --expected-revision are required. Comments and unknown
keys are preserved. The response is the persisted snapshot.`,
		`  camp settings document patch settings/jumps.yaml --expected-revision sha256:abc --stdin
  camp settings document patch settings/jumps.yaml --expected-revision sha256:abc --stdin --json
  camp settings document patch settings/jumps.yaml --campaign my-camp --expected-revision sha256:abc --stdin --json`,
		"Document patch with --json support",
		runSettingsDocumentPatch,
	)
}

func newSettingsDocumentRenameCmd() *cobra.Command {
	return newSettingsDocumentRevCmd(
		"rename <path> <destination>",
		"Rename a camp document",
		`Rename one document to <destination> inside .campaign.

A non-empty --expected-revision is required. An existing destination is refused.
The response is the destination snapshot.`,
		`  camp settings document rename notes/readme.md notes/done.md --expected-revision sha256:abc
  camp settings document rename notes/readme.md notes/done.md --expected-revision sha256:abc --json
  camp settings document rename notes/readme.md notes/done.md --campaign my-camp --expected-revision sha256:abc --json`,
		"Document rename with --json support",
		2,
		runSettingsDocumentRename,
	)
}

func newSettingsDocumentDeleteCmd() *cobra.Command {
	return newSettingsDocumentRevCmd(
		"delete <path>",
		"Delete a camp document",
		`Delete one document inside .campaign.

A non-empty --expected-revision is required. The response is an absent snapshot
for the original path.`,
		`  camp settings document delete notes/done.md --expected-revision sha256:abc
  camp settings document delete notes/done.md --expected-revision sha256:abc --json
  camp settings document delete notes/done.md --campaign my-camp --expected-revision sha256:abc --json`,
		"Document delete with --json support",
		1,
		runSettingsDocumentDelete,
	)
}

func newSettingsDocumentActionCmd() *cobra.Command {
	return newSettingsDocumentEditCmd(
		"action <path> <action>",
		"Run an owner action on a camp document",
		`Run one supported owner action. The JSON body is read from stdin.

--stdin and a non-empty --expected-revision are required. The response is the
persisted snapshot with applied:true. Unsupported actions are errors.`,
		`  camp settings document action settings/pins.json pin --expected-revision absent --stdin
  camp settings document action settings/pins.json unpin --expected-revision sha256:abc --stdin --json
  camp settings document action settings/workitems.json workitem-priority --campaign my-camp --expected-revision absent --stdin --json`,
		"Document owner action with --json support",
		runSettingsDocumentAction,
	)
}

func newSettingsDocumentEditCmd(use, short, long, example, reason string, run func(*cobra.Command, []string) error) *cobra.Command {
	cmd := newSettingsDocumentRevCmd(use, short, long, example, reason, bodyArgCount(use), run)
	cmd.Flags().Bool("stdin", false, "read the request body from stdin")
	return cmd
}

func newSettingsDocumentRevCmd(use, short, long, example, reason string, nArgs int, run func(*cobra.Command, []string) error) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:     use,
		Short:   short,
		Long:    long,
		Example: example,
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  reason,
		},
	}
	bindSettingsDocumentCmd(cmd, &jsonOut, cobra.ExactArgs(nArgs), run)
	cmd.Flags().String("expected-revision", "", "Revision the edit must match (absent or sha256:<hex>)")
	return cmd
}

func bodyArgCount(use string) int {
	if strings.HasPrefix(use, "action ") {
		return 2
	}
	return 1
}

func bindSettingsDocumentCmd(cmd *cobra.Command, jsonOut *bool, positional cobra.PositionalArgs, run func(*cobra.Command, []string) error) {
	requested := func() bool { return *jsonOut }
	cmd.SilenceUsage = true
	cmd.Flags().BoolVar(jsonOut, "json", false, "emit a structured JSON result")
	addSettingsCampaignFlag(cmd)
	cmd.Args = jsoncontract.Args(settings.DocumentSchemaVersion, requested, positional)
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(settings.DocumentSchemaVersion, requested))
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		err := run(cmd, args)
		if err == nil {
			return nil
		}
		var op *settings.OpError
		if errors.As(err, &op) {
			return writeSettingsDocumentOpError(cmd, op)
		}
		if jsoncontract.Requested(cmd, requested) {
			return jsoncontract.RenderError(cmd, settings.DocumentSchemaVersion, err)
		}
		return err
	}
}

func runSettingsCatalog(cmd *cobra.Command, _ []string) error {
	svc, err := settingsDocumentService(cmd)
	if err != nil {
		return err
	}
	catalog, err := svc.Catalog(cmd.Context())
	if err != nil {
		return err
	}
	return writeSettingsDocumentResult(cmd, catalog)
}

func runSettingsDocumentGet(cmd *cobra.Command, args []string) error {
	svc, err := settingsDocumentService(cmd)
	if err != nil {
		return err
	}
	snapshot, err := svc.Get(cmd.Context(), args[0])
	if err != nil {
		return err
	}
	return writeSettingsDocumentResult(cmd, snapshot)
}

func runSettingsDocumentValidate(cmd *cobra.Command, args []string) error {
	if err := requireSettingsDocumentStdin(cmd); err != nil {
		return err
	}
	svc, err := settingsDocumentService(cmd)
	if err != nil {
		return err
	}
	source, err := readSettingsDocumentBody(cmd, args[0])
	if err != nil {
		return err
	}
	result, err := svc.Validate(cmd.Context(), args[0], source)
	if err != nil {
		return err
	}
	return writeSettingsDocumentResult(cmd, result)
}

func runSettingsDocumentSet(cmd *cobra.Command, args []string) error {
	ref := args[0]
	return runSettingsDocumentBody(cmd, ref, func(svc settings.Service, expected string, source []byte) (settings.Snapshot, error) {
		return svc.Set(cmd.Context(), ref, expected, source)
	})
}

func runSettingsDocumentCreate(cmd *cobra.Command, args []string) error {
	ref := args[0]
	return runSettingsDocumentBody(cmd, ref, func(svc settings.Service, expected string, source []byte) (settings.Snapshot, error) {
		return svc.Create(cmd.Context(), ref, expected, source)
	})
}

func runSettingsDocumentPatch(cmd *cobra.Command, args []string) error {
	ref := args[0]
	return runSettingsDocumentBody(cmd, ref, func(svc settings.Service, expected string, source []byte) (settings.Snapshot, error) {
		return svc.Patch(cmd.Context(), ref, expected, source)
	})
}

func runSettingsDocumentRename(cmd *cobra.Command, args []string) error {
	expected, err := requireSettingsDocumentRevision(cmd)
	if err != nil {
		return err
	}
	svc, err := settingsDocumentService(cmd)
	if err != nil {
		return err
	}
	snapshot, err := svc.Rename(cmd.Context(), args[0], args[1], expected)
	if err != nil {
		return err
	}
	return writeSettingsDocumentResult(cmd, snapshot)
}

func runSettingsDocumentDelete(cmd *cobra.Command, args []string) error {
	expected, err := requireSettingsDocumentRevision(cmd)
	if err != nil {
		return err
	}
	svc, err := settingsDocumentService(cmd)
	if err != nil {
		return err
	}
	snapshot, err := svc.Delete(cmd.Context(), args[0], expected)
	if err != nil {
		return err
	}
	return writeSettingsDocumentResult(cmd, snapshot)
}

func runSettingsDocumentAction(cmd *cobra.Command, args []string) error {
	ref, action := args[0], args[1]
	return runSettingsDocumentBody(cmd, ref, func(svc settings.Service, expected string, source []byte) (settings.Snapshot, error) {
		return svc.Action(cmd.Context(), ref, action, expected, source)
	})
}

func runSettingsDocumentBody(cmd *cobra.Command, ref string, call func(settings.Service, string, []byte) (settings.Snapshot, error)) error {
	expected, err := requireSettingsDocumentRevision(cmd)
	if err != nil {
		return err
	}
	if err := requireSettingsDocumentStdin(cmd); err != nil {
		return err
	}
	svc, err := settingsDocumentService(cmd)
	if err != nil {
		return err
	}
	source, err := readSettingsDocumentBody(cmd, ref)
	if err != nil {
		return err
	}
	snapshot, err := call(svc, expected, source)
	if err != nil {
		return err
	}
	return writeSettingsDocumentResult(cmd, snapshot)
}

// settingsDocumentService resolves an explicit registered camp without loading
// jumps or repairing the manifest. An omitted --campaign uses the current camp.
func settingsDocumentService(cmd *cobra.Command) (settings.Service, error) {
	ctx := cmd.Context()
	if cmd.Flags().Changed("campaign") {
		query, err := cmd.Flags().GetString("campaign")
		if err != nil {
			return settings.Service{}, err
		}
		if strings.TrimSpace(query) == "" {
			return settings.Service{}, camperrors.New("--campaign requires a registered camp name or ID")
		}
		reg, err := config.LoadRegistry(ctx)
		if err != nil {
			return settings.Service{}, camperrors.Wrap(err, "load target camp registry")
		}
		selected, err := selectSettingsCampaign(reg, query)
		if err != nil {
			return settings.Service{}, err
		}
		return settings.NewService(ctx, selected.Path, selected.ID)
	}
	root, err := settingsCampaignRoot(ctx)
	if err != nil {
		return settings.Service{}, err
	}
	return settings.NewService(ctx, root, "")
}

func requireSettingsDocumentRevision(cmd *cobra.Command) (string, error) {
	flag := cmd.Flags().Lookup("expected-revision")
	if flag == nil || !flag.Changed {
		return "", camperrors.NewValidation("expected-revision", `required flag "expected-revision" not set`, nil)
	}
	value, err := cmd.Flags().GetString("expected-revision")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", camperrors.NewValidation("expected-revision", `required flag "expected-revision" not set`, nil)
	}
	return value, nil
}

func requireSettingsDocumentStdin(cmd *cobra.Command) error {
	flag := cmd.Flags().Lookup("stdin")
	if flag == nil || !flag.Changed {
		return camperrors.NewValidation("stdin", `required flag "stdin" not set`, nil)
	}
	enabled, err := cmd.Flags().GetBool("stdin")
	if err != nil {
		return err
	}
	if !enabled {
		return camperrors.NewValidation("stdin", `required flag "stdin" not set`, nil)
	}
	return nil
}

func readSettingsDocumentBody(cmd *cobra.Command, ref string) ([]byte, error) {
	limit := settings.DefaultLimits().MaxDocumentBytes
	body, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), int64(limit)+1))
	if err != nil {
		return nil, &settings.OpError{Code: settings.CodeIO, Message: "read stdin", Path: ref, Err: err}
	}
	if len(body) > limit {
		return nil, &settings.OpError{
			Code:    settings.CodeOversized,
			Message: "document exceeds the size limit",
			Path:    ref,
		}
	}
	return body, nil
}

func writeSettingsDocumentResult(cmd *cobra.Command, payload any) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func writeSettingsDocumentOpError(cmd *cobra.Command, op *settings.OpError) error {
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	enc := json.NewEncoder(cmd.ErrOrStderr())
	enc.SetIndent("", "  ")
	if err := enc.Encode(op.Response()); err != nil {
		return err
	}
	return camperrors.NewCommand(cmd.CommandPath(), 1, "", op)
}
