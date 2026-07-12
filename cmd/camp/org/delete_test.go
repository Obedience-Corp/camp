package org

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/spf13/cobra"
)

func execOrgWithFlags(t *testing.T, run func(*cobra.Command, []string) error, flags map[string]bool, args ...string) (string, error) {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	for name, val := range flags {
		cmd.Flags().Bool(name, val, "")
	}
	if cmd.Flags().Lookup("json") == nil {
		cmd.Flags().Bool("json", false, "")
	}
	err := run(cmd, args)
	return buf.String(), err
}

func TestOrgDelete(t *testing.T) {
	cases := []struct {
		name         string
		fixture      string
		arg          string
		force        bool
		wantErrSub   string // "" = success
		wantDeleted  bool
		wantMemberAt string // campaign id -> expected org after ("" = skip)
		wantMemberID string
	}{
		{
			name:        "empty org deletes",
			fixture:     `{"version":3,"orgs":[{"name":"empty-co"}],"campaigns":{}}`,
			arg:         "empty-co",
			wantDeleted: true,
		},
		{
			name:       "non-empty needs force",
			fixture:    orgFixture,
			arg:        "obey",
			wantErrSub: "has 2 member",
		},
		{
			name:         "force reassigns and deletes",
			fixture:      orgFixture,
			arg:          "obey",
			force:        true,
			wantDeleted:  true,
			wantMemberID: "B-2",
			wantMemberAt: "default",
		},
		{
			name:       "fallback refused",
			fixture:    orgFixture,
			arg:        "default",
			wantErrSub: "cannot delete the fallback",
		},
		{
			name:       "unknown not found",
			fixture:    orgFixture,
			arg:        "ghost",
			wantErrSub: "not found",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setOrgRegistry(t, tc.fixture)
			// Ensure load reconciliation has run so reg.Orgs is populated from v2 fixtures.
			if _, err := config.LoadRegistry(context.Background()); err != nil {
				t.Fatalf("preload: %v", err)
			}
			// For empty fixture, orgs are already on disk; for v2-style, seed via UpdateRegistry
			// so delete can find them after load reconcile.
			if tc.arg != "default" && tc.arg != "ghost" {
				_ = config.UpdateRegistry(context.Background(), func(reg *config.Registry) error {
					ensureOrg(reg, tc.arg)
					return nil
				})
			}

			out, err := execOrgWithFlags(t, runOrgDelete, map[string]bool{
				"force": tc.force,
				"json":  false,
			}, tc.arg)

			if tc.wantErrSub != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (out=%q)", tc.wantErrSub, out)
				}
				if !strings.Contains(err.Error(), tc.wantErrSub) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			reg, loadErr := config.LoadRegistry(context.Background())
			if loadErr != nil {
				t.Fatalf("LoadRegistry: %v", loadErr)
			}
			if tc.wantDeleted && orgExists(reg, tc.arg) {
				t.Fatalf("org %q still present after delete", tc.arg)
			}
			if tc.wantMemberID != "" {
				c, ok := reg.Campaigns[tc.wantMemberID]
				if !ok {
					t.Fatalf("campaign %s missing", tc.wantMemberID)
				}
				if c.Org != tc.wantMemberAt {
					t.Fatalf("campaign %s org = %q, want %q", tc.wantMemberID, c.Org, tc.wantMemberAt)
				}
			}
		})
	}
}

func TestOrgDelete_JSONShape(t *testing.T) {
	setOrgRegistry(t, `{"version":3,"orgs":[{"name":"empty-co"}],"campaigns":{}}`)
	out, err := execOrgWithFlags(t, runOrgDelete, map[string]bool{"json": true, "force": false}, "empty-co")
	if err != nil {
		t.Fatalf("delete --json: %v", err)
	}
	var result orgDeleteResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal: %v\nout=%s", err, out)
	}
	if !result.Deleted || result.Org != "empty-co" {
		t.Fatalf("unexpected result: %+v", result)
	}
	// reassigned must be [] not null in raw JSON
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["reassigned"]) != "[]" && string(raw["reassigned"]) != "" {
		// omitempty may drop empty slice; either [] or absent is OK, never null
		if string(raw["reassigned"]) == "null" {
			t.Fatal("reassigned marshaled as null")
		}
	}
}

func TestOrgDelete_ForceJSONReassignedSlice(t *testing.T) {
	setOrgRegistry(t, orgFixture)
	_ = config.UpdateRegistry(context.Background(), func(reg *config.Registry) error {
		ensureOrg(reg, "obey")
		return nil
	})
	out, err := execOrgWithFlags(t, runOrgDelete, map[string]bool{"json": true, "force": true}, "obey")
	if err != nil {
		t.Fatalf("delete --force --json: %v", err)
	}
	if strings.Contains(out, `"reassigned": null`) {
		t.Fatalf("reassigned is null: %s", out)
	}
	var result orgDeleteResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Reassigned) == 0 {
		t.Fatalf("expected reassigned members, got %+v / %s", result, out)
	}
}

func TestOrgDelete_DoesNotWriteOnError(t *testing.T) {
	path := setOrgRegistry(t, orgFixture)
	before, _ := os.ReadFile(path)
	_, err := execOrgWithFlags(t, runOrgDelete, map[string]bool{"force": false}, "obey")
	if err == nil {
		t.Fatal("expected error for non-empty delete without force")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("registry modified after failed delete")
	}
}
