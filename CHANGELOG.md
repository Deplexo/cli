# Changelog

Versions follow [Semantic Versioning](https://semver.org/).

## 0.1.0-beta.3

- Install with the plain curl or PowerShell command. It selects the latest stable release, or the newest beta while no stable release exists. Version pinning remains available for CI.
- Set up PATH with an optional Bash or zsh configuration prompt. The shell installer prints an activation command and a full-path sign-in command so you can use the installed binary immediately.
- Upgrade with `deplexo upgrade`, or accept the update prompt after a successful interactive command. Checks and skipped reminders are cached for 24 hours; scripts, offline commands, and redirected output skip automatic prompts.
- Verify update checksums and executable identity before replacement. Interrupted or failed downloads leave the installed executable unchanged.

Native verification outside Linux ARM64 remains pending, including Windows executable replacement and PowerShell installation.

## 0.1.0-beta.2

- List your apps with `deplexo apps list`, including names, statuses, and UUIDs.
- Rebuild an existing app with `deplexo deploy --app <uuid>`, or use the app linked to your directory. The command reports the queued deployment ID; it does not wait for the build to finish.
- Read app details and deployment history in aligned tables and labeled fields. Terminal output colors statuses and help headings, with `--color` and `NO_COLOR` controls. JSON and completion scripts keep their existing formatting.
- New sign-ins request `app:restart` for rebuilding existing apps. Existing sessions need a new sign-in to grant this permission.

Native verification remains incomplete outside Linux ARM64. This beta does not add a process-only restart or local source uploads.

## 0.1.0-beta.1

The first beta of the Deplexo CLI. Builds target Linux, macOS, and Windows on amd64 and arm64. Native verification is incomplete; see the release notes for the checks completed on each platform.

- Sign in through your browser with a pairing code, or use a scoped API key in CI. Interactive sign-in stores credentials in your OS keyring; plaintext file storage requires an explicit flag.
- Create apps from Git repositories, inspect app details, and start, stop, or delete apps. Link a working directory to an app to reuse its ID across commands.
- Read deployment history and build-log snapshots, cancel deployments, and follow runtime logs.
- Use JSON output for commands and one JSON object per line when following logs. Help, version information, and shell completion work offline.
- Install through the shell or PowerShell installer, which selects a release for your OS and CPU and verifies its checksum.
