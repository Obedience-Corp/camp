package intent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	intentcore "github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/spf13/cobra"
)

func TestIntentListJSONEnvelopeAndFormatAlias(t *testing.T) {
	root, _ := setupIntentJSONCampaign(t)
	chdirIntentJSONTest(t, root)

	stdout, stderr, err := executeIntentJSONTestCommand(t, newIntentListCommand(), "--json")
	if err != nil {
		t.Fatalf("list --json error = %v\nstderr=%s", err, stderr)
	}
	payload := decodeIntentJSONPayload(t, stdout)
	if got := payload["schema_version"]; got != IntentJSONVersion {
		t.Fatalf("schema_version = %v, want %q", got, IntentJSONVersion)
	}
	wantRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", root, err)
	}
	if got := payload["campaign_root"]; got != wantRoot {
		t.Fatalf("campaign_root = %v, want %q", got, wantRoot)
	}
	if got := len(payload["items"].([]any)); got != 3 {
		t.Fatalf("items length = %d, want 3", got)
	}
	assertIntentPayloadPathsJoin(t, payload)

	aliasStdout, aliasStderr, err := executeIntentJSONTestCommand(t, newIntentListCommand(), "-f", "json")
	if err != nil {
		t.Fatalf("list -f json error = %v\nstderr=%s", err, aliasStderr)
	}
	alias := decodeIntentJSONPayload(t, aliasStdout)
	normalizeGeneratedAt(payload)
	normalizeGeneratedAt(alias)
	if !reflect.DeepEqual(alias, payload) {
		t.Fatalf("-f json payload differs from --json\nalias=%#v\njson=%#v", alias, payload)
	}
}

func TestIntentListJSONPathsJoinFromSymlinkedRoot(t *testing.T) {
	root, _ := setupIntentJSONCampaign(t)
	link := filepath.Join(t.TempDir(), "campaign-link")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symlink campaign root: %v", err)
	}
	chdirIntentJSONTest(t, link)

	stdout, stderr, err := executeIntentJSONTestCommand(t, newIntentListCommand(), "--json")
	if err != nil {
		t.Fatalf("list --json error = %v\nstderr=%s", err, stderr)
	}
	payload := decodeIntentJSONPayload(t, stdout)
	wantRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", root, err)
	}
	if got := payload["campaign_root"]; got != wantRoot {
		t.Fatalf("campaign_root = %v, want %q", got, wantRoot)
	}
	assertIntentPayloadPathsJoin(t, payload)
}

func TestIntentListJSONHonorsMultipleTypeFilters(t *testing.T) {
	root, _ := setupIntentJSONCampaign(t)
	chdirIntentJSONTest(t, root)

	stdout, stderr, err := executeIntentJSONTestCommand(t, newIntentListCommand(), "--json", "--type", "idea", "--type", "bug")
	if err != nil {
		t.Fatalf("list --type idea --type bug error = %v\nstderr=%s", err, stderr)
	}
	payload := decodeIntentJSONPayload(t, stdout)
	items := payload["items"].([]any)
	if got := len(items); got != 2 {
		t.Fatalf("multi-type items length = %d, want 2", got)
	}
	types := map[string]bool{}
	for _, raw := range items {
		item := raw.(map[string]any)
		types[item["type"].(string)] = true
	}
	if !types["idea"] || !types["bug"] {
		t.Fatalf("multi-type filter returned types %#v, want idea and bug", types)
	}

	stdout, stderr, err = executeIntentJSONTestCommand(t, newIntentListCommand(), "--json", "--type", "idea")
	if err != nil {
		t.Fatalf("list --type idea error = %v\nstderr=%s", err, stderr)
	}
	payload = decodeIntentJSONPayload(t, stdout)
	items = payload["items"].([]any)
	if got := len(items); got != 1 {
		t.Fatalf("single-type items length = %d, want 1", got)
	}
	item := items[0].(map[string]any)
	if got := item["type"]; got != "idea" {
		t.Fatalf("single-type item type = %v, want idea", got)
	}
}

func TestIntentFindCountShowJSONEnvelopes(t *testing.T) {
	root, created := setupIntentJSONCampaign(t)
	chdirIntentJSONTest(t, root)

	stdout, stderr, err := executeIntentJSONTestCommand(t, newIntentFindCommand(), "--json", "Bug")
	if err != nil {
		t.Fatalf("find --json error = %v\nstderr=%s", err, stderr)
	}
	payload := decodeIntentJSONPayload(t, stdout)
	if got := payload["schema_version"]; got != IntentJSONVersion {
		t.Fatalf("find schema_version = %v, want %q", got, IntentJSONVersion)
	}
	if got := len(payload["items"].([]any)); got != 1 {
		t.Fatalf("find items length = %d, want 1", got)
	}

	stdout, stderr, err = executeIntentJSONTestCommand(t, newIntentCountCommand(), "--json")
	if err != nil {
		t.Fatalf("count --json error = %v\nstderr=%s", err, stderr)
	}
	payload = decodeIntentJSONPayload(t, stdout)
	if got := payload["schema_version"]; got != IntentJSONVersion {
		t.Fatalf("count schema_version = %v, want %q", got, IntentJSONVersion)
	}
	if _, ok := payload["total"].(float64); !ok {
		t.Fatalf("count payload missing numeric total: %#v", payload["total"])
	}
	if got := len(payload["items"].([]any)); got == 0 {
		t.Fatalf("count items length = %d, want status-count entries", got)
	}

	stdout, stderr, err = executeIntentJSONTestCommand(t, newIntentShowCommand(), "--json", created[0].ID)
	if err != nil {
		t.Fatalf("show --json error = %v\nstderr=%s", err, stderr)
	}
	payload = decodeIntentJSONPayload(t, stdout)
	items := payload["items"].([]any)
	if got := len(items); got != 1 {
		t.Fatalf("show items length = %d, want 1", got)
	}
	item := items[0].(map[string]any)
	if got := item["id"]; got != created[0].ID {
		t.Fatalf("show item id = %v, want %q", got, created[0].ID)
	}
}

func TestIntentAddJSONEmitsCreatedPayload(t *testing.T) {
	root, _ := setupIntentJSONCampaign(t)
	chdirIntentJSONTest(t, root)

	stdout, stderr, err := executeIntentJSONTestCommand(t, newIntentAddCommand(), "--json", "--no-commit", "Created from JSON")
	if err != nil {
		t.Fatalf("add --json error = %v\nstderr=%s", err, stderr)
	}
	payload := decodeIntentJSONPayload(t, stdout)
	if got := payload["schema_version"]; got != IntentJSONVersion {
		t.Fatalf("schema_version = %v, want %q", got, IntentJSONVersion)
	}
	if payload["id"] == "" {
		t.Fatalf("add payload missing id: %#v", payload)
	}
	path, ok := payload["path"].(string)
	if !ok || path == "" {
		t.Fatalf("add payload missing path: %#v", payload)
	}
	if filepath.IsAbs(path) {
		t.Fatalf("add payload path is absolute: %q", path)
	}
	campaignRoot := payload["campaign_root"].(string)
	if _, err := os.Stat(filepath.Join(campaignRoot, path)); err != nil {
		t.Fatalf("created intent path missing: %v", err)
	}
	if bytes.Contains([]byte(stdout), []byte("Idea created")) {
		t.Fatalf("add --json leaked human output: %s", stdout)
	}
}

func TestIntentAddFullHasNoFShorthand(t *testing.T) {
	cmd := newIntentAddCommand()
	if flag := cmd.Flags().ShorthandLookup("f"); flag != nil {
		t.Fatalf("unexpected -f shorthand on %q", flag.Name)
	}
}

func TestIntentListJSONErrorEnvelope(t *testing.T) {
	chdirIntentJSONTest(t, t.TempDir())

	stdout, stderr, err := executeIntentJSONTestCommand(t, newIntentListCommand(), "--json")
	if err == nil {
		t.Fatal("list --json outside campaign error = nil, want non-zero command error")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty on JSON error", stdout)
	}
	var cmdErr *camperrors.CommandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("error = %T %v, want *CommandError", err, err)
	}
	payload := decodeIntentJSONPayload(t, stderr)
	if got := payload["schema_version"]; got != IntentJSONVersion {
		t.Fatalf("error schema_version = %v, want %q", got, IntentJSONVersion)
	}
	if _, ok := payload["error"].(map[string]any); !ok {
		t.Fatalf("missing error envelope: %#v", payload)
	}
}

func TestIntentFormatJSONFlagErrorsUseJSONEnvelope(t *testing.T) {
	chdirIntentJSONTest(t, t.TempDir())

	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{name: "list", cmd: newIntentListCommand()},
		{name: "count", cmd: newIntentCountCommand()},
		{name: "find", cmd: newIntentFindCommand()},
		{name: "show", cmd: newIntentShowCommand()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, err := executeIntentJSONTestCommand(t, tt.cmd, "-f", "json", "--badflag")
			if err == nil {
				t.Fatal("-f json flag error = nil, want non-zero command error")
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty on JSON flag error", stdout)
			}
			var cmdErr *camperrors.CommandError
			if !errors.As(err, &cmdErr) {
				t.Fatalf("error = %T %v, want *CommandError", err, err)
			}
			payload := decodeIntentJSONPayload(t, stderr)
			if got := payload["schema_version"]; got != IntentJSONVersion {
				t.Fatalf("error schema_version = %v, want %q", got, IntentJSONVersion)
			}
			if _, ok := payload["error"].(map[string]any); !ok {
				t.Fatalf("missing error envelope: %#v", payload)
			}
		})
	}
}

func setupIntentJSONCampaign(t *testing.T) (string, []*intentcore.Intent) {
	t.Helper()

	ctx := context.Background()
	root := t.TempDir()
	cfg := &config.CampaignConfig{
		ID:        "intent-json-test",
		Name:      "intent-json-test",
		Type:      config.CampaignTypeProduct,
		CreatedAt: time.Now(),
	}
	if err := config.SaveCampaignConfig(ctx, root, cfg); err != nil {
		t.Fatalf("SaveCampaignConfig: %v", err)
	}
	jumps := config.DefaultJumpsConfig()
	if err := config.SaveJumpsConfig(ctx, root, &jumps); err != nil {
		t.Fatalf("SaveJumpsConfig: %v", err)
	}

	resolver := paths.NewResolverFromConfig(root, cfg)
	svc := intentcore.NewIntentService(root, resolver.Intents())
	if err := svc.EnsureDirectories(ctx); err != nil {
		t.Fatalf("EnsureDirectories: %v", err)
	}

	fixtures := []intentcore.CreateOptions{
		{Title: "Idea payload", Type: intentcore.TypeIdea, Author: "agent", Timestamp: time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)},
		{Title: "Bug payload", Type: intentcore.TypeBug, Author: "agent", Timestamp: time.Date(2026, 1, 1, 10, 1, 0, 0, time.UTC)},
		{Title: "Chore payload", Type: intentcore.TypeChore, Author: "agent", Timestamp: time.Date(2026, 1, 1, 10, 2, 0, 0, time.UTC)},
	}
	created := make([]*intentcore.Intent, 0, len(fixtures))
	for _, opts := range fixtures {
		i, err := svc.CreateDirect(ctx, opts)
		if err != nil {
			t.Fatalf("CreateDirect(%q): %v", opts.Title, err)
		}
		created = append(created, i)
	}
	return root, created
}

func chdirIntentJSONTest(t *testing.T, dir string) {
	t.Helper()
	t.Chdir(dir)
}

func executeIntentJSONTestCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, string, error) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err := cmd.ExecuteContext(context.Background())
	return stdout.String(), stderr.String(), err
}

func decodeIntentJSONPayload(t *testing.T, raw string) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("invalid JSON payload: %v\n%s", err, raw)
	}
	return payload
}

func assertIntentPayloadPathsJoin(t *testing.T, payload map[string]any) {
	t.Helper()

	campaignRoot, ok := payload["campaign_root"].(string)
	if !ok || campaignRoot == "" {
		t.Fatalf("payload missing campaign_root: %#v", payload)
	}
	items, ok := payload["items"].([]any)
	if !ok {
		t.Fatalf("payload missing items: %#v", payload)
	}
	for _, raw := range items {
		item := raw.(map[string]any)
		path, ok := item["path"].(string)
		if !ok || path == "" {
			t.Fatalf("intent item missing path: %#v", item)
		}
		if filepath.IsAbs(path) {
			t.Fatalf("intent item path is absolute: %q", path)
		}
		if _, err := os.Stat(filepath.Join(campaignRoot, path)); err != nil {
			t.Fatalf("joined intent path missing for %q: %v", path, err)
		}
	}
}

func normalizeGeneratedAt(payload map[string]any) {
	payload["generated_at"] = ""
}
