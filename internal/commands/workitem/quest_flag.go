package workitem

import "github.com/Obedience-Corp/camp/internal/version"

func questFlagHelp() string {
	if version.Profile == "dev" {
		return "capture quest_id from this quest (defaults to CAMP_QUEST env var if set)"
	}
	return "quest ID to associate (requires dev-profile camp; forward-compatible flag)"
}
