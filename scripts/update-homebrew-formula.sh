#!/usr/bin/env bash
#
# Regenerate the Homebrew formula in nrf110/homebrew-tap from a published
# release's checksums and push it. Keeps a single cross-platform formula
# (macOS + Linux), which Homebrew casks cannot provide.
#
# Usage: update-homebrew-formula.sh <version>   e.g. 0.1.2
# Requires: gh (authenticated for the release repo) and $HOMEBREW_TAP_TOKEN
# (a token with write access to the tap repo).
set -euo pipefail

VERSION="${1:?usage: update-homebrew-formula.sh <version>}"
if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "refusing to run: version must be MAJOR.MINOR.PATCH, got '$VERSION'" >&2
  exit 1
fi
: "${HOMEBREW_TAP_TOKEN:?HOMEBREW_TAP_TOKEN must be set}"

TAG="v${VERSION}"
RELEASE_REPO="nrf110/test-term"
TAP_REPO="nrf110/homebrew-tap"
BASE_URL="https://github.com/${RELEASE_REPO}/releases/download/${TAG}"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

gh release download "$TAG" --repo "$RELEASE_REPO" --pattern checksums.txt --output "$work/checksums.txt"

sha_for() {
  local file="tt_${VERSION}_$1.tar.gz"
  local sum
  sum="$(awk -v f="$file" '$2 == f {print $1}' "$work/checksums.txt")"
  if [[ -z "$sum" ]]; then
    echo "no checksum for $file" >&2
    exit 1
  fi
  printf '%s' "$sum"
}

darwin_amd64="$(sha_for darwin_amd64)"
darwin_arm64="$(sha_for darwin_arm64)"
linux_amd64="$(sha_for linux_amd64)"
linux_arm64="$(sha_for linux_arm64)"

git clone --depth 1 "https://x-access-token:${HOMEBREW_TAP_TOKEN}@github.com/${TAP_REPO}.git" "$work/tap"
mkdir -p "$work/tap/Formula"

cat > "$work/tap/Formula/tt.rb" <<EOF
class Tt < Formula
  desc "Terminal-UI test runner for polyglot projects"
  homepage "https://github.com/nrf110/test-term"
  version "${VERSION}"
  license "MIT"

  on_macos do
    on_intel do
      url "${BASE_URL}/tt_${VERSION}_darwin_amd64.tar.gz"
      sha256 "${darwin_amd64}"
    end
    on_arm do
      url "${BASE_URL}/tt_${VERSION}_darwin_arm64.tar.gz"
      sha256 "${darwin_arm64}"
    end
  end

  on_linux do
    on_intel do
      url "${BASE_URL}/tt_${VERSION}_linux_amd64.tar.gz"
      sha256 "${linux_amd64}"
    end
    on_arm do
      url "${BASE_URL}/tt_${VERSION}_linux_arm64.tar.gz"
      sha256 "${linux_arm64}"
    end
  end

  def install
    bin.install "tt"
  end

  test do
    assert_match "tt ${VERSION}", shell_output("#{bin}/tt --version")
  end
end
EOF

cd "$work/tap"
git config user.name "github-actions[bot]"
git config user.email "github-actions[bot]@users.noreply.github.com"
git add Formula/tt.rb
if git diff --cached --quiet; then
  echo "formula already up to date for ${VERSION}"
  exit 0
fi
git commit -m "tt ${VERSION}"
git push
echo "updated ${TAP_REPO} formula to ${VERSION}"
