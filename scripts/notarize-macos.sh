#!/usr/bin/env bash
# notarize-macos.sh — 將 dist/4x-Live.dmg 提交 Apple 公證（notarization）並 staple 票證。
#
# 前置：dist/4x-Live.dmg 須已存在，且其中的 app 已用 Developer ID Application 憑證簽名
#       （由 package-macos.sh 在設定 CODESIGN_IDENTITY 時產出）。ad-hoc 簽名的 DMG 會被公證服務拒絕。
#
# 認證從環境變數或 repo 根目錄的 .env 讀取，二選一：
#   A) App Store Connect API key（推薦）：
#      NOTARY_KEY_PATH   — .p8 私鑰檔路徑
#      NOTARY_KEY_ID     — Key ID
#      NOTARY_ISSUER_ID  — Issuer ID
#   B) Apple ID + app-specific password：
#      NOTARY_APPLE_ID   — Apple ID email
#      NOTARY_PASSWORD   — app-specific password（在 appleid.apple.com 產生）
#      NOTARY_TEAM_ID    — Team ID
#
# 產物：已 staple 的 dist/4x-Live.dmg
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# 載入 repo 根目錄的 .env（若存在）——公證憑證不 commit，改由 .env 或環境變數提供
if [ -f "$ROOT/.env" ]; then
  set -a
  # shellcheck disable=SC1091
  . "$ROOT/.env"
  set +a
fi

DMG="$ROOT/dist/4x-Live.dmg"
if [ ! -f "$DMG" ]; then
  echo "ERROR: $DMG not found; run 'make package-macos' (with CODESIGN_IDENTITY set) first" >&2
  exit 1
fi

# 組 notarytool 認證參數（優先用 API key）
if [ -n "${NOTARY_KEY_PATH:-}" ]; then
  if [ -z "${NOTARY_KEY_ID:-}" ] || [ -z "${NOTARY_ISSUER_ID:-}" ]; then
    echo "ERROR: NOTARY_KEY_PATH set but NOTARY_KEY_ID / NOTARY_ISSUER_ID missing" >&2
    exit 1
  fi
  echo "==> notarization credentials: App Store Connect API key ($NOTARY_KEY_ID)"
  CRED=(--key "$NOTARY_KEY_PATH" --key-id "$NOTARY_KEY_ID" --issuer "$NOTARY_ISSUER_ID")
elif [ -n "${NOTARY_APPLE_ID:-}" ]; then
  if [ -z "${NOTARY_PASSWORD:-}" ] || [ -z "${NOTARY_TEAM_ID:-}" ]; then
    echo "ERROR: NOTARY_APPLE_ID set but NOTARY_PASSWORD / NOTARY_TEAM_ID missing" >&2
    exit 1
  fi
  echo "==> notarization credentials: Apple ID ($NOTARY_APPLE_ID)"
  CRED=(--apple-id "$NOTARY_APPLE_ID" --password "$NOTARY_PASSWORD" --team-id "$NOTARY_TEAM_ID")
else
  echo "ERROR: notarization credentials not set." >&2
  echo "  Provide either an App Store Connect API key (NOTARY_KEY_PATH / NOTARY_KEY_ID / NOTARY_ISSUER_ID)" >&2
  echo "  or an Apple ID (NOTARY_APPLE_ID / NOTARY_PASSWORD / NOTARY_TEAM_ID) via .env or environment." >&2
  echo "  See docs/guide/dashboard.md → macOS release build for setup." >&2
  exit 1
fi

# 1. 提交公證並等待結果（--wait 會阻塞至 Apple 回傳 Accepted/Invalid）
echo "==> submitting $DMG for notarization (this may take a few minutes)"
xcrun notarytool submit "$DMG" "${CRED[@]}" --wait

# 2. staple 票證到 DMG，讓離線環境也能通過 Gatekeeper 驗證
echo "==> stapling notarization ticket"
xcrun stapler staple "$DMG"

# 3. 驗證
echo "==> validating"
xcrun stapler validate "$DMG"

echo "==> notarization complete: $DMG"
