package tasks

import (
	"reflect"
	"testing"
)

func TestGoTestArgsHonorsBuildTags(t *testing.T) {
	t.Setenv("BUILD_TAGS", "dev")

	got := goTestArgs("./cmd/camp")
	want := []string{
		"test",
		"-count=1",
		"-json",
		"-short",
		"-timeout",
		"120s",
		"-tags",
		"dev",
		"./cmd/camp",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("goTestArgs() = %#v, want %#v", got, want)
	}
}

func TestGoTestArgsOmitsEmptyBuildTags(t *testing.T) {
	t.Setenv("BUILD_TAGS", " ")

	got := goTestArgs("./cmd/camp")
	want := []string{
		"test",
		"-count=1",
		"-json",
		"-short",
		"-timeout",
		"120s",
		"./cmd/camp",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("goTestArgs() = %#v, want %#v", got, want)
	}
}

func TestGoListArgsHonorsBuildTags(t *testing.T) {
	t.Setenv("BUILD_TAGS", "dev")

	got := goListArgs("-f", "{{.TestGoFiles}}", "./cmd/camp/quest")
	want := []string{
		"list",
		"-tags",
		"dev",
		"-f",
		"{{.TestGoFiles}}",
		"./cmd/camp/quest",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("goListArgs() = %#v, want %#v", got, want)
	}
}
