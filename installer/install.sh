#!/usr/bin/env sh
set -eu

REPO="${AVIARY_REPO:-lsegal/aviary}"
VERSION="${AVIARY_VERSION:-}"
API_BASE="${AVIARY_API_BASE:-https://api.github.com}"

usage() {
	cat <<'EOF'
Usage: . ./installer/install.sh [--version <tag>] [--repo <owner/name>]

Environment:
  AVIARY_VERSION   Install a specific tag instead of the latest release
  AVIARY_REPO      Override the GitHub repository (default: lsegal/aviary)
  AVIARY_API_BASE  Override the GitHub API base URL
EOF
}

while [ "$#" -gt 0 ]; do
	case "$1" in
	--version)
		VERSION="$2"
		shift 2
		;;
	--repo)
		REPO="$2"
		shift 2
		;;
	-h|--help)
		usage
		return 0 2>/dev/null || exit 0
		;;
	*)
		echo "Unknown argument: $1" >&2
		return 1 2>/dev/null || exit 1
		;;
	esac
done

require() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "Missing required command: $1" >&2
		return 1 2>/dev/null || exit 1
	fi
}

require curl
require tar

OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
Linux) GOOS="linux" ;;
Darwin) GOOS="darwin" ;;
*)
	echo "Unsupported operating system: $OS" >&2
	return 1 2>/dev/null || exit 1
	;;
esac

case "$ARCH" in
x86_64|amd64) GOARCH="amd64" ;;
aarch64|arm64) GOARCH="arm64" ;;
*)
	echo "Unsupported architecture: $ARCH" >&2
	return 1 2>/dev/null || exit 1
	;;
esac

if [ -z "$VERSION" ]; then
	RELEASE_JSON="$(curl -fsSL -H 'Accept: application/vnd.github+json' "$API_BASE/repos/$REPO/releases/latest")"
	VERSION="$(printf '%s' "$RELEASE_JSON" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)"
else
	RELEASE_JSON="$(curl -fsSL -H 'Accept: application/vnd.github+json' "$API_BASE/repos/$REPO/releases/tags/$VERSION")"
fi

if [ -z "$VERSION" ]; then
	echo "Failed to resolve release version from GitHub API" >&2
	return 1 2>/dev/null || exit 1
fi

ASSET="aviary_${VERSION}_${GOOS}_${GOARCH}.tar.gz"
ASSET_URL="$(printf '%s' "$RELEASE_JSON" | sed -n "s|.*\\\"browser_download_url\\\"[[:space:]]*:[[:space:]]*\\\"\\([^\\\"]*${ASSET}\\)\\\".*|\\1|p" | head -n1)"

if [ -z "$ASSET_URL" ]; then
	ASSET_URL="https://github.com/$REPO/releases/download/$VERSION/$ASSET"
fi

CONFIG_ROOT="${XDG_CONFIG_HOME:-$HOME/.config}/aviary"
BIN_DIR="$CONFIG_ROOT/bin"
TMP_DIR="$(mktemp -d)"

cleanup() {
	rm -rf "$TMP_DIR"
}
trap cleanup EXIT INT TERM

mkdir -p "$BIN_DIR"
ARCHIVE_PATH="$TMP_DIR/$ASSET"
curl -fL "$ASSET_URL" -o "$ARCHIVE_PATH"
tar -xzf "$ARCHIVE_PATH" -C "$TMP_DIR"
install -m 0755 "$TMP_DIR/aviary" "$BIN_DIR/aviary"

case ":$PATH:" in
*":$BIN_DIR:"*) ;;
*)
	export PATH="$BIN_DIR:$PATH"
	;;
esac

PROFILE_FILE="${HOME}/.profile"
PATH_LINE="export PATH=\"$BIN_DIR:\$PATH\""
if [ -f "$PROFILE_FILE" ]; then
	if ! grep -F "$PATH_LINE" "$PROFILE_FILE" >/dev/null 2>&1; then
		printf '\n%s\n' "$PATH_LINE" >>"$PROFILE_FILE"
	fi
else
	printf '%s\n' "$PATH_LINE" >"$PROFILE_FILE"
fi

echo "Installed aviary to $BIN_DIR/aviary"
echo "Version: $VERSION"
echo "PATH updated for this shell."
echo "Future POSIX shells will load $PROFILE_FILE."
echo "To affect the parent shell immediately, run: . ./installer/install.sh"
