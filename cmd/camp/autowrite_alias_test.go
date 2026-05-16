package main

import (
	"reflect"
	"testing"
)

func TestNormalizeAutoWriteAlias(t *testing.T) {
	got := normalizeAutoWriteAlias([]string{"camp", "p", "commit", "-aw", "-m", "msg", "-aw=false"})
	want := []string{"camp", "p", "commit", "--auto-write", "-m", "msg", "-aw=false"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeAutoWriteAlias() = %#v, want %#v", got, want)
	}
}
