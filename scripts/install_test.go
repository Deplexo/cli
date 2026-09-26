package scripts

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestShellInstaller(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell installer is for Linux and macOS")
	}
	for _, scenario := range []string{"install", "update", "beta", "pinned-stable", "invalid-pinned", "newline-pinned", "prerelease-latest", "corrupt", "missing-checksum", "duplicate-checksum", "no-release", "invalid-version", "unsupported-cpu", "bad-binary", "destination-directory"} {
		t.Run(scenario, func(t *testing.T) {
			tag, version := "v0.1.0", ""
			switch scenario {
			case "beta":
				tag, version = "v0.1.0-beta.1", "v0.1.0-beta.1"
			case "pinned-stable":
				version = tag
			case "invalid-pinned":
				version = "v0.1.0-beta.01"
			case "newline-pinned":
				version = "v0.1.0\nv0.2.0"
			}
			root := t.TempDir()
			bin := filepath.Join(root, "commands")
			dest := filepath.Join(root, "installed bin")
			for _, dir := range []string{bin, dest} {
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			write := func(path, text string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(text), 0700); err != nil {
					t.Fatal(err)
				}
			}
			old := "previous installed binary\n"
			target := filepath.Join(dest, "deplexo")
			if scenario == "destination-directory" {
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
			} else if scenario != "install" {
				write(target, old)
			}
			binary := "#!/bin/sh\nprintf 'deplexo v0.1.0 (linux/arm64)\\n'\n"
			if scenario == "bad-binary" {
				binary = "#!/bin/sh\nexit 1\n"
			}
			archive := filepath.Join(root, "fixture.tar.gz")
			file, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}
			gz := gzip.NewWriter(file)
			tw := tar.NewWriter(gz)
			if err := tw.WriteHeader(&tar.Header{Name: "deplexo", Size: int64(len(binary)), Mode: 0755}); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write([]byte(binary)); err != nil {
				t.Fatal(err)
			}
			for _, close := range []func() error{tw.Close, gz.Close, file.Close} {
				if err := close(); err != nil {
					t.Fatal(err)
				}
			}
			data, err := os.ReadFile(archive)
			if err != nil {
				t.Fatal(err)
			}
			checksum := fmt.Sprintf("%x  deplexo_%s_linux_arm64.tar.gz\n", sha256.Sum256(data), tag)
			switch scenario {
			case "corrupt":
				checksum = strings.Repeat("0", 64) + "  deplexo_v0.1.0_linux_arm64.tar.gz\n"
			case "missing-checksum":
				checksum = ""
			case "duplicate-checksum":
				checksum += checksum
			}
			write(filepath.Join(root, "checksums"), checksum)
			write(filepath.Join(bin, "uname"), "#!/bin/sh\nif [ \"$1\" = '-s' ]; then echo Linux; elif [ \"$SCENARIO\" = 'unsupported-cpu' ]; then echo mips; else echo aarch64; fi\n")
			write(filepath.Join(bin, "curl"), `#!/bin/sh
output=; url=; effective=false
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) output=$2; shift ;;
    --write-out) effective=true; shift ;;
    https://*) url=$1 ;;
  esac
  shift
done
if [ "$effective" = true ]; then
  [ "$SCENARIO" != no-release ] || exit 22
  [ "$SCENARIO" != beta ] && [ "$SCENARIO" != pinned-stable ] || exit 23
  if [ "$SCENARIO" = invalid-version ]; then echo https://github.com/Deplexo/cli/releases/tag/v01.0.0
  elif [ "$SCENARIO" = prerelease-latest ]; then echo https://github.com/Deplexo/cli/releases/tag/v0.1.0-beta.1
  else echo https://github.com/Deplexo/cli/releases/tag/v0.1.0; fi
elif [ "${url##*/}" = SHA256SUMS ]; then
  [ "$url" = "https://github.com/Deplexo/cli/releases/download/$EXPECTED_TAG/SHA256SUMS" ] || exit 24
  cp "$FIXTURE/checksums" "$output"
else
  [ "$url" = "https://github.com/Deplexo/cli/releases/download/$EXPECTED_TAG/deplexo_${EXPECTED_TAG}_linux_arm64.tar.gz" ] || exit 25
  cp "$FIXTURE/fixture.tar.gz" "$output"
fi
`)
			cmd := exec.Command("sh", "../site/install.sh")
			cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "DEPLEXO_INSTALL_DIR="+dest, "DEPLEXO_VERSION="+version, "EXPECTED_TAG="+tag, "TMPDIR="+root, "FIXTURE="+root, "SCENARIO="+scenario)
			out, err := cmd.CombinedOutput()
			success := scenario == "install" || scenario == "update" || scenario == "beta" || scenario == "pinned-stable"
			if success && err != nil || !success && err == nil {
				t.Fatalf("unexpected result %v: %s", err, out)
			}
			if scenario != "destination-directory" {
				got, err := os.ReadFile(target)
				if err != nil {
					t.Fatal(err)
				}
				want := old
				if success {
					want = binary
				}
				if string(got) != want {
					t.Fatal("installer did not preserve or replace binary as expected")
				}
			}
			for _, pattern := range []string{filepath.Join(root, "deplexo-install.*"), filepath.Join(dest, ".deplexo.*")} {
				left, _ := filepath.Glob(pattern)
				if len(left) != 0 {
					t.Fatalf("temporary files left: %v", left)
				}
			}
		})
	}
}

func TestWindowsInstaller(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires native Windows file replacement and PowerShell")
	}
	root := t.TempDir()
	fixture := filepath.Join(root, "deplexo.exe")
	build := exec.Command("go", "build", "-ldflags=-X main.version=v0.1.0", "-o", fixture, "../cmd/deplexo")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build installer fixture: %v %s", err, out)
	}
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "fixture.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	entry, err := z.Create("deplexo.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	packed, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("../site/install.ps1")
	if err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	for _, scenario := range []string{"install", "update", "beta", "pinned-stable", "invalid-pinned", "prerelease-latest", "wrong-tag", "corrupt", "missing-checksum", "no-release"} {
		t.Run(scenario, func(t *testing.T) {
			tag, version := "v0.1.0", ""
			switch scenario {
			case "beta":
				tag, version = "v0.1.0-beta.1", "v0.1.0-beta.1"
			case "pinned-stable":
				version = tag
			case "invalid-pinned":
				version = "v0.1.0-beta.01"
			case "wrong-tag":
				version = "v0.2.0"
			}
			dir := t.TempDir()
			dest := filepath.Join(dir, "[installed] bin")
			if err := os.Mkdir(dest, 0700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(dest, "deplexo.exe")
			old := []byte("old installation")
			if scenario != "install" {
				if err := os.WriteFile(target, old, 0600); err != nil {
					t.Fatal(err)
				}
			}
			checksum := fmt.Sprintf("%x  deplexo_%s_windows_%s.zip\n", sha256.Sum256(packed), tag, runtime.GOARCH)
			switch scenario {
			case "corrupt":
				checksum = strings.Repeat("0", 64) + "  deplexo_v0.1.0_windows_" + runtime.GOARCH + ".zip\n"
			case "missing-checksum":
				checksum = ""
			}
			checksums := filepath.Join(dir, "checksums")
			if err := os.WriteFile(checksums, []byte(checksum), 0600); err != nil {
				t.Fatal(err)
			}
			harness := "$ErrorActionPreference = 'Stop'\n"
			harness += "function Invoke-RestMethod { param($Uri, $TimeoutSec) "
			apiPath := "latest"
			if version != "" {
				apiPath = "tags/" + version
			}
			harness += "if ($Uri -cne " + quote("https://api.github.com/repos/Deplexo/cli/releases/"+apiPath) + ") { throw 'wrong release endpoint' }; "
			if scenario == "no-release" {
				harness += "throw 'no release'"
			} else {
				prerelease := "$false"
				if scenario == "beta" || scenario == "prerelease-latest" {
					prerelease = "$true"
				}
				harness += "return @{tag_name=" + quote(tag) + "; draft=$false; prerelease=" + prerelease + "}"
			}
			harness += " }\nfunction Invoke-WebRequest { param([switch]$UseBasicParsing,$Uri,$OutFile,$TimeoutSec)\n"
			harness += "if ($Uri.EndsWith('/SHA256SUMS')) { Copy-Item -LiteralPath " + quote(checksums) + " -Destination $OutFile } else { Copy-Item -LiteralPath " + quote(archive) + " -Destination $OutFile }\n}\n"
			harness += "& " + quote(script) + " -InstallDir " + quote(dest)
			if version != "" {
				harness += " -Version " + quote(version)
			}
			path := filepath.Join(dir, "check.ps1")
			if err := os.WriteFile(path, []byte(harness), 0600); err != nil {
				t.Fatal(err)
			}
			temp := filepath.Join(dir, "[temporary] files")
			if err := os.Mkdir(temp, 0700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", path)
			cmd.Env = append(os.Environ(), "TEMP="+temp, "TMP="+temp, "DEPLEXO_VERSION=")
			out, err := cmd.CombinedOutput()
			success := scenario == "install" || scenario == "update" || scenario == "beta" || scenario == "pinned-stable"
			if success && err != nil || !success && err == nil {
				t.Fatalf("unexpected result: %v %s", err, out)
			}
			got, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			want := old
			if success {
				want = data
			}
			if string(got) != string(want) {
				t.Fatal("installer did not preserve or replace the executable correctly")
			}
		})
	}
}
