// Package version validates release identities and identifies local builds.
package version

import (
	"regexp"
	"runtime/debug"
	"strings"
)

var semantic = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+)(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

// Valid accepts SemVer 2.0.0 without a tag prefix.
func Valid(value string) bool {
	if !semantic.MatchString(value) {
		return false
	}
	core, _, _ := strings.Cut(value, "+")
	_, prerelease, _ := strings.Cut(core, "-")
	for _, part := range strings.Split(prerelease, ".") {
		if len(part) > 1 && part[0] == '0' && strings.Trim(part, "0123456789") == "" {
			return false
		}
	}
	return true
}

// ValidTag excludes build metadata from public releases so package managers
// never have to distinguish releases with identical SemVer precedence.
func ValidTag(tag string) bool {
	return strings.HasPrefix(tag, "v") && Valid(tag[1:]) && !strings.Contains(tag, "+")
}

func Current(value, commit string) (string, string) {
	info, _ := debug.ReadBuildInfo()
	return fromBuild(value, commit, info)
}

func fromBuild(value, commit string, info *debug.BuildInfo) (string, string) {
	if value == "" {
		value = "dev"
	}
	if commit == "" {
		commit = "unknown"
	}
	if info == nil || value != "dev" {
		return value, commit
	}
	modified := false
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" && commit == "unknown" {
			commit = setting.Value
		}
		if setting.Key == "vcs.modified" && setting.Value == "true" {
			modified = true
		}
	}
	// Module versions identify go install builds. Local checkouts remain dev,
	// even when their last commit happens to carry a release tag.
	if strings.HasPrefix(info.Main.Version, "v") && Valid(strings.TrimPrefix(info.Main.Version, "v")) && !modified {
		value = info.Main.Version
	}
	if modified {
		value = "dev-dirty"
	}
	return value, commit
}
