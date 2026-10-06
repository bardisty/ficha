#!/bin/sh
# Installs the latest ficha release for macOS or Linux:
#
#   curl -fsSL https://raw.githubusercontent.com/bardisty/ficha/main/install.sh | sh
#
# FICHA_VERSION picks a release, as in FICHA_VERSION=v0.65.20.
# FICHA_INSTALL_DIR picks the directory, ~/.local/bin by default.
#
# Everything runs inside main, called on the last line, so a download cut off
# partway through runs nothing.

set -eu

repo=bardisty/ficha

say() { printf '%s\n' "$*"; }
fail() {
	printf 'ficha install: %s\n' "$*" >&2
	exit 1
}

detect_platform() {
	case $(uname -s) in
	Darwin) os=darwin ;;
	Linux) os=linux ;;
	MINGW* | MSYS* | CYGWIN*) fail "on Windows, run install.ps1 in PowerShell instead. See https://github.com/$repo#install" ;;
	*) fail "no ficha build for $(uname -s). See https://github.com/$repo#install" ;;
	esac
	case $(uname -m) in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) fail "no ficha build for $(uname -m). See https://github.com/$repo#install" ;;
	esac
	# A shell running under Rosetta reports x86_64 on Apple silicon. The
	# native build is the one to install there.
	if [ "$os" = darwin ] && [ "$arch" = amd64 ] &&
		[ "$(sysctl -n sysctl.proc_translated 2>/dev/null)" = 1 ]; then
		arch=arm64
	fi
}

download() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$2" "$1"
	else
		fail "needs curl or wget"
	fi
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d ' ' -f 1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d ' ' -f 1
	else
		fail "needs sha256sum or shasum to check the download"
	fi
}

# The line to add to the shell's startup file, for when dir isn't on PATH.
path_hint() {
	case ${SHELL:-} in
	*/zsh) say "  echo 'export PATH=\"$1:\$PATH\"' >> ~/.zshrc" ;;
	*/fish) say "  fish_add_path \"$1\"" ;;
	*/bash)
		if [ "$os" = darwin ]; then
			say "  echo 'export PATH=\"$1:\$PATH\"' >> ~/.bash_profile"
		else
			say "  echo 'export PATH=\"$1:\$PATH\"' >> ~/.bashrc"
		fi
		;;
	*) say "  echo 'export PATH=\"$1:\$PATH\"' >> ~/.profile" ;;
	esac
}

main() {
	detect_platform
	file="ficha-$os-$arch"
	version=${FICHA_VERSION:-latest}
	if [ "$version" = latest ]; then
		url="https://github.com/$repo/releases/latest/download"
	else
		case $version in v*) ;; *) version="v$version" ;; esac
		url="https://github.com/$repo/releases/download/$version"
	fi
	dir=${FICHA_INSTALL_DIR:-$HOME/.local/bin}

	tmp=$(mktemp -d)
	new=
	trap 'rm -rf "$tmp"; [ -z "$new" ] || rm -f "$new"' EXIT
	trap 'exit 1' HUP INT TERM

	say "Downloading $file ($version)"
	download "$url/$file" "$tmp/$file" || fail "couldn't download $url/$file"
	download "$url/checksums.txt" "$tmp/checksums.txt" || fail "couldn't download $url/checksums.txt"

	want=$(awk -v f="$file" '$2 == f || $2 == "*" f { print $1 }' "$tmp/checksums.txt")
	[ -n "$want" ] || fail "checksums.txt has no entry for $file"
	got=$(sha256 "$tmp/$file")
	[ "$got" = "$want" ] || fail "$file doesn't match checksums.txt, so nothing was installed"

	mkdir -p "$dir" || fail "couldn't create $dir"
	# An absolute path, so the PATH check and hint below match what a shell
	# would look up.
	dir=$(CDPATH='' cd -- "$dir" && pwd)
	[ -w "$dir" ] || fail "can't write to $dir"
	new=$(mktemp "$dir/.ficha.XXXXXX")
	cp "$tmp/$file" "$new"
	chmod 755 "$new"
	# Try the new binary before it replaces a working one. It runs from dir
	# rather than tmp, since tmp may be mounted noexec.
	installed=$("$new" version) || fail "the new ficha won't run from $dir, so nothing was changed"
	# A move within one filesystem swaps the file in whole, so a ficha that's
	# running keeps its old binary and the next run gets the new one.
	mv -f "$new" "$dir/ficha"
	new=
	say "Installed $installed to $dir/ficha"

	case ":$PATH:" in
	*":$dir:"*)
		found=$(command -v ficha 2>/dev/null || true)
		if [ -n "$found" ] && [ "$found" != "$dir/ficha" ]; then
			say ""
			say "But 'ficha' runs $found, which comes earlier on your PATH."
			say "Remove that one, or put $dir earlier on your PATH."
		fi
		;;
	*)
		say ""
		say "$dir isn't on your PATH yet. Add it, then open a new terminal:"
		# Single quotes in the hint keep $HOME for the startup file to expand.
		case $dir in
		"$HOME"/*) path_hint "\$HOME${dir#"$HOME"}" ;;
		*) path_hint "$dir" ;;
		esac
		;;
	esac
}

main "$@"
