#!/usr/bin/env bash
set -euo pipefail
VERSION="${1:-dev}"
mkdir -p dist
for target in "windows/amd64/.exe" "linux/amd64/" "darwin/arm64/"; do
  IFS=/ read -r goos goarch ext <<<"$target"
  out="dist/crema-${goos}-${goarch}${ext}"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags "-s -w -X main.Version=${VERSION}" -o "$out" ./cmd/crema
  bytes=$(wc -c <"$out")
  mb=$(( bytes / 1048576 ))
  printf '%-36s %s MB\n' "$out" "$mb"
  if [ "$mb" -gt 15 ]; then
    echo "$out is ${mb} MB — over the 15 MB budget" >&2
    exit 1
  fi
done
echo "all targets built and under budget"
