package ui

import (
	"testing"
)

func TestValidateCommitMessage(t *testing.T) {
	tests := []struct {
		name    string
		message string
		wantErr bool
	}{
		{
			name:    "valid message",
			message: "Add new feature",
			wantErr: false,
		},
		{
			name:    "empty message",
			message: "",
			wantErr: true,
		},
		{
			name:    "whitespace only",
			message: "   ",
			wantErr: true,
		},
		{
			name:    "newlines only",
			message: "\n\n",
			wantErr: true,
		},
		{
			name:    "tabs only",
			message: "\t\t",
			wantErr: true,
		},
		{
			name:    "message with leading whitespace",
			message: "   Fix bug",
			wantErr: false,
		},
		{
			name:    "message with trailing whitespace",
			message: "Fix bug   ",
			wantErr: false,
		},
		{
			name:    "multiline message",
			message: "Fix bug\n\nDetailed description here",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCommitMessage(tt.message)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateCommitMessage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
