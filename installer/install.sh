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

AUTO_YES=0
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
		-y)
			AUTO_YES=1
			shift
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
if [ -t 1 ]; then
	CURL_PROGRESS_ARG="--progress-bar"
else
	CURL_PROGRESS_ARG="--silent --show-error"
fi
curl --fail --location $CURL_PROGRESS_ARG "$ASSET_URL" -o "$ARCHIVE_PATH"
tar -xzf "$ARCHIVE_PATH" -C "$TMP_DIR"
install -m 0755 "$TMP_DIR/aviary" "$BIN_DIR/aviary"

case ":$PATH:" in
*":$BIN_DIR:"*) ;;
*)
	export PATH="$BIN_DIR:$PATH"
	;;
esac

detect_shell_name() {
	if [ -n "${SHELL:-}" ]; then
		basename "$SHELL"
		return
	fi

	ps -p "$PPID" -o comm= 2>/dev/null | sed 's|.*/||' | head -n1
}

SHELL_NAME="$(detect_shell_name)"
PROFILE_FILE="${HOME}/.profile"
PATH_LINE="export PATH=\"$BIN_DIR:\$PATH\""

case "$SHELL_NAME" in
bash)
	PROFILE_FILE="${HOME}/.bashrc"
	;;
zsh)
	PROFILE_FILE="${HOME}/.zshrc"
	;;
fish)
	PROFILE_FILE="${XDG_CONFIG_HOME:-$HOME/.config}/fish/config.fish"
	PATH_LINE="fish_add_path \"$BIN_DIR\""
	;;
ksh)
	PROFILE_FILE="${HOME}/.kshrc"
	;;
*)
	PROFILE_FILE="${HOME}/.profile"
	;;
esac

mkdir -p "$(dirname "$PROFILE_FILE")"
if [ -f "$PROFILE_FILE" ]; then
	if ! grep -F "$PATH_LINE" "$PROFILE_FILE" >/dev/null 2>&1; then
		printf '\n%s\n' "$PATH_LINE" >>"$PROFILE_FILE"
	fi
else
	printf '%s\n' "$PATH_LINE" >"$PROFILE_FILE"
fi

PROFILE_DISPLAY="$PROFILE_FILE"
case "$PROFILE_DISPLAY" in
"$HOME"/*)
	PROFILE_DISPLAY="~${PROFILE_DISPLAY#$HOME}"
	;;
esac

BIN_DISPLAY="$BIN_DIR/aviary"
case "$BIN_DISPLAY" in
"$HOME"/*)
	BIN_DISPLAY="~${BIN_DISPLAY#$HOME}"
	;;
esac

if [ -t 1 ]; then
	BOLD="$(printf '\033[1m')"
	BRIGHT_WHITE="$(printf '\033[97m')"
	RESET="$(printf '\033[0m')"
else
	BOLD=""
	BRIGHT_WHITE=""
	RESET=""
fi

printf "Installed %s%saviary %s%s to %s%s%s\n" "$BOLD" "$BRIGHT_WHITE" "$VERSION" "$RESET" "$BOLD" "$BIN_DISPLAY" "$RESET"
printf "Restart your terminal or run %s%ssource %s%s to update your PATH.\n" "$BOLD" "$BRIGHT_WHITE" "$PROFILE_DISPLAY" "$RESET"
echo ""
printf "Run %s%saviary configure%s to set up your Aviary configuration.\n" "$BOLD" "$BRIGHT_WHITE" "$RESET"
printf "Run %s%saviary service install%s to set up and start the system service (optional).\n" "$BOLD" "$BRIGHT_WHITE" "$RESET"
