package campaign

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func registryPath() string {
	if override := os.Getenv("CAMP_REGISTRY_PATH"); override != "" {
		return override
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "obey", "campaign", "registry.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".obey", "campaign", "registry.json")
}

type registrySnapshot struct {
	Campaigns map[string]registeredCampaign `json:"campaigns"`
}

type registeredCampaign struct {
	Path string `json:"path"`
}

func lookupRegisteredCampaignRoot(campaignID string) (string, bool, error) {
	if campaignID == "" {
		return "", false, nil
	}

	data, err := os.ReadFile(registryPath())
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}

	var snapshot registrySnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return "", false, err
	}

	entry, ok := snapshot.Campaigns[campaignID]
	if !ok || entry.Path == "" {
		return "", false, nil
	}

	root, err := filepath.Abs(entry.Path)
	if err != nil {
		return "", false, err
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return root, true, nil
}
