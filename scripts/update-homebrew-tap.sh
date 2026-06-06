#!/usr/bin/env bash
set -euo pipefail

version="${GITHUB_REF_NAME#v}"
tap_dir="$(mktemp -d)"
repo_url="https://x-access-token:${HOMEBREW_TAP_GITHUB_TOKEN}@github.com/parseablehq/homebrew-tap.git"

git clone "$repo_url" "$tap_dir"
mkdir -p "$tap_dir/Formula"

sha256_file() {
  shasum -a 256 "$1" | awk '{print $1}'
}

darwin_arm64_sha="$(sha256_file "dist/pb_${version}_darwin_arm64.tar.gz")"
darwin_amd64_sha="$(sha256_file "dist/pb_${version}_darwin_amd64.tar.gz")"
linux_arm64_sha="$(sha256_file "dist/pb_${version}_linux_arm64.tar.gz")"
linux_amd64_sha="$(sha256_file "dist/pb_${version}_linux_amd64.tar.gz")"

cat > "$tap_dir/Formula/pb.rb" <<EOF
class Pb < Formula
  desc "Command line interface for Parseable"
  homepage "https://github.com/parseablehq/pb"
  license "AGPL-3.0"
  version "${version}"

  on_macos do
    on_arm do
      url "https://github.com/parseablehq/pb/releases/download/v${version}/pb_${version}_darwin_arm64.tar.gz"
      sha256 "${darwin_arm64_sha}"
    end

    on_intel do
      url "https://github.com/parseablehq/pb/releases/download/v${version}/pb_${version}_darwin_amd64.tar.gz"
      sha256 "${darwin_amd64_sha}"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/parseablehq/pb/releases/download/v${version}/pb_${version}_linux_arm64.tar.gz"
      sha256 "${linux_arm64_sha}"
    end

    on_intel do
      url "https://github.com/parseablehq/pb/releases/download/v${version}/pb_${version}_linux_amd64.tar.gz"
      sha256 "${linux_amd64_sha}"
    end
  end

  def install
    bin.install "pb"
  end

  test do
    system "#{bin}/pb", "--help"
  end
end
EOF

git -C "$tap_dir" config user.name "github-actions[bot]"
git -C "$tap_dir" config user.email "41898282+github-actions[bot]@users.noreply.github.com"
git -C "$tap_dir" add Formula/pb.rb

if git -C "$tap_dir" diff --cached --quiet; then
  echo "Homebrew formula is already up to date."
else
  git -C "$tap_dir" commit -m "Update pb to v${version}"
  git -C "$tap_dir" push origin HEAD
fi
