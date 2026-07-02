package main

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/nav/fuzzy"
	"github.com/spf13/cobra"
)

func newTestRegistry(campaigns ...config.RegisteredCampaign) *config.Registry {
	reg := config.NewRegistry()
	for _, c := range campaigns {
		_ = reg.Register(c.ID, c.Name, c.Path, c.Type)
	}
	return reg
}

func newSwitchScopedRegistry(campaigns ...config.RegisteredCampaign) *config.Registry {
	reg := config.NewRegistry()
	for _, c := range campaigns {
		if c.Org == "" {
			c.Org = config.DefaultOrg
		}
		if c.Status == "" {
			c.Status = config.StatusActive
		}
		reg.Campaigns[c.ID] = c
	}
	return reg
}

func TestSwitchLookup(t *testing.T) {
	reg := newTestRegistry(
		config.RegisteredCampaign{
			ID:         "aaaa-1111",
			Name:       "obey-campaign",
			Path:       "/home/user/obey-campaign",
			Type:       "product",
			LastAccess: time.Now(),
		},
		config.RegisteredCampaign{
			ID:         "bbbb-2222",
			Name:       "side-project",
			Path:       "/home/user/side-project",
			Type:       "personal",
			LastAccess: time.Now(),
		},
		config.RegisteredCampaign{
			ID:         "cccc-3333",
			Name:       "research-ai",
			Path:       "/home/user/research-ai",
			Type:       "research",
			LastAccess: time.Now(),
		},
	)

	tests := []struct {
		name    string
		query   string
		want    string
		wantErr bool
	}{
		{
			name:  "exact name match",
			query: "obey-campaign",
			want:  "aaaa-1111",
		},
		{
			name:  "exact ID match",
			query: "bbbb-2222",
			want:  "bbbb-2222",
		},
		{
			name:  "ID prefix match",
			query: "cccc",
			want:  "cccc-3333",
		},
		{
			name:    "no match",
			query:   "nonexistent",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, ok := reg.Get(tt.query)
			if tt.wantErr {
				if ok {
					t.Fatalf("expected no match for %q, got %q", tt.query, c.Name)
				}
				return
			}
			if !ok {
				t.Fatalf("expected match for %q, got none", tt.query)
			}
			if c.ID != tt.want {
				t.Errorf("Get(%q).ID = %q, want %q", tt.query, c.ID, tt.want)
			}
		})
	}
}

func TestSwitchEmptyRegistry(t *testing.T) {
	reg := config.NewRegistry()
	if reg.Len() != 0 {
		t.Fatalf("expected empty registry, got %d", reg.Len())
	}
	_, ok := reg.Get("anything")
	if ok {
		t.Fatal("expected no match from empty registry")
	}
}

func TestSwitchFuzzyFallback(t *testing.T) {
	reg := newTestRegistry(
		config.RegisteredCampaign{
			ID:         "aaaa-1111",
			Name:       "obey-platform-monorepo",
			Path:       "/home/user/obey-platform-monorepo",
			Type:       "product",
			LastAccess: time.Now(),
		},
		config.RegisteredCampaign{
			ID:         "bbbb-2222",
			Name:       "side-project",
			Path:       "/home/user/side-project",
			Type:       "personal",
			LastAccess: time.Now(),
		},
		config.RegisteredCampaign{
			ID:         "cccc-3333",
			Name:       "research-ai",
			Path:       "/home/user/research-ai",
			Type:       "research",
			LastAccess: time.Now(),
		},
	)

	tests := []struct {
		name    string
		query   string
		want    string
		wantErr bool
	}{
		{
			name:  "fuzzy match partial name",
			query: "obey",
			want:  "obey-platform-monorepo",
		},
		{
			name:  "fuzzy match substring",
			query: "plat",
			want:  "obey-platform-monorepo",
		},
		{
			name:  "fuzzy match side",
			query: "side",
			want:  "side-project",
		},
		{
			name:    "no fuzzy match",
			query:   "zzzznothing",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Exact match should fail for these queries
			_, ok := reg.Get(tt.query)
			if ok {
				t.Skip("exact match found, fuzzy fallback not needed")
			}

			// Fuzzy fallback
			names := reg.List()
			matches := fuzzy.Filter(names, tt.query)
			if tt.wantErr {
				if len(matches) > 0 {
					t.Fatalf("expected no fuzzy match for %q, got %q", tt.query, matches[0].Target)
				}
				return
			}
			if len(matches) == 0 {
				t.Fatalf("expected fuzzy match for %q, got none", tt.query)
			}

			bestName := matches[0].Target
			c, ok := reg.GetByName(bestName)
			if !ok {
				t.Fatalf("GetByName(%q) failed after fuzzy match", bestName)
			}
			if c.Name != tt.want {
				t.Errorf("fuzzy match for %q = %q, want %q", tt.query, c.Name, tt.want)
			}
		})
	}
}

func TestSwitchExactMatchPreserved(t *testing.T) {
	reg := newTestRegistry(
		config.RegisteredCampaign{
			ID:         "aaaa-1111",
			Name:       "obey-campaign",
			Path:       "/home/user/obey-campaign",
			Type:       "product",
			LastAccess: time.Now(),
		},
	)

	// Exact name match should still work directly (no fuzzy needed)
	c, ok := reg.Get("obey-campaign")
	if !ok {
		t.Fatal("expected exact name match")
	}
	if c.ID != "aaaa-1111" {
		t.Errorf("exact match ID = %q, want %q", c.ID, "aaaa-1111")
	}
}

func TestParseSwitchSelector(t *testing.T) {
	tests := []struct {
		input        string
		wantOrg      string
		wantCampaign string
		wantTab      string
		wantHasTab   bool
	}{
		{input: "platform", wantCampaign: "platform"},
		{input: "platform@p", wantCampaign: "platform", wantTab: "p", wantHasTab: true},
		{input: "obey/platform", wantOrg: "obey", wantCampaign: "platform"},
		{input: "obey/platform@p", wantOrg: "obey", wantCampaign: "platform", wantTab: "p", wantHasTab: true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := cmdutil.ParseSwitchSelector(tt.input)
			if got.Org != tt.wantOrg || got.Campaign != tt.wantCampaign || got.Tab != tt.wantTab || got.HasTab != tt.wantHasTab {
				t.Fatalf("ParseSwitchSelector(%q) = %#v, want org=%q campaign=%q tab=%q hasTab=%v",
					tt.input, got, tt.wantOrg, tt.wantCampaign, tt.wantTab, tt.wantHasTab)
			}
		})
	}
}

func TestSwitchScopedFilteringAndResolution(t *testing.T) {
	reg := newSwitchScopedRegistry(
		config.RegisteredCampaign{ID: "default-alpha", Name: "alpha", Path: "/tmp/alpha", Org: config.DefaultOrg, Status: config.StatusActive},
		config.RegisteredCampaign{ID: "obey-platform", Name: "platform", Path: "/tmp/obey-platform", Org: "obey", Status: config.StatusActive},
		config.RegisteredCampaign{ID: "obey-content", Name: "content", Path: "/tmp/obey-content", Org: "obey", Status: config.StatusInactive},
		config.RegisteredCampaign{ID: "acme-platform", Name: "platform", Path: "/tmp/acme-platform", Org: "client-acme", Status: config.StatusActive},
		config.RegisteredCampaign{ID: "acme-archive", Name: "archive", Path: "/tmp/acme-archive", Org: "client-acme", Status: config.StatusReference},
	)

	got := cmdutil.FilterCampaigns(reg, cmdutil.CampaignScope{Org: "obey"})
	if names := campaignNamesForTest(got); strings.Join(names, ",") != "platform" {
		t.Fatalf("active obey candidates = %v, want [platform]", names)
	}

	_, err := cmdutil.ResolveCampaignSelectionScoped("content", reg, cmdutil.CampaignScope{Org: "obey"}, nil)
	if err == nil || !strings.Contains(err.Error(), "status \"active\"") {
		t.Fatalf("inactive default resolve error = %v, want active-status not found", err)
	}

	c, err := cmdutil.ResolveCampaignSelectionScoped("content", reg, cmdutil.CampaignScope{Org: "obey", All: true}, nil)
	if err != nil {
		t.Fatalf("--all inactive resolve: %v", err)
	}
	if c.ID != "obey-content" {
		t.Fatalf("--all resolved %q, want obey-content", c.ID)
	}

	c, err = cmdutil.ResolveCampaignSelectionScoped("plat", reg, cmdutil.CampaignScope{Org: "client-acme"}, nil)
	if err != nil {
		t.Fatalf("scoped fuzzy resolve: %v", err)
	}
	if c.ID != "acme-platform" {
		t.Fatalf("scoped fuzzy resolved %q, want acme-platform", c.ID)
	}
}

func TestSwitchCompletionScopedByOrgAndStatus(t *testing.T) {
	reg := newSwitchScopedRegistry(
		config.RegisteredCampaign{ID: "obey-platform", Name: "platform", Path: "/tmp/obey-platform", Org: "obey", Status: config.StatusActive},
		config.RegisteredCampaign{ID: "obey-content", Name: "content", Path: "/tmp/obey-content", Org: "obey", Status: config.StatusInactive},
		config.RegisteredCampaign{ID: "acme-platform", Name: "platform", Path: "/tmp/acme-platform", Org: "client-acme", Status: config.StatusActive},
	)

	got := completeSwitchCampaigns(reg, cmdutil.CampaignScope{}, "obey/")
	if strings.Join(got, ",") != "obey/platform" {
		t.Fatalf("obey/ completion = %v, want [obey/platform]", got)
	}

	got = completeSwitchCampaigns(reg, cmdutil.CampaignScope{Org: "obey"}, "")
	if strings.Join(got, ",") != "platform" {
		t.Fatalf("--org obey completion = %v, want [platform]", got)
	}

	got = completeSwitchCampaigns(reg, cmdutil.CampaignScope{Org: "obey", All: true}, "")
	if strings.Join(got, ",") != "content,platform" {
		t.Fatalf("--org obey --all completion = %v, want [content platform]", got)
	}

	got = completeSwitchCampaigns(reg, cmdutil.CampaignScope{}, "obe")
	if !containsString(got, "obey/") {
		t.Fatalf("org namespace completion = %v, want obey/", got)
	}
}

func TestSwitchJSONOutput(t *testing.T) {
	var buf bytes.Buffer
	cmd := switchCommandForTest(&buf)
	selected := config.RegisteredCampaign{
		ID:     "obey-platform",
		Name:   "platform",
		Org:    "obey",
		Status: config.StatusActive,
		Path:   "/tmp/platform",
	}
	if err := emitSwitchSelection(cmd, selected, "/tmp/platform/projects", "p", false, true); err != nil {
		t.Fatalf("emitSwitchSelection: %v", err)
	}
	var got switchOutput
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json output: %v\n%s", err, buf.String())
	}
	if got.SchemaVersion != "camp-switch/v1" || got.Campaign.Org != "obey" || got.Target.Tab != "p" || got.Target.Path != "/tmp/platform/projects" {
		t.Fatalf("json output = %#v", got)
	}
}

func TestSwitchTabCompletionFuzzy(t *testing.T) {
	names := []string{"obey-platform-monorepo", "side-project", "research-ai"}

	tests := []struct {
		name       string
		toComplete string
		wantCount  int
		wantFirst  string
	}{
		{
			name:       "empty query returns all",
			toComplete: "",
			wantCount:  3,
		},
		{
			name:       "fuzzy filter partial",
			toComplete: "obey",
			wantCount:  1,
			wantFirst:  "obey-platform-monorepo",
		},
		{
			name:       "fuzzy filter no match",
			toComplete: "zzzznothing",
			wantCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.toComplete == "" {
				if len(names) != tt.wantCount {
					t.Errorf("empty query: got %d names, want %d", len(names), tt.wantCount)
				}
				return
			}
			matches := fuzzy.Filter(names, tt.toComplete)
			if len(matches) != tt.wantCount {
				t.Errorf("Filter(%q): got %d matches, want %d", tt.toComplete, len(matches), tt.wantCount)
			}
			if tt.wantFirst != "" && len(matches) > 0 {
				if matches[0].Target != tt.wantFirst {
					t.Errorf("Filter(%q)[0] = %q, want %q", tt.toComplete, matches[0].Target, tt.wantFirst)
				}
			}
		})
	}
}

func campaignNamesForTest(campaigns []config.RegisteredCampaign) []string {
	names := make([]string, len(campaigns))
	for i, c := range campaigns {
		names[i] = c.Name
	}
	sort.Strings(names)
	return names
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func switchCommandForTest(out *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.Flags().Bool("print", false, "")
	cmd.Flags().Bool("json", false, "")
	cmd.Flags().String("org", "", "")
	cmd.Flags().String("status", "", "")
	cmd.Flags().Bool("all", false, "")
	return cmd
}

func TestResolveTabInCampaign(t *testing.T) {
	root, campaign := newTestCampaignDir(t, "tab-campaign")

	tests := []struct {
		name        string
		tabKey      string
		wantSuffix  string
		wantErr     bool
		errContains string
	}{
		{
			name:       "p resolves to projects/",
			tabKey:     "p",
			wantSuffix: "projects",
		},
		{
			name:       "f resolves to festivals/",
			tabKey:     "f",
			wantSuffix: "festivals",
		},
		{
			name:       "d resolves to docs/",
			tabKey:     "d",
			wantSuffix: "docs",
		},
		{
			name:        "unknown tab returns error",
			tabKey:      "zzz-no-such-tab",
			wantErr:     true,
			errContains: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			got, err := resolveTabInCampaign(ctx, campaign, tt.tabKey)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for tab %q, got path %q", tt.tabKey, got)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveTabInCampaign(%q): %v", tt.tabKey, err)
			}
			if !strings.HasPrefix(got, root) {
				t.Errorf("path %q does not start with campaign root %q", got, root)
			}
			if !strings.HasSuffix(got, tt.wantSuffix) {
				t.Errorf("path %q does not end with %q", got, tt.wantSuffix)
			}
		})
	}
}

func TestSwitchContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := config.LoadRegistry(ctx)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func newTestCampaignDir(t *testing.T, name string) (string, config.RegisteredCampaign) {
	t.Helper()
	root := t.TempDir()
	cfg := &config.CampaignConfig{
		ID:        name + "-id",
		Name:      name,
		Type:      config.CampaignTypeProduct,
		CreatedAt: time.Now(),
	}
	if err := config.SaveCampaignConfig(context.Background(), root, cfg); err != nil {
		t.Fatalf("save campaign config: %v", err)
	}
	c := config.RegisteredCampaign{
		ID:   name + "-id",
		Name: name,
		Path: root,
		Type: "product",
	}
	return root, c
}

func TestCompleteSwitchTabs(t *testing.T) {
	_, campaign := newTestCampaignDir(t, "test-campaign")
	reg := newTestRegistry(campaign)

	tests := []struct {
		name          string
		campaignQuery string
		tabPrefix     string
		wantContains  []string
		wantEmpty     bool
	}{
		{
			name:          "exact campaign no prefix returns default tabs",
			campaignQuery: "test-campaign",
			tabPrefix:     "",
			wantContains:  []string{"p", "f", "d", "w"},
		},
		{
			name:          "tab prefix filters to matching tabs",
			campaignQuery: "test-campaign",
			tabPrefix:     "d",
			wantContains:  []string{"d", "de", "du", "docs"},
		},
		{
			name:          "fuzzy campaign name resolves and returns tabs",
			campaignQuery: "test",
			tabPrefix:     "",
			wantContains:  []string{"p", "f"},
		},
		{
			name:          "unknown campaign returns empty",
			campaignQuery: "does-not-exist",
			tabPrefix:     "",
			wantEmpty:     true,
		},
		{
			name:          "tab prefix with no matches returns empty",
			campaignQuery: "test-campaign",
			tabPrefix:     "zzz",
			wantEmpty:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			got := completeSwitchTabs(ctx, reg, tt.campaignQuery, tt.tabPrefix, cmdutil.CampaignScope{All: true})
			if tt.wantEmpty {
				if len(got) != 0 {
					t.Errorf("expected empty result, got %v", got)
				}
				return
			}
			sort.Strings(got)
			for _, want := range tt.wantContains {
				found := false
				for _, g := range got {
					if g == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("completeSwitchTabs(%q, %q): missing %q in %v", tt.campaignQuery, tt.tabPrefix, want, got)
				}
			}
		})
	}
}
