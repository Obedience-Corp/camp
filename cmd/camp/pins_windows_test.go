//go:build windows

package main

import (
	"context"
	"testing"
)

// These cases exercise Windows filepath.Rel results in the owning pin helper.
// Cancel attachment detection before filesystem access so this is a pure path
// classification test, including UNC paths that must not trigger a network probe.
func TestBuildPinForPathWindowsContainment(t *testing.T) {
	const root = `C:\camp-pin-containment-fixture\camp`
	const uncRoot = `\\camp-pin-fixture\share\camp`
	cases := []struct {
		name, root, target, wantPath string
		wantError                    bool
	}{
		{name: "root", root: root, target: root, wantPath: "."},
		{name: "child", root: root, target: root + `\docs`, wantPath: "docs"},
		{name: "nested", root: root, target: root + `\docs\notes`, wantPath: `docs\notes`},
		{name: "case insensitive root", root: root, target: `c:\CAMP-PIN-CONTAINMENT-FIXTURE\CAMP\docs`, wantPath: "docs"},
		{name: "dot prefix is a name", root: root, target: root + `\..named`, wantPath: "..named"},
		{name: "parent", root: root, target: `C:\camp-pin-containment-fixture`, wantError: true},
		{name: "sibling traversal", root: root, target: `C:\camp-pin-containment-fixture\outside`, wantError: true},
		{name: "nested traversal", root: root, target: root + `\..\outside\notes`, wantError: true},
		{name: "forward slash traversal", root: root, target: `C:/camp-pin-containment-fixture/outside`, wantError: true},
		{name: "different volume", root: root, target: `D:\camp-pin-containment-fixture\camp\docs`, wantError: true},
		{name: "UNC child", root: uncRoot, target: uncRoot + `\docs`, wantPath: "docs"},
		{name: "UNC sibling traversal", root: uncRoot, target: `\\camp-pin-fixture\share\outside`, wantError: true},
		{name: "different UNC share", root: uncRoot, target: `\\camp-pin-fixture\other\camp`, wantError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			pin, display, err := buildPinForPath(ctx, tc.target, tc.root)
			if tc.wantError {
				if err == nil {
					t.Fatalf("outside path accepted: pin=%+v display=%q", pin, display)
				}
				if pin.Path != "" || pin.AbsPath != "" || display != "" {
					t.Fatalf("refused path returned a usable pin: %+v display=%q", pin, display)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if pin.Path != tc.wantPath || pin.AbsPath != "" || display != tc.wantPath {
				t.Fatalf("pin=%+v display=%q, want relative %q", pin, display, tc.wantPath)
			}
		})
	}
}
