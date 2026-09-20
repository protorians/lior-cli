#!/usr/bin/env bash
#
# release-notes.sh <version>
#
# Builds the GitHub Release body for lior-cli:
#   - download table of every binary + archive produced by GoReleaser
#   - install instructions
#   - changelog extracted from CHANGELOG.md (falls back to git log)
#
# Usage: bash scripts/release-notes.sh 0.3.0

set -euo pipefail

VERSION="${1:?usage: release-notes.sh <version>}"
TAG="v${VERSION}"
REPO="${GITHUB_REPOSITORY:-protorians/lior-cli}"
BASE="https://github.com/${REPO}/releases/download/${TAG}"

changelog() {
  awk -v v="${VERSION}" '
    $0 ~ "^## \\[v?" v "\\]" { capture = 1; next }
    capture && /^## \[/ { exit }
    capture { print }
  ' CHANGELOG.md
}

if [[ -n "$(changelog | tr -d '[:space:]')" ]]; then
  CHANGELOG="$(changelog)"
else
  PREV_TAG="$(git describe --tags --abbrev=0 "${TAG}^" 2>/dev/null || true)"
  if [[ -n "${PREV_TAG}" ]]; then
    CHANGELOG="$(git log --oneline --no-merges "${PREV_TAG}..${TAG}" | sed 's/^/- /')"
  else
    CHANGELOG="_No changelog entry found for ${TAG} in CHANGELOG.md._"
  fi
fi

cat <<EOF
## Download binaries

| Platform | Architecture | Archive | Binary |
|---|---|---|---|
| Linux | amd64 | [\`lior-cli_${VERSION}_linux_amd64.tar.gz\`](${BASE}/lior-cli_${VERSION}_linux_amd64.tar.gz) | [\`liorian_${VERSION}_linux_amd64\`](${BASE}/liorian_${VERSION}_linux_amd64) |
| Linux | arm64 | [\`lior-cli_${VERSION}_linux_arm64.tar.gz\`](${BASE}/lior-cli_${VERSION}_linux_arm64.tar.gz) | [\`liorian_${VERSION}_linux_arm64\`](${BASE}/liorian_${VERSION}_linux_arm64) |
| macOS | amd64 (Intel) | [\`lior-cli_${VERSION}_darwin_amd64.tar.gz\`](${BASE}/lior-cli_${VERSION}_darwin_amd64.tar.gz) | [\`liorian_${VERSION}_darwin_amd64\`](${BASE}/liorian_${VERSION}_darwin_amd64) |
| macOS | arm64 (Apple Silicon) | [\`lior-cli_${VERSION}_darwin_arm64.tar.gz\`](${BASE}/lior-cli_${VERSION}_darwin_arm64.tar.gz) | [\`liorian_${VERSION}_darwin_arm64\`](${BASE}/liorian_${VERSION}_darwin_arm64) |
| Windows | amd64 | [\`lior-cli_${VERSION}_windows_amd64.zip\`](${BASE}/lior-cli_${VERSION}_windows_amd64.zip) | [\`liorian_${VERSION}_windows_amd64.exe\`](${BASE}/liorian_${VERSION}_windows_amd64.exe) |

All archives and binaries are published to the [\`./dist\`](${BASE}) release assets of this tag. Checksums for every artifact are in [\`checksums.txt\`](${BASE}/checksums.txt).

## Install

### Go

\`\`\`bash
go install github.com/protorians/lior-cli@${TAG}
\`\`\`

### npm / pnpm / yarn / bun

\`\`\`bash
npm install -g @lior/cli
\`\`\`

### Homebrew (macOS / Linux)

\`\`\`bash
brew tap protorians/lior-cli https://github.com/protorians/lior-cli.git
brew install protorians/lior-cli/lior-cli
\`\`\`

### Binary

Download the archive matching your platform from the table above, extract it, and add the executable to your \`PATH\`.

---

## Changelog

${CHANGELOG}
EOF