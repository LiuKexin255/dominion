#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BIN_DIR="${PROJECT_DIR}/resources/bin"
METADATA_FILE="${BIN_DIR}/ffmpeg-metadata.json"

FFMPEG_VERSION="8.1"
FFMPEG_VARIANT="essentials"
FFMPEG_URL="https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-${FFMPEG_VARIANT}.zip"
EXPECTED_SHA256=""

if [ -f "$METADATA_FILE" ]; then
    EXPECTED_SHA256=$(python3 -c "import json; print(json.load(open('$METADATA_FILE'))['sha256'])" 2>/dev/null || true)
fi

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

echo "Downloading FFmpeg ${FFMPEG_VERSION} (${FFMPEG_VARIANT})..."
wget -q --show-progress "${FFMPEG_URL}" -O "${TMPDIR}/ffmpeg.zip"

echo "Extracting ffmpeg.exe..."
unzip -j -o "${TMPDIR}/ffmpeg.zip" "ffmpeg-${FFMPEG_VERSION}-essentials_build/bin/ffmpeg.exe" -d "${BIN_DIR}/"

echo "Computing SHA256..."
ACTUAL_SHA256=$(sha256sum "${BIN_DIR}/ffmpeg.exe" | cut -d' ' -f1)

if [ -n "$EXPECTED_SHA256" ] && [ "$ACTUAL_SHA256" != "$EXPECTED_SHA256" ]; then
    echo "SHA256 mismatch!"
    echo "  Expected: ${EXPECTED_SHA256}"
    echo "  Actual:   ${ACTUAL_SHA256}"
    exit 1
fi

sha256sum "${BIN_DIR}/ffmpeg.exe" > "${BIN_DIR}/ffmpeg.exe.sha256"

cat > "${METADATA_FILE}" << METAEOF
{
  "version": "${FFMPEG_VERSION}",
  "variant": "${FFMPEG_VARIANT}",
  "source_url": "${FFMPEG_URL}",
  "sha256": "${ACTUAL_SHA256}",
  "download_date": "$(date -I)",
  "includes_libx264": true
}
METAEOF

echo "Done. ffmpeg.exe (${ACTUAL_SHA256}) saved to ${BIN_DIR}/"
