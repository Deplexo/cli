# Deplexo CLI

`deplexo` is a Go client for Deplexo's public API. It supports device sign-in, account checks, app creation from Git, app controls, project association, deployment history, and logs.

This repository is under development. The build workflow produces Linux, macOS, and Windows archives for amd64 and arm64. Native keyring and filesystem tests must pass on each target before a release claims support for that target. No production credentials or private repositories are needed to build or test it.

## Build

Install Go 1.26 or later, then run:

```sh
go build -trimpath -o bin/deplexo ./cmd/deplexo
./bin/deplexo --help
```

On Windows, use `-o bin/deplexo.exe`. Build outputs stay in this repository; the build does not replace an installed executable.

## Sign in

```sh
deplexo auth login
deplexo whoami
deplexo auth status
deplexo auth logout
```

Sign-in prints the verification URL and pairing code before opening your browser. Use `--no-browser` to open the URL yourself. `--no-input` disables CLI prompts and automatic browser opening. Ordinary commands never start sign-in.

The default scopes are `profile:read app:read app:deploy logs:read`. Use `--read-only` for read access, or `--scopes` to supply the complete scope set. Stored sign-ins require `profile:read`. App start, stop, and delete need their respective scopes; request those explicitly when needed.

Credentials use Linux Secret Service, macOS Keychain, or Windows Credential Manager. Unlock the keyring before use. Linux does not open a desktop unlock prompt. On macOS, `--no-input` requires `DEPLEXO_TOKEN` or explicit file storage because the Keychain command can request desktop approval. File storage requires an explicit `--insecure-storage` on each invocation. It stores plaintext with owner-only permissions or a Windows owner-only ACL. The CLI never silently falls back to a file.

For automation, supply a scoped API key through your CI secret manager as `DEPLEXO_TOKEN`. It takes precedence over stored sign-ins and is never saved or refreshed. An invalid injected token fails without switching accounts. There is no token command-line flag. Unset `DEPLEXO_TOKEN` before interactive sign-in or sign-out.

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

App selection uses `--app`, then `.deplexo.json` in the current directory. Linking checks access before writing the file. The project file holds only a format version and app UUID. It cannot select an account or API origin.

`apps create` always creates an app and its first deployment. It prints both UUIDs. Creation is not retried after an ambiguous response; check the dashboard before retrying. App stop, cancellation, and deletion require `--yes`. Stopping an app differs from cancelling its current deployment.

Runtime `logs --follow` polls the documented cursor API, passes resume cursors unchanged, and deduplicates recent stable event IDs. It stops after `--timeout` (30 minutes by default). Build logs are snapshots for the requested deployment UUID.

## Configuration and output

`--origin` and `--profile` override `DEPLEXO_ORIGIN` and `DEPLEXO_PROFILE`, then user settings and defaults. The defaults are `https://deplexo.com` and profile `default`. Profiles separate credentials and process locks. User settings live in `deplexo/settings.json` under the OS user configuration directory:

```json
{"version":1,"origin":"https://deplexo.com","profile":"default"}
```

Results go to stdout; pairing instructions and errors go to stderr. `--json` writes JSON, or one JSON object per line when following logs. Human output escapes terminal controls. The CLI emits no colors or animations, including when `NO_COLOR` is set. Help, version, and shell completion work offline.

| Exit code | Meaning |
| --- | --- |
| 0 | Success |
| 1 | Operation failed |
| 2 | Invalid usage |
| 3 | Sign-in required |
| 4 | Insufficient permission |
| 130 | Interrupted |

## API gaps

The CLI does not expose proposed endpoints. Dedicated app listing, deployment to an existing app with `app:deploy`, metadata-only deployment polling, and public build-log streaming need confirmed public contracts. Local-directory and ZIP deployment are not implemented until the upload and existing-app deployment contracts are published together. `apps create --repo` uses the documented creation endpoint. The CLI does not use browser cookies or private RPCs.

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

Packaging creates an archive, checksum, and third-party license notices. When the target matches the build host, it extracts and runs the actual packaged executable to check version and help.

All workflows use self-hosted runners. CI tests trusted `main` pushes and builds six target archives. It also supports manual runs. It deliberately has no automatic fork-PR trigger: fork code must not execute on a persistent runner with access to organizational resources. Review contributions before running them on a trusted branch; use disposable, isolated runners for untrusted changes. CI needs no production secrets.

The native workflow selects a runner by its `self-hosted`, OS, and architecture labels. Runners need Go-compatible C tooling for the race detector, Bash (Git Bash on Windows), and an isolated unlocked native keyring. Set `DEPLEXO_TEST_NATIVE_KEYRING=1` to run the native round-trip test locally. The test creates and deletes its own uniquely named entry. A skipped native test is not evidence of platform support.

The release workflow runs for a version tag pointing to a commit on `main`, verifies source, builds archives, and creates a draft GitHub release with checksums and provenance. Configure required reviewers on the `release` environment and provision isolated release runners before using it. Review native test results for every advertised target before publishing the draft. The release runner also needs `gh` and `sha256sum`.

Licensed under Apache-2.0. Dependencies retain their own licenses, included in packaged artifacts.
