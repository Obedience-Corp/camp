package main

import "testing"

func TestShortenRemoteURL(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"https://github.com/Obedience-Corp/camp.git", "Obedience-Corp/camp"},
		{"https://github.com/Obedience-Corp/camp", "Obedience-Corp/camp"},
		{"git@github.com:Obedience-Corp/camp.git", "Obedience-Corp/camp"},
		{"git@github.com:Obedience-Corp/camp", "Obedience-Corp/camp"},
		{"https://gitlab.com/org/repo.git", "https://gitlab.com/org/repo"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := shortenRemoteURL(tt.input); got != tt.want {
			t.Errorf("shortenRemoteURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
