package dungeon

import (
	"testing"

	dungeonexplore "github.com/Obedience-Corp/camp/cmd/camp/dungeon/explore"
)

func TestDungeonRegistersPackageOwnedExplorePointer(t *testing.T) {
	for _, child := range Cmd.Commands() {
		if child == dungeonexplore.Cmd {
			return
		}
	}
	t.Fatal("dungeon command does not register package-owned explore pointer")
}
