#!/usr/bin/env bash
set -euo pipefail

NODE_VERSION=22.23.4
case "$(dpkg --print-architecture)" in
  amd64) arch=x64; sha256=1805bcd35e6815559bd2eb97db438d3fdb0eb3a5e6a977b7c1badca76337e9ce ;;
  arm64) arch=arm64; sha256=6287aa46b6c375d816e63ae66d024fbbfd70404825aabdfdf898a970871d2f4b ;;
  *) echo 'Unsupported Node.js architecture' >&2; exit 1 ;;
esac

apt-get update
apt-get install --no-install-recommends -y ca-certificates curl xz-utils
archive="$(mktemp)"
trap 'rm -f "$archive"' EXIT
curl --retry 5 --retry-delay 1 --retry-max-time 120 --retry-connrefused -fsSL \
  "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-${arch}.tar.xz" -o "$archive"
printf '%s  %s\n' "$sha256" "$archive" | sha256sum --check --strict
tar -xJf "$archive" -C /usr/local --strip-components=1
apt-get clean
rm -rf /var/lib/apt/lists/* /var/cache/apt/archives/*
node --version
npm --version
