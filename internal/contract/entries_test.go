package contract

import (
	"testing"

	"github.com/obediencecorp/obey-shared/contract"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCampEntries_ReturnsNonEmpty(t *testing.T) {
	entries := CampEntries()
	require.NotEmpty(t, entries, "CampEntries must return at least one entry")
}

func TestCampEntries_AllOwnedByCamp(t *testing.T) {
	for _, e := range CampEntries() {
		assert.Equal(t, contract.OwnerCamp, e.Owner,
			"entry %q must be owned by camp", e.ID)
	}
}

func TestCampEntries_UniqueIDs(t *testing.T) {
	seen := make(map[string]bool)
	for _, e := range CampEntries() {
		assert.False(t, seen[e.ID], "duplicate entry ID: %s", e.ID)
		seen[e.ID] = true
	}
}

func TestCampEntries_RequiredFieldsPresent(t *testing.T) {
	for _, e := range CampEntries() {
		t.Run(e.ID, func(t *testing.T) {
			assert.NotEmpty(t, e.ID, "ID must not be empty")
			assert.NotEmpty(t, e.Path, "Path must not be empty")
			assert.NotEmpty(t, e.Type, "Type must not be empty")
			assert.NotEmpty(t, string(e.Format), "Format must not be empty")
			assert.NotEmpty(t, string(e.Watch), "Watch must not be empty")
			assert.NotEmpty(t, e.Owner, "Owner must not be empty")
		})
	}
}

func TestCampEntries_ValidFormats(t *testing.T) {
	validFormats := map[contract.Format]bool{
		contract.FormatYAML:      true,
		contract.FormatJSON:      true,
		contract.FormatJSONL:     true,
		contract.FormatDirectory: true,
	}
	for _, e := range CampEntries() {
		assert.True(t, validFormats[e.Format],
			"entry %q has invalid format: %s", e.ID, e.Format)
	}
}

func TestCampEntries_ValidWatchModes(t *testing.T) {
	validModes := map[contract.WatchMode]bool{
		contract.WatchFile:      true,
		contract.WatchDirectory: true,
		contract.WatchAppend:    true,
	}
	for _, e := range CampEntries() {
		assert.True(t, validModes[e.Watch],
			"entry %q has invalid watch mode: %s", e.ID, e.Watch)
	}
}

func TestCampEntries_PassesContractValidation(t *testing.T) {
	c := &contract.Contract{
		Version: contract.ContractVersion,
		Entries: CampEntries(),
	}
	err := contract.Validate(c)
	assert.NoError(t, err, "CampEntries must produce a valid contract")
}

func TestCampEntries_NoTemplateEntries(t *testing.T) {
	for _, e := range CampEntries() {
		assert.False(t, e.Template,
			"camp entry %q must not use templates (camp doesn't use template expansion)", e.ID)
	}
}

func TestCampEntries_IntentStatusDirs(t *testing.T) {
	expectedStatuses := map[string]bool{
		"inbox":    false,
		"ready":    false,
		"active":   false,
		"archived": false,
	}

	for _, e := range CampEntries() {
		if e.Type == contract.TypeIntentStatusDir {
			assert.NotEmpty(t, e.Status, "intent dir entry %q must have Status", e.ID)
			assert.Equal(t, contract.FormatDirectory, e.Format,
				"intent dir entry %q must use FormatDirectory", e.ID)
			assert.Equal(t, contract.WatchDirectory, e.Watch,
				"intent dir entry %q must use WatchDirectory", e.ID)
			if _, ok := expectedStatuses[e.Status]; ok {
				expectedStatuses[e.Status] = true
			}
		}
	}

	for status, found := range expectedStatuses {
		assert.True(t, found, "expected intent status directory for %q not found", status)
	}
}

func TestCampEntries_ExpectedCount(t *testing.T) {
	entries := CampEntries()
	// 6 campaign state + 4 intent dirs + 1 workflow config = 11
	assert.Len(t, entries, 11, "expected 11 camp entries")
}
