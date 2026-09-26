package scripts

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestShellInstaller(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell installer is for Linux and macOS")
	}
	for _, scenario := range []string{"install", "update", "beta", "latest-beta", "pinned-stable", "invalid-pinned", "newline-pinned", "rc-latest", "corrupt", "missing-checksum", "duplicate-checksum", "no-release", "invalid-version", "unsupported-cpu", "bad-binary", "destination-directory", "colon-directory", "newline-directory", "control-directory"} {
		t.Run(scenario, func(t *testing.T) {
			tag, version := "v0.1.0", ""
			switch scenario {
			case "beta":
				tag, version = "v0.1.0-beta.1", "v0.1.0-beta.1"
			case "latest-beta":
				tag = "v0.1.0-beta.2"
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
			switch scenario {
			case "colon-directory":
				dest += ":other"
			case "newline-directory":
				dest += "\nother"
			case "control-directory":
				dest += "\tother"
			}
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
output=; url=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) output=$2; shift ;;
    https://*) url=$1 ;;
  esac
  shift
done
if [ "$url" = "https://cli.deplexo.com/latest-version" ]; then
  [ "$SCENARIO" != no-release ] || exit 22
  [ "$SCENARIO" != beta ] && [ "$SCENARIO" != pinned-stable ] || exit 23
  if [ "$SCENARIO" = invalid-version ]; then echo v01.0.0
  elif [ "$SCENARIO" = rc-latest ]; then echo v0.1.0-rc.1
  elif [ "$SCENARIO" = latest-beta ]; then echo v0.1.0-beta.2
  else echo v0.1.0; fi
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
			success := scenario == "install" || scenario == "update" || scenario == "beta" || scenario == "latest-beta" || scenario == "pinned-stable"
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

func TestShellInstallerPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell installer is for Linux and macOS")
	}
	data, err := os.ReadFile("../site/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	// Run the actual onboarding function without downloading a second fixture.
	definitions, _, ok := strings.Cut(string(data), "\n# A complete function")
	if !ok {
		t.Fatal("installer entrypoint marker missing")
	}
	for _, scenario := range []string{"zsh-yes", "zsh-repeat", "zsh-decline", "zsh-default", "zsh-no-tty", "zsh-optout", "zsh-ci", "zsh-symlink", "zsh-directory", "zsh-missing-parent", "zsh-zdotdir", "bash-linux", "bash-mac", "bash-login", "bash-profile", "fish", "unknown", "already-on-path", "quoted-path"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			destination := filepath.Join(root, "installed bin")
			if scenario == "quoted-path" {
				destination += "'\"$`!\\literal"
			}
			shell, targetOS, config := "/bin/zsh", "linux", filepath.Join(root, ".zshrc")
			extra := []string{}
			switch scenario {
			case "zsh-zdotdir", "zsh-missing-parent":
				dir := filepath.Join(root, "zsh config")
				if scenario == "zsh-zdotdir" {
					if err := os.Mkdir(dir, 0700); err != nil {
						t.Fatal(err)
					}
				}
				extra = append(extra, "ZDOTDIR="+dir)
				config = filepath.Join(dir, ".zshrc")
			case "bash-linux":
				shell, config = "/bin/bash", filepath.Join(root, ".bashrc")
			case "bash-mac", "bash-login", "bash-profile":
				shell, targetOS, config = "/bin/bash", "darwin", filepath.Join(root, ".bash_profile")
				if scenario == "bash-login" {
					config = filepath.Join(root, ".bash_login")
				}
				if scenario == "bash-profile" {
					config = filepath.Join(root, ".profile")
				}
			case "fish":
				shell = "/bin/fish"
			case "unknown":
				shell = ""
			case "zsh-optout":
				extra = append(extra, "DEPLEXO_NO_MODIFY_PATH=1")
			case "zsh-ci":
				extra = append(extra, "CI=true")
			}
			initial := "# Existing user configuration\n"
			if scenario == "bash-login" || scenario == "bash-profile" || scenario == "zsh-repeat" {
				if err := os.WriteFile(config, []byte(initial), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "zsh-directory" {
				if err := os.Mkdir(config, 0700); err != nil {
					t.Fatal(err)
				}
			}
			linked := filepath.Join(root, "linked-config")
			if scenario == "zsh-symlink" {
				if err := os.WriteFile(linked, []byte(initial), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(linked, config); err != nil {
					t.Fatal(err)
				}
			}
			path := os.Getenv("PATH")
			if scenario == "already-on-path" {
				path = destination + ":" + path
			}
			body := definitions + "\ndestination=$TEST_DESTINATION\ntarget_os=$TEST_OS\nSHELL=$TEST_SHELL\nsetup_path\n"
			if scenario == "zsh-repeat" {
				body += "setup_path\n"
			}
			scriptPath := filepath.Join(root, "path-test.sh")
			if err := os.WriteFile(scriptPath, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			var cmd *exec.Cmd
			if scenario == "zsh-no-tty" {
				cmd = exec.CommandContext(ctx, "sh", scriptPath)
			} else {
				if _, err := exec.LookPath("script"); err != nil {
					t.Skip("PTY regression needs the script utility")
				}
				if runtime.GOOS == "darwin" {
					cmd = exec.CommandContext(ctx, "script", "-q", "/dev/null", "sh", scriptPath)
				} else {
					cmd = exec.CommandContext(ctx, "script", "-q", "-e", "-c", "cat '"+strings.ReplaceAll(scriptPath, "'", "'\\''")+"' | sh", "/dev/null")
				}
			}
			cmd.Env = append(os.Environ(), "HOME="+root, "SHELL=/bin/sh", "TEST_SHELL="+shell, "ZDOTDIR="+root, "CI=", "DEPLEXO_NO_MODIFY_PATH=", "TEST_DESTINATION="+destination, "TEST_OS="+targetOS, "PATH="+path)
			cmd.Env = append(cmd.Env, extra...)
			answer := "y\n"
			if scenario == "zsh-decline" {
				answer = "n\n"
			}
			if scenario == "zsh-default" {
				answer = "\n"
			}
			cmd.Stdin = strings.NewReader(answer)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("onboarding failed: %v: %s", err, out)
			}
			modified := scenario == "zsh-yes" || scenario == "zsh-repeat" || scenario == "zsh-zdotdir" || strings.HasPrefix(scenario, "bash-") || scenario == "quoted-path"
			contents, readErr := os.ReadFile(config)
			if modified {
				if readErr != nil {
					t.Fatalf("expected shell config: %v: %s", readErr, out)
				}
				if strings.Count(string(contents), "# Deplexo CLI") != 1 {
					t.Fatalf("missing or duplicated entry: %s", contents)
				}
				if scenario == "zsh-repeat" && !strings.HasPrefix(string(contents), initial) {
					t.Fatal("existing configuration changed")
				}
				check := exec.Command("sh", "-c", ". \"$1\"; printf '%s' \"$PATH\"", "sh", config)
				check.Env = append(os.Environ(), "PATH="+path)
				got, err := check.CombinedOutput()
				if err != nil || string(got) != destination+":"+path {
					t.Fatalf("unsafe or incorrect PATH setting: %q %v", got, err)
				}
			} else if readErr == nil && string(contents) != initial {
				t.Fatalf("configuration changed without consent: %s", contents)
			}
			if scenario == "zsh-symlink" {
				got, err := os.ReadFile(linked)
				if err != nil || string(got) != initial {
					t.Fatal("symlink target changed")
				}
			}
			if scenario != "already-on-path" && !strings.Contains(string(out), "auth login") {
				t.Fatalf("missing full-path login instructions: %s", out)
			}
			if scenario == "fish" && !strings.Contains(string(out), "fish_add_path -- '") {
				t.Fatalf("missing fish activation: %s", out)
			}
			for _, skipped := range []string{"zsh-ci", "zsh-no-tty", "zsh-optout"} {
				if scenario == skipped && strings.Contains(string(out), "[y/N]") {
					t.Fatalf("unexpected prompt: %s", out)
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
	for _, scenario := range []string{"install", "update", "beta", "latest-beta", "pinned-stable", "invalid-pinned", "rc-latest", "wrong-tag", "corrupt", "missing-checksum", "no-release"} {
		t.Run(scenario, func(t *testing.T) {
			tag, version := "v0.1.0", ""
			switch scenario {
			case "beta":
				tag, version = "v0.1.0-beta.1", "v0.1.0-beta.1"
			case "latest-beta":
				tag = "v0.1.0-beta.2"
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
			harness += "if ($Uri -cne 'https://cli.deplexo.com/latest-version') { throw 'wrong release endpoint' }; "
			if version != "" {
				harness += "throw 'pinned install looked up latest'"
			} else if scenario == "no-release" {
				harness += "throw 'no release'"
			} else if scenario == "rc-latest" {
				harness += "return 'v0.1.0-rc.1'"
			} else {
				harness += "return " + quote(tag)
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
			success := scenario == "install" || scenario == "update" || scenario == "beta" || scenario == "latest-beta" || scenario == "pinned-stable"
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
