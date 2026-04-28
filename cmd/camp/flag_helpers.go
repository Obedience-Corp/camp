package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// getFlagString retrieves a registered string flag value. Missing or wrong-type
// flags are programmer errors, so fail loudly instead of silently zeroing.
func getFlagString(cmd *cobra.Command, name string) string {
	v, err := cmd.Flags().GetString(name)
	if err != nil {
		panic(fmt.Sprintf("flag not registered or not string: %s: %v", name, err))
	}
	return v
}

// getFlagBool retrieves a registered bool flag value. Missing or wrong-type
// flags are programmer errors, so fail loudly instead of silently zeroing.
func getFlagBool(cmd *cobra.Command, name string) bool {
	v, err := cmd.Flags().GetBool(name)
	if err != nil {
		panic(fmt.Sprintf("flag not registered or not bool: %s: %v", name, err))
	}
	return v
}
