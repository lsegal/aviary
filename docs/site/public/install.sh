#!/usr/bin/env sh
# The installer is published as an asset of every Aviary release. This copy on
# aviary.bot only forwards to it, so the script itself is never served from the
# site host. Prefer the documented URL:
#   curl -fsSL https://github.com/lsegal/aviary/releases/latest/download/install.sh | sh
set -eu
curl -fsSL https://github.com/lsegal/aviary/releases/latest/download/install.sh | sh -s -- "$@"
