#!/bin/sh
# Builds the three services for a Linux server and packs them with the deploy files.
#   sh deploy/build.sh [amd64|arm64]        (default amd64)
# The database driver is pure Go (modernc.org/sqlite), so this cross-compiles from a Mac without cgo.
set -eu
cd "$(dirname "$0")/.."
ARCH="${1:-amd64}"
OUT=deploy/out
rm -rf "$OUT" && mkdir -p "$OUT/bin" "$OUT/configs"
for svc in loginhttp gateway admin; do
  CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -trimpath -o "$OUT/bin/$svc" "./cmd/$svc"
done
cp configs/prod.example.yaml "$OUT/configs/"
cp deploy/Caddyfile deploy/2dround-*.service deploy/README.md "$OUT/"
tar -czf deploy/2dround-server-linux-"$ARCH".tar.gz -C "$OUT" .
echo "built deploy/2dround-server-linux-$ARCH.tar.gz"
