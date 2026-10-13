package main

import "runtime/debug"

// buildVersion is set by release builds: -ldflags "-X main.buildVersion=v0.2.0".
var buildVersion string

// version is the release build's version, else the module version go install
// recorded, else the commit a local build came from.
func version() string {
	if buildVersion != "" {
		return buildVersion
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	return versionOf(info)
}

func versionOf(info *debug.BuildInfo) string {
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	var rev, dirty string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				dirty = "-dirty"
			}
		}
	}
	if rev == "" {
		return "devel"
	}
	return "devel " + rev[:min(12, len(rev))] + dirty
}
