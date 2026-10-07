package intent

import (
	"path/filepath"
	"testing"
)

func TestNoteFolderFromPath(t *testing.T) {
	notesRoot := filepath.Join("/camp-root", ".campaign", "intents", "notes")
	tests := []struct {
		name    string
		path    string
		want    string
		wantErr bool
	}{
		{name: "root", path: filepath.Join(notesRoot, "a.md"), want: ""},
		{name: "reserved", path: filepath.Join(notesRoot, "meetings", "a.md"), want: "meetings"},
		{name: "nested", path: filepath.Join(notesRoot, "reading", "papers", "a.md"), want: "reading/papers"},
		{name: "outside", path: filepath.Join("/camp-root", ".campaign", "intents", "inbox", "a.md"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := noteFolderFromPath(notesRoot, tt.path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("noteFolderFromPath(%q) = %q, want error", tt.path, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("noteFolderFromPath(%q): %v", tt.path, err)
			}
			if got != tt.want {
				t.Fatalf("noteFolderFromPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
			if status := noteFolderStatus(got); !status.IsNote() {
				t.Fatalf("noteFolderStatus(%q) = %q, want a note status", got, status)
			}
		})
	}
}
