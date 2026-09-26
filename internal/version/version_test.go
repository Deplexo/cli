package version

import (
	"runtime/debug"
	"testing"
)

func TestSemVer(t *testing.T) {
	for _, value := range []string{"0.1.0", "1.0.0-rc.1", "2.3.4-alpha-beta.0", "1.0.0+build.001", "1.0.0-0+build", "999999999999999999999.0.0"} {
		if !Valid(value) {
			t.Errorf("rejected %q", value)
		}
	}
	for _, value := range []string{"", "v1.0.0", "01.0.0", "1.01.0", "1.0.01", "1.0", "1.0.0-01", "1.0.0-rc..1", "1.0.0-", "1.0.0+a..b", "1.0.0+", "1.0.0\n", "1.0.0-ä"} {
		if Valid(value) {
			t.Errorf("accepted %q", value)
		}
	}
	for _, value := range []string{"v0.1.0", "v1.0.0-rc.1"} {
		if !ValidTag(value) {
			t.Errorf("rejected tag %q", value)
		}
	}
	for _, value := range []string{"1.0.0", "v01.0.0", "v1.0.0+build", "dev", "v1.0.0-01"} {
		if ValidTag(value) {
			t.Errorf("accepted tag %q", value)
		}
	}
}

func TestBuildIdentity(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}, Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc123"}}}
	if v, c := fromBuild("dev", "unknown", info); v != "v0.1.0" || c != "abc123" {
		t.Fatalf("module build: %s %s", v, c)
	}
	info.Main.Version = "(devel)"
	if v, _ := fromBuild("dev", "unknown", info); v != "dev" {
		t.Fatal("local build reported a release")
	}
	info.Settings = append(info.Settings, debug.BuildSetting{Key: "vcs.modified", Value: "true"})
	if v, _ := fromBuild("dev", "unknown", info); v != "dev-dirty" {
		t.Fatal("dirty build not identified")
	}
	if v, c := fromBuild("v1.0.0", "release-commit", info); v != "v1.0.0" || c != "release-commit" {
		t.Fatal("linker identity overwritten")
	}
}

func TestSemVerUpdatePrecedence(t *testing.T) {
	ordered := []string{"v1.0.0-alpha", "v1.0.0-alpha.1", "v1.0.0-alpha.beta", "v1.0.0-beta", "v1.0.0-beta.2", "v1.0.0-beta.11", "v1.0.0-rc.1", "v1.0.0", "v1.0.1", "v2.0.0", "v999999999999999999999.0.0"}
	for i, current := range ordered {
		for j, candidate := range ordered {
			if Newer(candidate, current) != (j > i) {
				t.Errorf("precedence %s > %s", candidate, current)
			}
		}
	}
	for _, invalid := range []string{"dev", "v01.0.0", "v1.0.0-beta.01", "v1.0.0\n"} {
		if Newer(invalid, "v0.0.1") || Newer("v1.0.0", invalid) {
			t.Errorf("accepted invalid tag %q", invalid)
		}
	}
}
