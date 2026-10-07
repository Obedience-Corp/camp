package notice

func Detectors() []Detector {
	return []Detector{DungeonLegacy, StaleLinks, ArtifactRootNeverSynced, ArtifactRootsMissingLocally, ArtifactRootDrift}
}
