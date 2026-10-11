//go:build unix

package dungeon

import (
	"os"
	"syscall"
	"testing"
	"time"
)

func TestExploreSignalsNameTheSignalThatArrived(t *testing.T) {
	quiet := watchExploreSignals()
	if sig := quiet.stop(); sig != nil {
		t.Fatalf("a watch with no signal reported %v", sig)
	}
	for _, want := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		watch := watchExploreSignals()
		if err := syscall.Kill(os.Getpid(), want); err != nil {
			watch.stop()
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for len(watch.ch) == 0 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if got := watch.stop(); got != want {
			t.Fatalf("watch reported %v, want %v", got, want)
		}
	}
}
