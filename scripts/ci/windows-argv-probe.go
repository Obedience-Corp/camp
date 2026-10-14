// windows-argv-probe records the argv, cwd, and camp root seen by a child
// process. The Windows container proof builds and invokes it through camp.
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	f, err := os.Create(os.Getenv("CAMP_PROBE_OUT"))
	if err != nil {
		fail(err)
	}
	if err := json.NewEncoder(f).Encode(struct {
		Cwd      string   `json:"cwd"`
		CampRoot string   `json:"camp_root"`
		Args     []string `json:"args"`
	}{cwd, os.Getenv("CAMP_ROOT"), os.Args[1:]}); err != nil {
		_ = f.Close()
		fail(err)
	}
	if err := f.Close(); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
