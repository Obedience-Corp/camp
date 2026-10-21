package main

import (
	"github.com/Obedience-Corp/camp/internal/config"
	"testing"
)

func TestSelectSettingsCampaign(t *testing.T) {
	reg := &config.Registry{Campaigns: map[string]config.RegisteredCampaign{
		"alpha-id": {ID: "alpha-id", Name: "alpha", Path: "/alpha"},
		"beta-id":  {ID: "beta-id", Name: "beta", Path: "/beta"},
		"other-id": {ID: "other-id", Name: "beta", Path: "/other"},
	}}
	for _, tc := range []struct{ query, want string }{
		{"alpha", "alpha-id"}, {"beta-id", "beta-id"}, {"beta", ""}, {"alph", ""}, {"missing", ""}, {"", ""},
	} {
		t.Run(tc.query, func(t *testing.T) {
			got, err := selectSettingsCampaign(reg, tc.query)
			if tc.want == "" {
				if err == nil {
					t.Fatal("expected rejection")
				}
				return
			}
			if err != nil || got.ID != tc.want {
				t.Fatalf("got %v, %v; want %s", got, err, tc.want)
			}
		})
	}
}
