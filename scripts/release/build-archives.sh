#!/usr/bin/env bash

# Build the release archives used by GitHub Actions and by maintainers making a
# local release candidate. Set VERSION to the exact tag name (for example,
# v0.1.0) before running this script.
set -euo pipefail

: "${VERSION:?VERSION must be set to the release tag, for example v0.1.0}"

DIST_DIR="${DIST_DIR:-dist}"
TARGETS=(
  "darwin/amd64"
  "darwin/arm64"
  "linux/amd64"
  "linux/arm64"
  "windows/amd64"
  "windows/arm64"
)
PROGRAMS=(
  "rexc:./cmd/rexc"
  "rexd:./cmd/rexd"
  "redis_setup:./tools/redis_setup"
  "rex_stressor:./tools/rex_stressor"
  "rule_gen:./tools/rule_gen"
)

rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"

for target in "${TARGETS[@]}"; do
  goos="${target%%/*}"
  goarch="${target##*/}"
  archive_name="rex_${VERSION}_${goos}_${goarch}"
  package_dir="${DIST_DIR}/${archive_name}"
  executable_suffix=""
  if [[ "$goos" == "windows" ]]; then
    executable_suffix=".exe"
  fi

  mkdir -p "$package_dir"
  for program in "${PROGRAMS[@]}"; do
    name="${program%%:*}"
    package="${program#*:}"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
      -trimpath \
      -buildvcs=false \
      -ldflags="-s -w" \
      -o "${package_dir}/${name}${executable_suffix}" \
      "$package"
  done
  cp LICENSE README.md "$package_dir/"

  if [[ "$goos" == "windows" ]]; then
    (
      cd "$DIST_DIR"
      zip -qr "${archive_name}.zip" "$archive_name"
    )
  else
    tar -C "$DIST_DIR" -czf "${DIST_DIR}/${archive_name}.tar.gz" "$archive_name"
  fi
  rm -rf "$package_dir"
done

(
  cd "$DIST_DIR"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum *.tar.gz *.zip > checksums.txt
  else
    shasum -a 256 *.tar.gz *.zip > checksums.txt
  fi
)
