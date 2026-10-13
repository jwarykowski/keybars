package main

import (
	"runtime/debug"
	"testing"
)

func TestBuildVersionWins(t *testing.T) {
	defer func(v string) { buildVersion = v }(buildVersion)
	buildVersion = "v9.9.9"
	if got := version(); got != "v9.9.9" {
		t.Errorf("got %q, want v9.9.9", got)
	}
}

func TestVersionOf(t *testing.T) {
	rev := debug.BuildSetting{Key: "vcs.revision", Value: "32d50371a2b4c5d6e7f8"}
	dirty := debug.BuildSetting{Key: "vcs.modified", Value: "true"}
	for _, tc := range []struct {
		name string
		info debug.BuildInfo
		want string
	}{
		{"go install", debug.BuildInfo{Main: debug.Module{Version: "v0.2.0"}, Settings: []debug.BuildSetting{rev}}, "v0.2.0"},
		{"local build", debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{rev}}, "devel 32d50371a2b4"},
		{"uncommitted changes", debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{rev, dirty}}, "devel 32d50371a2b4-dirty"},
		{"no vcs", debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, "devel"},
	} {
		if got := versionOf(&tc.info); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
