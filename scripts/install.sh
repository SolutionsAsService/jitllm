#!/bin/sh
# Installs jitllm from a GitHub release on Linux or macOS.
#
#   curl -fsSL https://jitllm.org/install.sh | sh
#   curl -fsSL https://jitllm.org/install.sh | sh -s -- all
#
# Arguments name the programs to install: jitllm (the CLI), jitllmd (the
# server), desktop (jitllm-desktop), tui (jitllm-tui), or all. With none it
# installs jitllm and jitllmd. Each archive is checked against the release's
# checksums.txt before anything is installed. On macOS the desktop app is the
# signed jitllm.app bundle (the release's .app.zip), installed into
# /Applications, or ~/Applications when /Applications is not writable.
#
#   JITLLM_VERSION      a release tag, such as v0.1.0 (default: the latest)
#   JITLLM_INSTALL_DIR  where the binaries go (default: ~/.local/bin, or
#                       /usr/local/bin when run as root)
#   JITLLM_DOWNLOAD_URL where releases are fetched from (default: GitHub)
set -eu

repo=jitllm/jitllm
base=${JITLLM_DOWNLOAD_URL:-https://github.com/$repo/releases/download}

say() { printf '%s\n' "$*" >&2; }
die() { say "jitllm install: $*"; exit 1; }

if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -qO "$2" "$1"; }
else
	die "needs curl or wget"
fi
if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum "$1" | cut -d' ' -f1; }
else
	sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
fi

case $(uname -s) in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) die "$(uname -s) is not supported by this script; on Windows use https://jitllm.org/install.ps1" ;;
esac
case $(uname -m) in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) die "$(uname -m) is not a release architecture (amd64, arm64)" ;;
esac

[ $# -gt 0 ] || set -- jitllm jitllmd
progs=
for p in "$@"; do
	case $p in
	all) progs="$progs jitllm jitllmd jitllm-desktop jitllm-tui" ;;
	jitllm | cli) progs="$progs jitllm" ;;
	jitllmd | server) progs="$progs jitllmd" ;;
	desktop | jitllm-desktop | ui) progs="$progs jitllm-desktop" ;;
	tui | jitllm-tui) progs="$progs jitllm-tui" ;;
	*) die "unknown program $p (jitllm, jitllmd, desktop, tui, all)" ;;
	esac
done

tag=${JITLLM_VERSION:-}
if [ -z "$tag" ]; then
	# The latest release's page redirects to its tag; no API call, no token.
	command -v curl >/dev/null 2>&1 || die "set JITLLM_VERSION (finding the latest release needs curl)"
	tag=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest" 2>/dev/null) ||
		die "no release found at https://github.com/$repo/releases"
	tag=${tag##*/}
	case $tag in v*) ;; *) die "no release found at https://github.com/$repo/releases" ;; esac
fi
version=${tag#v}

if [ -n "${JITLLM_INSTALL_DIR:-}" ]; then
	dir=$JITLLM_INSTALL_DIR
elif [ "$(id -u)" = 0 ]; then
	dir=/usr/local/bin
else
	dir=$HOME/.local/bin
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

say "jitllm $tag for $os/$arch"
fetch "$base/$tag/checksums.txt" "$tmp/checksums.txt" || die "no checksums.txt for $tag at $base/$tag"
for p in $progs; do
	archive=${p}_${version}_${os}_${arch}.tar.gz
	[ "$os/$p" = darwin/jitllm-desktop ] && archive=jitllm_${version}_${os}_${arch}.app.zip
	fetch "$base/$tag/$archive" "$tmp/$archive" || die "could not download $archive"
	want=$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")
	[ -n "$want" ] || die "$archive is not in checksums.txt"
	[ "$(sha256 "$tmp/$archive")" = "$want" ] || die "$archive does not match its checksum"
	mkdir -p "$tmp/$p"
	case $archive in
	*.app.zip)
		# ditto keeps the bundle's signature, stapled ticket and links.
		if command -v ditto >/dev/null 2>&1; then
			ditto -x -k "$tmp/$archive" "$tmp/$p"
		else
			unzip -q "$tmp/$archive" -d "$tmp/$p"
		fi
		[ -d "$tmp/$p/jitllm.app" ] || die "$archive holds no jitllm.app"
		;;
	*) tar -xzf "$tmp/$archive" -C "$tmp/$p" ;;
	esac
done

mkdir -p "$dir"
for p in $progs; do
	if [ -d "$tmp/$p/jitllm.app" ]; then
		apps=/Applications
		[ -w "$apps" ] || apps=$HOME/Applications
		mkdir -p "$apps"
		rm -rf "$apps/jitllm.app.new"
		mv "$tmp/$p/jitllm.app" "$apps/jitllm.app.new"
		rm -rf "$apps/jitllm.app"
		mv "$apps/jitllm.app.new" "$apps/jitllm.app"
		say "installed $apps/jitllm.app"
		continue
	fi
	cp "$tmp/$p/$p" "$dir/$p.new"
	chmod 755 "$dir/$p.new"
	mv -f "$dir/$p.new" "$dir/$p"
	say "installed $dir/$p"
done

case ":$PATH:" in
*":$dir:"*) ;;
*) say "$dir is not on your PATH; add it, e.g. export PATH=\"$dir:\$PATH\"" ;;
esac
