package pathutil

import (
	"errors"
	"testing"
)

func TestValidateSubmodulePath(t *testing.T) {
	repoRoot := "/home/user/campaign"

	tests := []struct {
		name    string
		subPath string
		wantErr bool
	}{
		{name: "simple submodule", subPath: "projects/camp", wantErr: false},
		{name: "nested submodule", subPath: "projects/obey/core", wantErr: false},
		{name: "single component", subPath: "camp", wantErr: false},
		{name: "with hyphen", subPath: "projects/my-proj", wantErr: false},

		{name: "dot prefix valid", subPath: "./projects", wantErr: false},
		{name: "trailing dot valid", subPath: "projects/.", wantErr: false},

		{name: "dot alone", subPath: ".", wantErr: true},
		{name: "dot slash", subPath: "./", wantErr: true},
		{name: "dot slash dot", subPath: "./.", wantErr: true},
		{name: "multi dot", subPath: "././.", wantErr: true},
		{name: "absolute path", subPath: "/etc/passwd", wantErr: true},
		{name: "dotdot alone", subPath: "..", wantErr: true},
		{name: "dotdot at start", subPath: "../etc", wantErr: true},
		{name: "dotdot in middle", subPath: "projects/../../../etc", wantErr: true},
		{name: "dotdot at end", subPath: "projects/camp/..", wantErr: true},
		{name: "empty string", subPath: "", wantErr: true},
		{name: "double dotdot", subPath: "../..", wantErr: true},
		{name: "embedded escape", subPath: "projects/camp/../../..", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSubmodulePath(repoRoot, tc.subPath)
			if tc.wantErr && err == nil {
				t.Errorf("ValidateSubmodulePath(%q, %q): expected error, got nil", repoRoot, tc.subPath)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("ValidateSubmodulePath(%q, %q): expected no error, got: %v", repoRoot, tc.subPath, err)
			}
			if tc.wantErr && err != nil && !errors.Is(err, ErrInvalidSubmodulePath) {
				t.Errorf("expected ErrInvalidSubmodulePath, got: %v", err)
			}
		})
	}
}

func TestValidateSubmodulePath_PathNeedNotExist(t *testing.T) {
	repoRoot := "/nonexistent/root/that/does/not/exist"
	subPath := "projects/new-module"

	err := ValidateSubmodulePath(repoRoot, subPath)
	if err != nil {
		t.Errorf("expected no error for valid non-existent path, got: %v", err)
	}
}
