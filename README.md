# Deplexo CLI

`deplexo` lets you sign in to Deplexo, create apps from Git repositories, control apps, and read deployment history and logs. You can link a local directory to an app so you do not need to pass its UUID each time. The CLI is written in Go and uses the public API.

The CLI is under development. Builds target Linux, macOS, and Windows on amd64 and arm64. Each target needs passing native keyring and filesystem tests before it can be listed as supported in a release. You can build and test the code without production credentials or private repositories.

## Install

Visit [the CLI site](https://deplexo.github.io/cli/) for installation commands and examples. On Linux or macOS:

```sh
curl -fsSL https://deplexo.github.io/cli/install.sh | sh
```

On Windows, run this in PowerShell:

```powershell
& ([scriptblock]::Create((Invoke-RestMethod -ErrorAction Stop 'https://deplexo.github.io/cli/install.ps1')))
```

The installers select the latest published stable release for your OS and CPU, verify its SHA-256 checksum, and install to `~/.local/bin` or `%LOCALAPPDATA%\Deplexo\bin`. Add that directory to your PATH if needed. Run the same command again to update. Set `DEPLEXO_INSTALL_DIR` on Linux/macOS or pass `-InstallDir` to the downloaded PowerShell script to use another directory. No administrator access is needed for the default destination.

Installation requires a published stable release; it does not use development artifacts or release candidates. You can [read the scripts](site/) before running them or download a versioned archive from [GitHub Releases](https://github.com/Deplexo/cli/releases). Pin a version in CI.

## Build

Install Go 1.26.6 or later, then run:

```sh
go build -trimpath -o bin/deplexo ./cmd/deplexo
./bin/deplexo --help
```

On Windows, use `-o bin/deplexo.exe`. These commands put the executable in this repository's `bin` directory and leave any installed copy alone.

## Sign in

```sh
deplexo auth login
deplexo whoami
deplexo auth status
deplexo auth logout
```

Sign-in prints the verification URL and pairing code, then asks you to press Enter before opening your browser. Type `n` to continue manually. Use `--no-browser` to skip that prompt. `--no-input` and redirected input also skip prompts and browser opening. Ordinary commands never start sign-in.

Sign-in requests `profile:read app:read app:deploy logs:read` by default. Use `--read-only` for read access. `--scopes` replaces the defaults with the scopes you specify and must include `profile:read` for a stored sign-in. To start, stop, or delete apps, also request `app:start`, `app:stop`, or `app:delete` as needed.

The CLI stores credentials in Linux Secret Service, macOS Keychain, or Windows Credential Manager. Unlock your keyring before running a command; the Linux adapter will not open an unlock prompt. macOS Keychain can request desktop approval, so `--no-input` on macOS requires `DEPLEXO_TOKEN` or `--insecure-storage`.

To use a plaintext credential file, pass `--insecure-storage` on every command that needs it. File permissions, or a Windows ACL, restrict access to your account. The CLI will not switch to file storage unless you request it.

For automation, set `DEPLEXO_TOKEN` to a scoped API key through your CI secret manager. The CLI uses it before any stored sign-in and never saves or refreshes it. If the token is invalid, the command fails without trying another account. Tokens are not accepted as command-line flags. Unset `DEPLEXO_TOKEN` before signing in or out interactively.

## Apps and logs

```sh
deplexo apps create --name example --repo https://github.com/example/app
deplexo link --app 11111111-1111-4111-8111-111111111111
deplexo apps get
deplexo deployments list
deplexo deployments logs 22222222-2222-4222-8222-222222222222
deplexo logs --follow
deplexo logs --follow --json
deplexo apps stop --yes
deplexo apps start
deplexo unlink
```

Commands use the app UUID from `--app`, or from `.deplexo.json` in the current directory when the flag is omitted. `link` checks your access before writing that file. The file contains only a format version and app UUID; it cannot change the account or API origin.

`apps create` creates a new app and its first deployment, then prints both UUIDs. If the response leaves the outcome unclear, the CLI stops without retrying. Check the dashboard before trying again. Stopping an app, cancelling a deployment, and deleting an app require `--yes`; stopping the app does not mean cancelling its current deployment.

`logs --follow` requests new runtime logs using the server's resume cursor and filters repeated lines by their recent event IDs. It passes the cursor back unchanged and stops after `--timeout`, which defaults to 30 minutes. Build log commands return a snapshot for the deployment UUID you request.

## Configuration and output

`--origin` and `--profile` override `DEPLEXO_ORIGIN` and `DEPLEXO_PROFILE`, then user settings and defaults. The defaults are `https://deplexo.com` and profile `default`. Profiles separate credentials and process locks. User settings live in `deplexo/settings.json` under the OS user configuration directory:

```json
{ "version": 1, "origin": "https://deplexo.com", "profile": "default" }
```

Results go to stdout; pairing instructions and errors go to stderr. `--json` writes JSON, or one JSON object per line when following logs. Human output escapes terminal controls. The CLI emits no colors or animations, including when `NO_COLOR` is set. Help, version, and shell completion work offline.

| Exit code | Meaning                 |
| --------- | ----------------------- |
| 0         | Success                 |
| 1         | Operation failed        |
| 2         | Invalid usage           |
| 3         | Sign-in required        |
| 4         | Insufficient permission |
| 130       | Interrupted             |

## API gaps

App listing, deployment to an existing app with `app:deploy`, deployment status polling without logs, and build log streaming still need confirmed public API contracts. Local directory and ZIP deployment also remain unimplemented until the upload and existing-app deployment contracts are published together. These features are not registered as commands. `apps create --repo` uses the documented creation endpoint. The CLI uses neither browser cookies nor private RPCs.

## Verification and workflows

```sh
go build ./...
go vet ./...
go test ./...
go test -race ./...
golangci-lint run --timeout 5m
govulncheck ./...
go run scripts/package.go
```

The packaging script writes an archive and checksum, with dependency licenses included in the archive. When the target matches the build host, it extracts the packaged executable and checks its version and help output.

All workflows use self-hosted runners. CI runs on pushes to `main` or when started manually, then builds archives for all six targets. Fork pull requests do not trigger it automatically. Review contributions before running them on a trusted branch; use disposable, isolated runners for untrusted code so it cannot access organizational resources. CI needs no production secrets.

The native workflow selects a runner by its `self-hosted`, OS, and architecture labels. Runners need Bash (Git Bash on Windows) and an isolated, unlocked native keyring. Targets that support Go's race detector also need a C compiler supported by Go. Windows ARM64 runs ordinary tests because Go does not support race tests there. Set `DEPLEXO_TEST_NATIVE_KEYRING=1` to test keyring storage locally. The test saves, reads, and deletes its own uniquely named entry. Skipping it leaves native credential storage unverified.

Release Please prepares a version and changelog PR from Conventional Commits. Merging that PR creates an immutable tag and a draft release, then explicitly dispatches release verification at the tag. The packaging tool checks that the tag, release manifest and clean source commit agree. All six native platform jobs must pass, including uncached keyring and installer tests, before checksums, archives and provenance are attached to the draft. A reviewer publishes the verified draft after the protected `release` environment approval. An existing published release cannot be overwritten by this workflow.

The `release` environment needs required reviewers and `v*` tag restrictions. Release runners need Bash, `gh` and `sha256sum`; native runners need an isolated unlocked keyring. GitHub Actions must be allowed to create release PRs; set the repository variable `RELEASE_PRS_ENABLED=true` after that permission is available. Version automation stays disabled until then. If verification is interrupted, dispatch `release.yml` again at the same tag. Only draft assets can be replaced on a retry. The separate Pages workflow publishes the static `site/` directory from `main` using a self-hosted Linux runner.

## Version policy

The first release is `v0.1.0`. Release tags use `vMAJOR.MINOR.PATCH`; release candidates use a suffix such as `-rc.1`. Invalid SemVer identifiers are rejected. Public release tags exclude build metadata so package managers never have to distinguish two releases with the same precedence.

During 0.x development, fixes increment patch; features and breaking changes increment minor. From 1.0 onward, incompatible changes increment major. The compatibility contract covers command names, flags, defaults, exit codes, JSON/JSONL fields and configuration formats. Human-readable tables are for people; scripts should use `--json`. At 1.x, removal follows a documented deprecation in an earlier minor release, except urgent security fixes with migration notes.

Release Please owns `.release-please-manifest.json`; the tag identifies each published build. `deplexo version --json` reports version, source commit, OS and architecture. Local checkouts report `dev` or `dev-dirty`, while versioned `go install` builds use Go's module build information. Release candidates are marked as prereleases and excluded from the installer's stable channel. Release notes need a wording and compatibility review before publication.

Licensed under Apache-2.0. Dependencies retain their own licenses, included in packaged artifacts.
