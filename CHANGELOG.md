# Changelog

Versions follow [Semantic Versioning](https://semver.org/).

## 0.1.0-beta.1

The first beta of the Deplexo CLI. Builds target Linux, macOS, and Windows on amd64 and arm64. Native verification is incomplete; see the release notes for the checks completed on each platform.

- Sign in through your browser with a pairing code, or use a scoped API key in CI. Interactive sign-in stores credentials in your OS keyring; plaintext file storage requires an explicit flag.
- Create apps from Git repositories, inspect app details, and start, stop, or delete apps. Link a working directory to an app to reuse its ID across commands.
- Read deployment history and build-log snapshots, cancel deployments, and follow runtime logs.
- Use JSON output for commands and one JSON object per line when following logs. Help, version information, and shell completion work offline.
- Install through the shell or PowerShell installer, which selects a release for your OS and CPU and verifies its checksum.
