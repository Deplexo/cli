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

quote_path() {
	# Single quotes also prevent history expansion when pasted into zsh.
	printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"
}

setup_path() {
	case ":$PATH:" in
	*":$destination:"*)
		printf 'Run deplexo auth login to sign in.\n'
		return
		;;
	esac
	quoted_destination=$(quote_path "$destination")
	activation="export PATH=$quoted_destination:\"\$PATH\""
	config_file=
	shell_name=${SHELL:-}
	shell_name=${shell_name##*/}
	case "${SHELL:-}" in
	*/zsh) config_file=${ZDOTDIR-$HOME}/.zshrc ;;
	*/bash)
		if [ "$target_os" = darwin ]; then
			config_file=$HOME/.bash_profile
			for candidate in "$HOME/.bash_profile" "$HOME/.bash_login" "$HOME/.profile"; do
				if [ -e "$candidate" ] || [ -L "$candidate" ]; then
					config_file=$candidate
					break
				fi
			done
		else
			config_file=$HOME/.bashrc
		fi
		;;
	*/fish)
		# fish_add_path persists the change itself; do not edit fish configuration.
		fish_path=$(printf '%s' "$destination" | sed "s/[\\\\']/\\\\&/g")
		activation="fish_add_path -- '$fish_path'"
		;;
	esac
	printf 'To use deplexo in this terminal, run:\n  %s\n' "$activation"
	if [ -n "$config_file" ]; then
		if [ -L "$config_file" ] || { [ -e "$config_file" ] && [ ! -f "$config_file" ]; }; then
			printf 'Update %s yourself; the installer only edits regular configuration files.\n' "$config_file"
		elif [ -f "$config_file" ] && grep -Fqx "$activation" "$config_file"; then
			printf 'The PATH setting is already in %s. Run the command above for this terminal.\n' "$config_file"
		elif [ -z "${CI:-}" ] && [ "${DEPLEXO_NO_MODIFY_PATH:-}" != 1 ] && [ -t 1 ] && [ -t 2 ] && (: </dev/tty) 2>/dev/null; then
			printf 'Add this PATH setting to %s for future terminals? [y/N] ' "$config_file" >&2
			answer=
			read -r answer </dev/tty || answer=
			case "$answer" in
			y | Y | yes | Yes | YES)
				# Refuse symlinks again after the prompt. Create new files exclusively.
				if [ ! -L "$config_file" ] && { [ -f "$config_file" ] || (
					set -C
					: >"$config_file"
				) 2>/dev/null; } &&
					printf '\n# Deplexo CLI\n%s\n' "$activation" >>"$config_file"; then
					printf 'Added PATH to %s. Run the command above now, or open a new terminal.\n' "$config_file"
				else
					printf 'Could not update %s. Add the PATH command above yourself.\n' "$config_file" >&2
				fi
				;;
			*) printf 'No shell configuration changed. Add the PATH command above to %s for future terminals.\n' "$config_file" ;;
			esac
		else
			printf 'Add the PATH command above to %s for future terminals.\n' "$config_file"
		fi
	elif [ "${shell_name}" != fish ]; then
		printf 'For sh-compatible shells, add that export line to your shell startup file.\n'
	fi
	if [ "${shell_name}" = fish ]; then
		printf "You can also sign in now with:\n  '%s/deplexo' auth login\n" "$fish_path"
	else
		printf 'You can also sign in now with:\n  %s auth login\n' "$(quote_path "$destination/deplexo")"
	fi
}

main() {
	[ "$#" -eq 0 ] || fail 'This installer takes no arguments. Set DEPLEXO_VERSION to select a release or DEPLEXO_INSTALL_DIR to change the destination.'
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
	if [ -n "${DEPLEXO_VERSION:-}" ]; then
		tag=$DEPLEXO_VERSION
		case "$tag" in *[!0-9A-Za-z.-]*) fail 'DEPLEXO_VERSION must be a release tag such as v0.1.0 or v0.1.0-beta.1.' ;; esac
		number='(0|[1-9][0-9]*)'
		identifier="($number|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)"
		printf '%s\n' "$tag" | LC_ALL=C grep -Eq "^v$number\.$number\.$number(-$identifier(\.$identifier)*)?$" || fail 'DEPLEXO_VERSION must be a release tag such as v0.1.0 or v0.1.0-beta.1.'
	else
		tag=$(download https://cli.deplexo.com/latest-version) || fail 'Could not find the current release. Check https://github.com/Deplexo/cli/releases.'
		case "$tag" in *[!0-9A-Za-z.-]*) fail 'The release version is invalid.' ;; esac
		printf '%s\n' "$tag" | LC_ALL=C grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-beta\.(0|[1-9][0-9]*))?$' || fail 'The release version is invalid.'
	fi

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
	case "$destination" in *:*) fail 'The install directory cannot contain a colon because PATH uses colons to separate directories.' ;; esac
	if [ "$destination" != "$(printf '%s' "$destination" | LC_ALL=C tr -d '[:cntrl:]')" ]; then
		fail 'The install directory cannot contain control characters.'
	fi
	mkdir -p "$destination" || fail 'Could not create the install directory.'
	[ ! -d "$destination/deplexo" ] || fail 'The destination is a directory, not an executable.'
	stage=$(mktemp "$destination/.deplexo.XXXXXXXX")
	cat "$work/deplexo" >"$stage"
	chmod 755 "$stage"
	mv -f "$stage" "$destination/deplexo"
	stage=
	printf 'Installed Deplexo %s at %s/deplexo\n' "$tag" "$destination"
	setup_path
	if [ -n "${DEPLEXO_VERSION:-}" ]; then
		printf 'To update, choose a newer DEPLEXO_VERSION.\n'
	else
		printf 'Run this installer again to update.\n'
	fi
}

# A complete function prevents a truncated download from running a partial install.
main "$@"
