#!/usr/bin/env sh
set -eu

REPO="${AVIARY_REPO:-lsegal/aviary}"
VERSION="${AVIARY_VERSION:-}"
DOWNLOAD_BASE="${AVIARY_DOWNLOAD_BASE:-https://github.com}"
SKIP_SKILLS="${AVIARY_SKIP_SKILLS:-}"

usage() {
	cat <<'EOF'
Usage: curl -fsSL https://github.com/lsegal/aviary/releases/latest/download/install.sh | sh
       sh install.sh [--version <tag>] [--repo <owner/name>] [--skip-skills]

Options:
  --version <tag>  Install a specific tag instead of the latest release
  --repo <name>    Override the GitHub repository (default: lsegal/aviary)
  --skip-skills    Do not run `aviary install-skills` after installing

Environment:
  AVIARY_VERSION        Same as --version
  AVIARY_REPO           Same as --repo
  AVIARY_SKIP_SKILLS=1  Same as --skip-skills
  AVIARY_DOWNLOAD_BASE  Override the release download host (default: https://github.com)
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
	--skip-skills)
		SKIP_SKILLS=1
		shift
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
	# <host>/<repo>/releases/latest redirects to the latest release's tag page,
	# so the tag is read from that redirect. It is an ordinary web request, not
	# a GitHub API call, so the unauthenticated API rate limit (which answers
	# with HTTP 403) never applies.
	LOCATION="$(curl -fsS -o /dev/null -w '%{redirect_url}' "$DOWNLOAD_BASE/$REPO/releases/latest")" || LOCATION=""
	case "$LOCATION" in
	*/releases/tag/*) VERSION="${LOCATION##*/releases/tag/}" ;;
	esac
fi

if [ -z "$VERSION" ]; then
	echo "Failed to resolve the latest release of $REPO" >&2
	return 1 2>/dev/null || exit 1
fi

ASSET="aviary_${VERSION}_${GOOS}_${GOARCH}.tar.gz"
DOWNLOAD_URL="$DOWNLOAD_BASE/$REPO/releases/download/$VERSION"

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	else
		openssl dgst -sha256 "$1" | awk '{print $NF}'
	fi
}

BIN_DIR="${HOME}/.local/bin"
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
curl --fail --location $CURL_PROGRESS_ARG "$DOWNLOAD_URL/$ASSET" -o "$ARCHIVE_PATH"
curl -fsSL "$DOWNLOAD_URL/checksums.txt" -o "$TMP_DIR/checksums.txt"
EXPECTED_SHA="$(awk -v asset="$ASSET" '$2 == asset {print $1}' "$TMP_DIR/checksums.txt")"
if [ -z "$EXPECTED_SHA" ]; then
	echo "checksums.txt for $VERSION has no entry for $ASSET" >&2
	return 1 2>/dev/null || exit 1
fi
ACTUAL_SHA="$(sha256_of "$ARCHIVE_PATH")"
if [ "$ACTUAL_SHA" != "$EXPECTED_SHA" ]; then
	echo "Checksum mismatch for $ASSET: expected $EXPECTED_SHA, got $ACTUAL_SHA" >&2
	return 1 2>/dev/null || exit 1
fi
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

# Install the gh-* skills the same way the app does. install-skills refreshes
# skills already installed, so re-running the installer or upgrading is safe.
# This script is normally piped into sh, so stdin is the rest of the script;
# keep the binary (and the npx it runs) off it.
if [ -n "$SKIP_SKILLS" ] && [ "$SKIP_SKILLS" != "0" ]; then
	SKILLS_STATUS="skipped"
elif ! command -v npx >/dev/null 2>&1; then
	SKILLS_STATUS="no-npx"
elif "$BIN_DIR/aviary" install-skills </dev/null; then
	SKILLS_STATUS="installed"
else
	SKILLS_STATUS="failed"
fi

printf "Installed %s%saviary %s%s to %s%s%s\n" "$BOLD" "$BRIGHT_WHITE" "$VERSION" "$RESET" "$BOLD" "$BIN_DISPLAY" "$RESET"
printf "Restart your terminal or run %s%ssource %s%s to update your PATH.\n" "$BOLD" "$BRIGHT_WHITE" "$PROFILE_DISPLAY" "$RESET"
echo ""
case "$SKILLS_STATUS" in
installed) echo "Installed Aviary's gh-* skills globally." ;;
skipped) printf "Skipped skill installation; run %s%saviary install-skills%s to install them later.\n" "$BOLD" "$BRIGHT_WHITE" "$RESET" ;;
no-npx) printf "Skills were not installed because npx was not found; install Node.js and run %s%saviary install-skills%s.\n" "$BOLD" "$BRIGHT_WHITE" "$RESET" >&2 ;;
*) printf "Skill installation failed; run %s%saviary install-skills%s to retry.\n" "$BOLD" "$BRIGHT_WHITE" "$RESET" >&2 ;;
esac
echo ""
printf "Run %s%saviary configure%s to set up your Aviary configuration.\n" "$BOLD" "$BRIGHT_WHITE" "$RESET"
printf "Run %s%saviary service install%s to set up and start the system service (optional).\n" "$BOLD" "$BRIGHT_WHITE" "$RESET"
