package workitem

import (
	"reflect"
	"testing"
)

func TestListOptions_FilterOptionsNormalizesTagsAndProjects(t *testing.T) {
	opts := listOptions{
		tags:     []string{"Public Launch", "public-launch"},
		projects: []string{"projects/camp/", "projects/./camp"},
	}
	fo, err := opts.filterOptions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []string{"public-launch"}; !reflect.DeepEqual(fo.Tags, want) {
		t.Errorf("Tags = %#v, want %#v", fo.Tags, want)
	}
	if want := []string{"projects/camp"}; !reflect.DeepEqual(fo.Projects, want) {
		t.Errorf("Projects = %#v, want %#v", fo.Projects, want)
	}
}

func TestListOptions_FilterOptionsRejectsUnnormalizableTag(t *testing.T) {
	opts := listOptions{tags: []string{"foo!bar"}}
	if _, err := opts.filterOptions(); err == nil {
		t.Fatal("expected error for a tag that is invalid after normalization")
	}
}

func TestListOptions_FilterOptionsRejectsEmptyProject(t *testing.T) {
	opts := listOptions{projects: []string{"/"}}
	if _, err := opts.filterOptions(); err == nil {
		t.Fatal("expected error for a project that is empty after normalization")
	}
}

func TestListOptions_FilterOptionsEmptyIsNoError(t *testing.T) {
	fo, err := listOptions{}.filterOptions()
	if err != nil {
		t.Fatalf("empty options should not error: %v", err)
	}
	if len(fo.Tags) != 0 || len(fo.Projects) != 0 {
		t.Errorf("empty options should yield no tag/project filters, got tags=%v projects=%v", fo.Tags, fo.Projects)
	}
}
