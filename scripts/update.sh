#!/usr/bin/env bash
# Built-in update helper for CLIProxyAPIPlus
# Usage: ./scripts/update.sh
# Requirements: curl, tar
# The script will fetch the latest release from GitHub, download the
# appropriate archive for the current platform (linux/amd64), stop the
# running service, replace the binary, and restart it.  It also performs
# a usage export/import so that temporary in-memory usage state is
# preserved across the upgrade.

set -euo pipefail

# GitHub repository information.  Adjust if you fork the project.
OWNER="router-for-me"
REPO="CLIProxyAPIPlus"

# detect architecture/os, fall back to linux/amd64
OS=$(uname | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "${ARCH}" in
    x86_64) ARCH="amd64" ;; 
    aarch64|arm64) ARCH="arm64" ;; 
    *) ARCH="${ARCH}" ;;
esac

# Only linux packaged currently by default; modify if other
# releases exist.
OS=${OS:-linux}
ARCH=${ARCH:-amd64}

# parse latest release version (strip leading 'v')
echo "获取最新版本号…"
VERSION=$(curl -s "https://api.github.com/repos/$OWNER/$REPO/releases/latest" \
  | grep -oP '"tag_name":\s*"\K(.*)(?=")' \
  | sed 's/^v//')

if [[ -z "$VERSION" ]]; then
  echo "❌ 获取版本号失败"
  exit 1
fi

echo "最新版本号：$VERSION"

# construct filename and download URL
FILENAME="cli-proxy-api-plus_${VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/$OWNER/$REPO/releases/download/v$VERSION/$FILENAME"

WORKINGDIR=$HOME/cli-proxy
# you may need to adjust this if you installed elsewhere
mkdir -p "$WORKINGDIR"
cd "$WORKINGDIR"

echo "准备下载：$FILENAME"
echo "下载地址：$URL"

curl -L --progress-bar -o "$FILENAME" "$URL"

echo "✅ 下载完成：$FILENAME"

# export usage state before stopping
if curl --connect-timeout 2 -sSf "http://127.0.0.1:8317/v0/management/usage/export" -H "X-Management-Key: secretkey" -o usage.json; then
    echo "usage export saved to usage.json"
else
    echo "warning: failed to export usage, continuing anyway"
fi

# stop running instance
if pid=$(pidof cli-proxy-api-plus); then
    echo "stopping running process (pid $pid)"
    kill -9 $pid || true
fi

# unpack tarball (overwrites existing binary)
tar xvf "$FILENAME"

# restart service in background (adjust your config path)
nohup "$WORKINGDIR/cli-proxy-api-plus" -config "$WORKINGDIR/config.yaml" > /dev/null 2>&1 &

sleep 3

# import usage state back
if [[ -f usage.json ]]; then
    curl -sSf "http://127.0.0.1:8317/v0/management/usage/import" -H "X-Management-Key: secretkey" -H "Content-Type: application/json" -d @"usage.json" && echo "usage imported"
fi

echo "✅ 升级完成，当前运行版本 $VERSION"
