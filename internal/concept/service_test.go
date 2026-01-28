package concept

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/obediencecorp/camp/internal/config"
)

// testPaths returns CampaignPaths for testing.
func testPaths() config.CampaignPaths {
	return config.CampaignPaths{
		Projects:  "projects",
		Festivals: "festivals",
		Intents:   "workflow/intents",
	}
}

// testFS returns a mock filesystem for testing.
func testFS() fstest.MapFS {
	return fstest.MapFS{
		"projects/camp/file":            &fstest.MapFile{Data: []byte("")},
		"projects/fest/file":            &fstest.MapFile{Data: []byte("")},
		"projects/.hidden/file":         &fstest.MapFile{Data: []byte("")},
		"festivals/active/file":         &fstest.MapFile{Data: []byte("")},
		"festivals/planned/file":        &fstest.MapFile{Data: []byte("")},
		"festivals/completed/file":      &fstest.MapFile{Data: []byte("")},
		"workflow/intents/inbox/file":   &fstest.MapFile{Data: []byte("")},
		"workflow/intents/active/file":  &fstest.MapFile{Data: []byte("")},
	}
}

func TestFSService_List(t *testing.T) {
	tests := []struct {
		name      string
		paths     config.CampaignPaths
		wantCount int
		wantNames []string
	}{
		{
			name:      "returns concepts from paths",
			paths:     testPaths(),
			wantCount: 3,                                         // projects, festivals, intents
			wantNames: []string{"festivals", "intents", "projects"}, // sorted alphabetically
		},
		{
			name:      "empty paths",
			paths:     config.CampaignPaths{},
			wantCount: 0,
			wantNames: nil,
		},
		{
			name: "partial paths",
			paths: config.CampaignPaths{
				Projects: "projects",
			},
			wantCount: 1,
			wantNames: []string{"projects"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewFSService("/test", tt.paths, testFS())

			got, err := svc.List(context.Background())
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}

			if len(got) != tt.wantCount {
				t.Errorf("List() returned %d concepts, want %d", len(got), tt.wantCount)
			}

			for i, wantName := range tt.wantNames {
				if i < len(got) && got[i].Name != wantName {
					t.Errorf("List()[%d].Name = %q, want %q", i, got[i].Name, wantName)
				}
			}
		})
	}
}

func TestFSService_ListItems(t *testing.T) {
	fsys := fstest.MapFS{
		"projects/camp/main.go":  &fstest.MapFile{Data: []byte("package main")},
		"projects/fest/main.go":  &fstest.MapFile{Data: []byte("package main")},
		"projects/.hidden/file":  &fstest.MapFile{Data: []byte("")},
		"projects/camp/cmd":      &fstest.MapFile{Mode: 0o755},
		"projects/camp/internal": &fstest.MapFile{Mode: 0o755},
	}

	paths := config.CampaignPaths{
		Projects: "projects",
	}

	tests := []struct {
		name        string
		conceptName string
		subpath     string
		wantCount   int
		wantErr     bool
	}{
		{
			name:        "list top level projects",
			conceptName: "projects",
			subpath:     "",
			wantCount:   2, // camp, fest (not .hidden)
			wantErr:     false,
		},
		{
			name:        "list with subpath",
			conceptName: "projects",
			subpath:     "camp",
			wantCount:   3, // cmd, internal, main.go
			wantErr:     false,
		},
		{
			name:        "unknown concept",
			conceptName: "unknown",
			subpath:     "",
			wantCount:   0,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewFSService("/test", paths, fsys)

			got, err := svc.ListItems(context.Background(), tt.conceptName, tt.subpath)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ListItems() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && len(got) != tt.wantCount {
				t.Errorf("ListItems() returned %d items, want %d", len(got), tt.wantCount)
			}
		})
	}
}

func TestFSService_ListItems_SkipsHiddenFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"projects/.git/config":  &fstest.MapFile{Data: []byte("")},
		"projects/.hidden":      &fstest.MapFile{Mode: 0o755},
		"projects/visible":      &fstest.MapFile{Mode: 0o755},
		"projects/visible/file": &fstest.MapFile{Data: []byte("")},
	}

	paths := config.CampaignPaths{
		Projects: "projects",
	}

	svc := NewFSService("/test", paths, fsys)

	items, err := svc.ListItems(context.Background(), "projects", "")
	if err != nil {
		t.Fatalf("ListItems() error = %v", err)
	}

	if len(items) != 1 {
		t.Errorf("ListItems() should return 1 visible item, got %d", len(items))
	}

	if len(items) > 0 && items[0].Name != "visible" {
		t.Errorf("ListItems()[0].Name = %q, want %q", items[0].Name, "visible")
	}
}

func TestFSService_ListItems_SortsDirectoriesFirst(t *testing.T) {
	fsys := fstest.MapFS{
		"projects/zfile.txt":      &fstest.MapFile{Data: []byte("content")},
		"projects/adir/child.txt": &fstest.MapFile{Data: []byte("")},
		"projects/bfile.txt":      &fstest.MapFile{Data: []byte("content")},
	}

	paths := config.CampaignPaths{
		Projects: "projects",
	}

	svc := NewFSService("/test", paths, fsys)

	items, err := svc.ListItems(context.Background(), "projects", "")
	if err != nil {
		t.Fatalf("ListItems() error = %v", err)
	}

	// Should be: adir (dir), bfile.txt (file), zfile.txt (file)
	if len(items) != 3 {
		t.Fatalf("ListItems() returned %d items, want 3", len(items))
	}

	// First item should be directory
	if !items[0].IsDir {
		t.Errorf("First item should be a directory, got IsDir=%v for %q", items[0].IsDir, items[0].Name)
	}
	if items[0].Name != "adir" {
		t.Errorf("First item should be 'adir', got %q", items[0].Name)
	}

	// Files should be sorted alphabetically after directories
	if items[1].Name != "bfile.txt" || items[2].Name != "zfile.txt" {
		t.Errorf("Files not sorted correctly: got %v", items)
	}
}

func TestFSService_Get(t *testing.T) {
	paths := testPaths()

	tests := []struct {
		name        string
		conceptName string
		wantErr     bool
		wantPath    string
	}{
		{
			name:        "existing concept - projects",
			conceptName: "projects",
			wantErr:     false,
			wantPath:    "projects",
		},
		{
			name:        "existing concept - festivals",
			conceptName: "festivals",
			wantErr:     false,
			wantPath:    "festivals",
		},
		{
			name:        "existing concept - intents",
			conceptName: "intents",
			wantErr:     false,
			wantPath:    "workflow/intents",
		},
		{
			name:        "unknown concept",
			conceptName: "unknown",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewFSService("/test", paths, testFS())

			got, err := svc.Get(context.Background(), tt.conceptName)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Get() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if got.Path != tt.wantPath {
					t.Errorf("Get().Path = %q, want %q", got.Path, tt.wantPath)
				}
			}
		})
	}
}

func TestFSService_Resolve(t *testing.T) {
	paths := testPaths()

	tests := []struct {
		name        string
		conceptName string
		item        string
		wantPath    string
		wantErr     bool
	}{
		{
			name:        "concept only",
			conceptName: "projects",
			item:        "",
			wantPath:    "/test/projects",
			wantErr:     false,
		},
		{
			name:        "concept with item",
			conceptName: "projects",
			item:        "camp",
			wantPath:    "/test/projects/camp",
			wantErr:     false,
		},
		{
			name:        "unknown concept",
			conceptName: "unknown",
			item:        "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewFSService("/test", paths, testFS())

			got, err := svc.Resolve(context.Background(), tt.conceptName, tt.item)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Resolve() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && got != tt.wantPath {
				t.Errorf("Resolve() = %q, want %q", got, tt.wantPath)
			}
		})
	}
}

func TestFSService_ResolvePath(t *testing.T) {
	fsys := fstest.MapFS{
		"projects/camp/sub/file.txt": &fstest.MapFile{Data: []byte("")},
		"projects/file.txt":          &fstest.MapFile{Data: []byte("content")},
	}

	paths := testPaths()

	tests := []struct {
		name     string
		path     string
		wantName string
		wantDir  bool
		wantErr  bool
	}{
		{
			name:     "valid directory",
			path:     "projects/camp",
			wantName: "camp",
			wantDir:  true,
			wantErr:  false,
		},
		{
			name:     "valid file",
			path:     "projects/file.txt",
			wantName: "file.txt",
			wantDir:  false,
			wantErr:  false,
		},
		{
			name:    "non-existent path",
			path:    "projects/nonexistent",
			wantErr: true,
		},
		{
			name:    "empty path",
			path:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewFSService("/test", paths, fsys)

			got, err := svc.ResolvePath(context.Background(), tt.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResolvePath() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if got.Name != tt.wantName {
					t.Errorf("ResolvePath().Name = %q, want %q", got.Name, tt.wantName)
				}
				if got.IsDir != tt.wantDir {
					t.Errorf("ResolvePath().IsDir = %v, want %v", got.IsDir, tt.wantDir)
				}
			}
		})
	}
}

func TestFSService_ConceptForPath(t *testing.T) {
	paths := testPaths()

	tests := []struct {
		name        string
		path        string
		wantConcept string
		wantErr     bool
	}{
		{
			name:        "path within projects",
			path:        "projects/camp",
			wantConcept: "projects",
			wantErr:     false,
		},
		{
			name:        "exact concept path",
			path:        "projects",
			wantConcept: "projects",
			wantErr:     false,
		},
		{
			name:        "path within festivals",
			path:        "festivals/active/some-fest",
			wantConcept: "festivals",
			wantErr:     false,
		},
		{
			name:        "path within intents",
			path:        "workflow/intents/inbox/task1",
			wantConcept: "intents",
			wantErr:     false,
		},
		{
			name:    "path not in any concept",
			path:    "other/random/path",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewFSService("/test", paths, testFS())

			got, err := svc.ConceptForPath(context.Background(), tt.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ConceptForPath() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && got.Name != tt.wantConcept {
				t.Errorf("ConceptForPath().Name = %q, want %q", got.Name, tt.wantConcept)
			}
		})
	}
}

func TestFSService_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	svc := NewFSService("/test", testPaths(), testFS())

	t.Run("List", func(t *testing.T) {
		_, err := svc.List(ctx)
		if err == nil {
			t.Error("List() should return error for cancelled context")
		}
	})

	t.Run("ListItems", func(t *testing.T) {
		_, err := svc.ListItems(ctx, "projects", "")
		if err == nil {
			t.Error("ListItems() should return error for cancelled context")
		}
	})

	t.Run("Get", func(t *testing.T) {
		_, err := svc.Get(ctx, "projects")
		if err == nil {
			t.Error("Get() should return error for cancelled context")
		}
	})

	t.Run("Resolve", func(t *testing.T) {
		_, err := svc.Resolve(ctx, "projects", "")
		if err == nil {
			t.Error("Resolve() should return error for cancelled context")
		}
	})

	t.Run("ResolvePath", func(t *testing.T) {
		_, err := svc.ResolvePath(ctx, "projects")
		if err == nil {
			t.Error("ResolvePath() should return error for cancelled context")
		}
	})

	t.Run("ConceptForPath", func(t *testing.T) {
		_, err := svc.ConceptForPath(ctx, "projects")
		if err == nil {
			t.Error("ConceptForPath() should return error for cancelled context")
		}
	})
}

func TestFSService_ListItems_ChildrenCount(t *testing.T) {
	fsys := fstest.MapFS{
		"projects/empty/.gitkeep":      &fstest.MapFile{Data: []byte("")},
		"projects/hasthree/one/file":   &fstest.MapFile{Data: []byte("")},
		"projects/hasthree/two/file":   &fstest.MapFile{Data: []byte("")},
		"projects/hasthree/three/file": &fstest.MapFile{Data: []byte("")},
	}

	paths := config.CampaignPaths{
		Projects: "projects",
	}

	svc := NewFSService("/test", paths, fsys)

	items, err := svc.ListItems(context.Background(), "projects", "")
	if err != nil {
		t.Fatalf("ListItems() error = %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("ListItems() returned %d items, want 2", len(items))
	}

	// Find the items by name
	var emptyDir, hasThreeDir *Item
	for i := range items {
		switch items[i].Name {
		case "empty":
			emptyDir = &items[i]
		case "hasthree":
			hasThreeDir = &items[i]
		}
	}

	if emptyDir == nil || hasThreeDir == nil {
		t.Fatal("Expected to find 'empty' and 'hasthree' directories")
	}

	// Empty has only hidden file, so Children should be 0
	if emptyDir.Children != 0 {
		t.Errorf("empty directory Children = %d, want 0", emptyDir.Children)
	}

	// hasthree has three visible subdirectories
	if hasThreeDir.Children != 3 {
		t.Errorf("hasthree directory Children = %d, want 3", hasThreeDir.Children)
	}
}

func TestIsHidden(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{".hidden", true},
		{".git", true},
		{"visible", false},
		{"file.txt", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isHidden(tt.name); got != tt.want {
				t.Errorf("isHidden(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestHasPathPrefix(t *testing.T) {
	tests := []struct {
		path   string
		prefix string
		want   bool
	}{
		{"projects/camp", "projects", true},
		{"projects/camp/sub", "projects", true},
		{"projects", "projects", false},   // Equal, not prefix
		{"projectsx", "projects", false},  // Different path
		{"other/path", "projects", false}, // No match
	}

	for _, tt := range tests {
		t.Run(tt.path+"_"+tt.prefix, func(t *testing.T) {
			if got := hasPathPrefix(tt.path, tt.prefix); got != tt.want {
				t.Errorf("hasPathPrefix(%q, %q) = %v, want %v", tt.path, tt.prefix, got, tt.want)
			}
		})
	}
}

func TestFSService_List_HasItemsDetection(t *testing.T) {
	fsys := fstest.MapFS{
		"projects/camp/file":  &fstest.MapFile{Data: []byte("")},
		"festivals/.hidden":   &fstest.MapFile{Mode: 0o755}, // Only hidden directory
	}

	paths := config.CampaignPaths{
		Projects:  "projects",
		Festivals: "festivals",
	}

	svc := NewFSService("/test", paths, fsys)

	concepts, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	// Find concepts by name
	var projects, festivals *Concept
	for i := range concepts {
		switch concepts[i].Name {
		case "projects":
			projects = &concepts[i]
		case "festivals":
			festivals = &concepts[i]
		}
	}

	if projects == nil || festivals == nil {
		t.Fatal("Expected both concepts")
	}

	// projects has a visible directory
	if !projects.HasItems {
		t.Error("projects.HasItems should be true")
	}

	// festivals only has hidden directory
	if festivals.HasItems {
		t.Error("festivals.HasItems should be false (only hidden items)")
	}
}
