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

func BetaTag(tag string) bool {
	_, pre, _ := strings.Cut(tag, "-")
	parts := strings.Split(pre, ".")
	return ValidTag(tag) && len(parts) == 2 && parts[0] == "beta" && parts[1] != "" && strings.Trim(parts[1], "0123456789") == ""
}

// Newer compares release tags by SemVer precedence without integer overflow.
func Newer(candidate, current string) bool {
	if !ValidTag(candidate) || !ValidTag(current) {
		return false
	}
	a, ap, _ := strings.Cut(candidate[1:], "-")
	b, bp, _ := strings.Cut(current[1:], "-")
	numeric := func(a, b string) int {
		if len(a) != len(b) {
			return len(a) - len(b)
		}
		return strings.Compare(a, b)
	}
	aa, bb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range aa {
		if cmp := numeric(aa[i], bb[i]); cmp != 0 {
			return cmp > 0
		}
	}
	if ap == "" || bp == "" {
		return ap == "" && bp != ""
	}
	aa, bb = strings.Split(ap, "."), strings.Split(bp, ".")
	for i := 0; i < min(len(aa), len(bb)); i++ {
		an, bn := strings.Trim(aa[i], "0123456789") == "", strings.Trim(bb[i], "0123456789") == ""
		if an != bn {
			return !an
		}
		cmp := strings.Compare(aa[i], bb[i])
		if an {
			cmp = numeric(aa[i], bb[i])
		}
		if cmp != 0 {
			return cmp > 0
		}
	}
	return len(aa) > len(bb)
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
