#!/bin/sh
set -eu

fail() {
	printf 'deplexo: %s\n' "$*" >&2
	exit 1
}
download() {
	curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
		--tlsv1.2 --connect-timeout 15 --max-time 180 --retry 2 "$@"
}

main() {
	[ "$#" -eq 0 ] || fail 'This installer takes no arguments. Set DEPLEXO_INSTALL_DIR to change the destination.'
	command -v curl >/dev/null 2>&1 || fail 'Install curl, then run this command again.'
	command -v tar >/dev/null 2>&1 || fail 'Install tar, then run this command again.'
	case "$(uname -s)" in
	Linux) target_os=linux ;;
	Darwin) target_os=darwin ;;
	*) fail 'Use install.ps1 in PowerShell on Windows. This script supports Linux and macOS.' ;;
	esac
	case "$(uname -m)" in
	x86_64 | amd64) target_arch=amd64 ;;
	aarch64 | arm64) target_arch=arm64 ;;
	*) fail 'This CPU architecture is not supported. Download a matching build from GitHub Releases.' ;;
	esac
	if command -v sha256sum >/dev/null 2>&1; then
		hash_tool=sha256sum
	elif command -v shasum >/dev/null 2>&1; then
		hash_tool=shasum
	else
		fail 'Install sha256sum or shasum so the download can be verified.'
	fi

	repo=https://github.com/Deplexo/cli
	release_url=$(download --output /dev/null --write-out '%{url_effective}' "$repo/releases/latest") || fail 'No stable release is available, or GitHub could not be reached. Check https://github.com/Deplexo/cli/releases.'
	case "$release_url" in "$repo"/releases/tag/v*) tag=${release_url##*/} ;; *) fail 'GitHub did not return a stable release.' ;; esac
	# Only a stable three-part release can be installed through this channel.
	printf '%s\n' "$tag" | LC_ALL=C grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$' || fail 'GitHub returned an invalid stable version.'

	umask 077
	work=$(mktemp -d "${TMPDIR:-/tmp}/deplexo-install.XXXXXXXX") || fail 'Could not create a temporary directory.'
	stage=
	trap 'rm -rf "$work"; if [ -n "$stage" ]; then rm -f "$stage"; fi' EXIT
	trap 'exit 130' INT
	trap 'exit 143' TERM HUP
	archive=deplexo_${tag}_${target_os}_${target_arch}.tar.gz
	printf 'Downloading Deplexo %s for %s/%s...\n' "$tag" "$target_os" "$target_arch"
	download "$repo/releases/download/$tag/$archive" --output "$work/$archive" || fail 'This release has no downloadable build for your platform.'
	download "$repo/releases/download/$tag/SHA256SUMS" --output "$work/SHA256SUMS" || fail 'Could not download release checksums.'
	expected=$(awk -v file="$archive" '$2 == file {print $1}' "$work/SHA256SUMS")
	[ "${#expected}" -eq 64 ] || fail 'The release checksum is missing or ambiguous.'
	case "$expected" in *[!0-9a-f]*) fail 'The release checksum is invalid.' ;; esac
	if [ "$hash_tool" = sha256sum ]; then
		actual=$(sha256sum "$work/$archive")
	else
		actual=$(shasum -a 256 "$work/$archive")
	fi
	[ "${actual%% *}" = "$expected" ] || fail 'The download checksum does not match. Your installed binary was not changed.'
	# Stream only the executable into a regular file; do not unpack archive paths.
	tar -xzOf "$work/$archive" deplexo >"$work/deplexo" || fail 'Could not read the executable from the archive.'
	[ -s "$work/deplexo" ] || fail 'The archive contains an empty executable.'
	chmod 700 "$work/deplexo"
	"$work/deplexo" version >/dev/null || fail 'The downloaded binary cannot run on this system.'
	destination=${DEPLEXO_INSTALL_DIR:-"$HOME/.local/bin"}
	case "$destination" in /*) ;; *) fail 'DEPLEXO_INSTALL_DIR must be an absolute path.' ;; esac
	mkdir -p "$destination" || fail 'Could not create the install directory.'
	[ ! -d "$destination/deplexo" ] || fail 'The destination is a directory, not an executable.'
	stage=$(mktemp "$destination/.deplexo.XXXXXXXX")
	cat "$work/deplexo" >"$stage"
	chmod 755 "$stage"
	mv -f "$stage" "$destination/deplexo"
	stage=
	printf 'Installed Deplexo %s at %s/deplexo\n' "$tag" "$destination"
	case ":$PATH:" in *":$destination:"*) ;; *) printf 'Add %s to your PATH, then open a new terminal.\n' "$destination" ;; esac
	printf 'Run deplexo auth login to sign in. Run this installer again to update.\n'
}

# A complete function prevents a truncated download from running a partial install.
main "$@"
