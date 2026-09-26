# Deplexo CLI

`deplexo` lets you sign in to Deplexo, list apps, create apps from Git repositories, rebuild existing apps, and read deployment history and logs. You can link a local directory to an app so you do not need to pass its UUID each time. The CLI is written in Go and uses the public API.

See the [Deplexo docs](https://docs.deplexo.com) for platform documentation.

The CLI is under development. Builds target Linux, macOS, and Windows on amd64 and arm64. Each target needs passing native keyring and filesystem tests before it can be listed as supported in a release. You can build and test the code without production credentials or private repositories.

## Install

Visit [the CLI site](https://cli.deplexo.com/) for installation commands and examples. The current beta is `v0.1.0-beta.3`; native testing is incomplete, so check the release notes for your platform. To install the beta on Linux or macOS:

```sh
curl -fsSL https://cli.deplexo.com/install.sh | sh
```

On Windows, run this in PowerShell:

```powershell
& ([scriptblock]::Create((Invoke-RestMethod -ErrorAction Stop 'https://cli.deplexo.com/install.ps1')))
```

The installers download the selected version for your OS and CPU, verify its SHA-256 checksum, and install to `~/.local/bin` or `%LOCALAPPDATA%\Deplexo\bin`. Run the installer again to update, or use `deplexo upgrade` once you have beta.3 or newer. Set `DEPLEXO_INSTALL_DIR` on Linux/macOS or pass `-InstallDir` to the downloaded PowerShell script to use another directory. No administrator access is needed for the default destination.

On Linux and macOS, if the install directory is missing from PATH, the installer prints a command to activate it in your current terminal. For Bash and zsh, it asks before adding that setting to your shell configuration for future terminals. It respects zsh's `ZDOTDIR` and macOS Bash login files. CI and redirected output skip this prompt; set `DEPLEXO_NO_MODIFY_PATH=1` to skip it yourself. Fish users get a `fish_add_path` command. On Windows, add the installation directory to your user PATH if needed.

The default selects the latest published stable release. Until a stable release exists, it selects the newest published beta. Release candidates require an explicit version. Set `DEPLEXO_VERSION` or use PowerShell’s `-Version` to pin a release; PowerShell also accepts `DEPLEXO_VERSION`. You can [read the scripts](site/) before running them or download a versioned archive from [GitHub Releases](https://github.com/Deplexo/cli/releases). Pin a version in CI.

If an existing zsh installation reports `command not found`, activate the default install directory in your current terminal:

```sh
export PATH="$HOME/.local/bin:$PATH"
deplexo auth login
```

Use your chosen directory if you set `DEPLEXO_INSTALL_DIR`. Rerun the installer in a fresh terminal and accept its PATH prompt to configure future sessions. A piped installer cannot change the PATH of the terminal that launched it.

## Updates

`deplexo upgrade` checks for a newer release and asks before replacing the executable. Use `deplexo upgrade --check` to check without installing, or `deplexo upgrade --yes` for an approved unattended upgrade. The updater verifies the archive checksum and the executable’s version and platform before replacing the installed file. It does not use your Deplexo credentials.

After a successful interactive command, the CLI checks for updates at most once per day. If one is available, it asks `Upgrade now? [y/N]`; Enter skips reminders for 24 hours. Failed checks are cached too. Help, version, completion, JSON output, redirected output, `--no-input`, and CI skip automatic checks and prompts. Set `DEPLEXO_NO_UPDATE_CHECK=1` to disable them. Development builds do not update themselves.

If this copy was installed by a package manager or its directory is not writable, use that package manager or the original installer. On Windows, an interrupted replacement may leave a `.deplexo.exe-previous-*.exe` recovery copy beside the executable. Rerun the installer if the executable is missing; the next successful upgrade verifies and removes the recorded backup after older processes have exited.

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

Sign-in requests `profile:read app:read app:deploy app:restart logs:read` by default. Use `--read-only` for read access. `--scopes` replaces the defaults with the scopes you specify and must include `profile:read` for a stored sign-in. To start, stop, or delete apps, also request `app:start`, `app:stop`, or `app:delete` as needed.

The CLI stores credentials in Linux Secret Service, macOS Keychain, or Windows Credential Manager. Unlock your keyring before running a command; the Linux adapter will not open an unlock prompt. macOS Keychain can request desktop approval, so `--no-input` on macOS requires `DEPLEXO_TOKEN` or `--insecure-storage`.

To use a plaintext credential file, pass `--insecure-storage` on every command that needs it. File permissions, or a Windows ACL, restrict access to your account. The CLI will not switch to file storage unless you request it.

For automation, [create an API key](https://deplexo.com/api-keys) with the permissions you need, then save it as `DEPLEXO_TOKEN` in your CI secret manager. The CLI uses it before any stored sign-in and never saves or refreshes it. If the token is invalid, the command fails without trying another account. Tokens are not accepted as command-line flags. Unset `DEPLEXO_TOKEN` before signing in or out interactively.

## Apps and logs

```sh
deplexo apps list
deplexo apps create --name example --repo https://github.com/example/app
deplexo link --app 11111111-1111-4111-8111-111111111111
deplexo apps get
deplexo deploy
deplexo deployments list
deplexo deployments logs 22222222-2222-4222-8222-222222222222
deplexo logs --follow
deplexo logs --follow --json
deplexo apps stop --yes
deplexo apps start
deplexo unlink
```

Commands use the app UUID from `--app`, or from `.deplexo.json` in the current directory when the flag is omitted. `link` checks your access before writing that file. The file contains only a format version and app UUID; it cannot change the account or API origin.

`apps list` shows the apps available to your account, with their names, statuses, and UUIDs. `--json` returns an `apps` array without account or plan information.

`deploy` rebuilds an existing app from its recorded source; Git apps use the latest source. It returns the new deployment UUID and status after the server accepts the request. It does not wait for the build to finish. Use `deployments logs <deployment-uuid>` to inspect the build. The public API calls this operation `restart` and requires `app:restart`. Sign in again if your saved session lacks that scope. A process-only restart and local directory/ZIP uploads are not available as CLI commands.

`apps create` creates a new app and its first deployment, then prints both UUIDs. If the response leaves the outcome unclear, the CLI stops without retrying. Check the dashboard before trying again. Stopping an app, cancelling a deployment, and deleting an app require `--yes`; stopping the app does not mean cancelling its current deployment.

`logs --follow` requests new runtime logs using the server's resume cursor and filters repeated lines by their recent event IDs. It passes the cursor back unchanged and stops after `--timeout`, which defaults to 30 minutes. Build log commands return a snapshot for the deployment UUID you request.

## Configuration and output

`--origin` and `--profile` override `DEPLEXO_ORIGIN` and `DEPLEXO_PROFILE`, then user settings and defaults. The defaults are `https://deplexo.com` and profile `default`. Profiles separate credentials and process locks. User settings live in `deplexo/settings.json` under the OS user configuration directory:

```json
{ "version": 1, "origin": "https://deplexo.com", "profile": "default" }
```

Results go to stdout; pairing instructions and errors go to stderr. `--json` writes JSON, or one JSON object per line when following logs. Human output escapes terminal controls. Human output uses aligned tables, labeled details, and colored status text. Tables switch to stacked fields when the terminal is too narrow. Colors are automatic on terminals and disabled when output is redirected. Use `--color always` to request colors explicitly or `--color never` to disable them. A nonempty `NO_COLOR` or `TERM=dumb` disables colors in every mode. JSON and completion scripts remain free of formatting codes; log contents are not recolored. No animations are used. Help, version, and shell completion work offline.

| Exit code | Meaning                 |
| --------- | ----------------------- |
| 0         | Success                 |
| 1         | Operation failed        |
| 2         | Invalid usage           |
| 3         | Sign-in required        |
| 4         | Insufficient permission |
| 130       | Interrupted             |

## API gaps

A process-only restart, deployment status polling without logs, and build log streaming still need public API contracts. Local directory and ZIP deployment also remain unimplemented until the upload and existing-app deployment contracts are published together. These features are not registered as commands. `apps list` reads the public `/me` inventory, `apps create --repo` uses the creation endpoint, and `deploy` uses the documented rebuild endpoint at `/apps/{id}/restart`. The CLI uses neither browser cookies nor private RPCs.

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

Release Please prepares a version and changelog PR from Conventional Commits. Merging that PR creates an immutable tag and a draft release, then explicitly dispatches release verification at the tag. The packaging tool checks that the tag, release manifest and clean source commit agree. Stable releases and release candidates require all six native platform jobs to pass, including uncached keyring and installer tests. Tags ending in `-beta.N` may publish cross-compiled prereleases after build, test, lint and vulnerability checks; their release notes must state which native checks remain pending. Both paths attach checksums and provenance and require approval through the protected `release` environment. An existing published release cannot be overwritten by this workflow.

The `release` environment needs required reviewers and `v*` tag restrictions. Release runners need Bash, `gh` and `sha256sum`; native runners need an isolated unlocked keyring. GitHub Actions must be allowed to create release PRs; set the repository variable `RELEASE_PRS_ENABLED=true` after that permission is available. Version automation stays disabled until then. If verification is interrupted, dispatch `release.yml` again at the same tag. Only draft assets can be replaced on a retry. The Pages workflow publishes `site/` from `main` using a self-hosted Linux runner. It generates `latest-version` from published GitHub releases; this file is ignored locally and is not a second source of version numbers. Release publication, edits, and deletion dispatch a refresh at `main`, preserving the Pages environment’s branch restriction.

## Version policy

The first beta is `v0.1.0-beta.1`; the planned first stable release is `v0.1.0`. Release tags use `vMAJOR.MINOR.PATCH`, with prerelease suffixes such as `-beta.1` or `-rc.1`. Invalid SemVer identifiers are rejected. Public release tags exclude build metadata so package managers never have to distinguish two releases with the same precedence.

During 0.x development, fixes increment patch; features and breaking changes increment minor. From 1.0 onward, incompatible changes increment major. The compatibility contract covers command names, flags, defaults, exit codes, JSON/JSONL fields and configuration formats. Human-readable tables are for people; scripts should use `--json`. At 1.x, removal follows a documented deprecation in an earlier minor release, except urgent security fixes with migration notes.

Release Please owns `.release-please-manifest.json`; the tag identifies each published build. `deplexo version --json` reports version, source commit, OS and architecture. Local checkouts report `dev` or `dev-dirty`, while versioned `go install` builds use Go's module build information. Betas and release candidates are marked as prereleases. The default installer prefers stable releases and falls back to betas only while no stable release exists. Release notes need a wording and compatibility review before publication.

Licensed under Apache-2.0. Dependencies retain their own licenses, included in packaged artifacts.
