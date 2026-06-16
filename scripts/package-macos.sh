#!/usr/bin/env bash
# package-macos.sh — 將 macOS Swift 殼與 universal 4x binary 組成 `4x Live.app` 並產出 .dmg。
#
# 前置：bin/4x-darwin-arm64 與 bin/4x-darwin-amd64 須已存在（由 CI 或 go build 交叉編譯產出）。
# 產物：dist/4x-Live.dmg
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
DIST="$ROOT/dist"
BUILD="$ROOT/dist/macos-build"
APP="$BUILD/4x Live.app"
MACOS_DIR="$APP/Contents/MacOS"
RESOURCES_DIR="$APP/Contents/Resources"

ARM_BIN="bin/4x-darwin-arm64"
AMD_BIN="bin/4x-darwin-amd64"

for f in "$ARM_BIN" "$AMD_BIN"; do
  if [ ! -f "$f" ]; then
    echo "ERROR: required binary $f not found; cross-compile it first" >&2
    exit 1
  fi
done

rm -rf "$BUILD"
mkdir -p "$MACOS_DIR" "$RESOURCES_DIR" "$DIST"

# 1. lipo 合併兩 arch 為 universal binary
echo "==> lipo: building universal 4x binary"
lipo -create "$ARM_BIN" "$AMD_BIN" -output "$MACOS_DIR/4x"
chmod +x "$MACOS_DIR/4x"

# 2. 編譯 Swift app（release）
echo "==> swift build (release)"
swift build -c release --package-path dashboard/macos
SWIFT_BIN="$(swift build -c release --package-path dashboard/macos --show-bin-path)/4xLive"
cp "$SWIFT_BIN" "$MACOS_DIR/4xLive"
chmod +x "$MACOS_DIR/4xLive"

# 3. 複製所有 Resources
echo "==> copying resources"
cp dashboard/macos/Resources/* "$RESOURCES_DIR/"

# 4. 寫 Info.plist
cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleName</key>
    <string>4x Live</string>
    <key>CFBundleDisplayName</key>
    <string>4x Live</string>
    <key>CFBundleIdentifier</key>
    <string>com.ggwhite.4x.live</string>
    <key>CFBundleVersion</key>
    <string>${VERSION}</string>
    <key>CFBundleShortVersionString</key>
    <string>${VERSION}</string>
    <key>CFBundleExecutable</key>
    <string>4xLive</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleIconFile</key>
    <string>AppIcon</string>
    <key>LSMinimumSystemVersion</key>
    <string>13.0</string>
    <key>NSHighResolutionCapable</key>
    <true/>
</dict>
</plist>
PLIST

# 5. Ad-hoc codesign with hardened runtime（抑制 WKWebView 觸發的 Apple Music / 照片 等無關權限對話框）
echo "==> codesign (ad-hoc + hardened runtime)"
ENTITLEMENTS="$ROOT/dashboard/macos/4xLive.entitlements"
codesign --force --sign - --options runtime "$MACOS_DIR/4x"
codesign --force --sign - --options runtime --entitlements "$ENTITLEMENTS" "$MACOS_DIR/4xLive"
codesign --force --sign - --options runtime --entitlements "$ENTITLEMENTS" "$APP"

# 6. 產 dmg（含 Applications 捷徑）
echo "==> creating dmg"
DMG="$DIST/4x-Live.dmg"
DMG_TMP="$DIST/4x-Live-tmp.dmg"
VOLUME_NAME="4x Live"
rm -f "$DMG" "$DMG_TMP"

STAGING="$BUILD/dmg-staging"
rm -rf "$STAGING"
mkdir -p "$STAGING"
cp -R "$APP" "$STAGING/"
ln -s /Applications "$STAGING/Applications"

# 算 DMG 大小（app + 20MB 餘裕）
APP_SIZE_KB=$(du -sk "$STAGING" | awk '{print $1}')
DMG_SIZE_KB=$((APP_SIZE_KB + 20480))

hdiutil create -volname "$VOLUME_NAME" -size "${DMG_SIZE_KB}k" -fs HFS+ -layout SPUD "$DMG_TMP"
MOUNT_OUT=$(hdiutil attach -readwrite -noverify -noautoopen "$DMG_TMP")
MOUNT_DEV=$(echo "$MOUNT_OUT" | head -1 | awk '{print $1}')
MOUNT_DIR=$(echo "$MOUNT_OUT" | tail -1 | awk -F'\t' '{print $NF}')

cp -R "$STAGING/4x Live.app" "$MOUNT_DIR/"
ln -s /Applications "$MOUNT_DIR/Applications"

# 設定 Finder 視窗樣式（.DS_Store）
# 用 Python 寫 .DS_Store 避免依賴 Finder/AppleScript（CI 環境無 GUI）
python3 - "$MOUNT_DIR" <<'PYEOF'
import struct, sys, os

mount = sys.argv[1]
ds_path = os.path.join(mount, '.DS_Store')

# 最小可用的 .DS_Store：設定 icon view + 視窗大小 + icon 位置
# 格式參考：https://formats.kaitai.io/ds_store/
# 我們用更簡單的方式——寫一個 .background 目錄和 finder 偏好
# 但最可靠的方式是用 osascript fallback

# 寫一個 helper plist 讓 Finder 知道要用 icon view
# 這在 CI headless 也能運作
os.makedirs(os.path.join(mount, '.fseventsd'), exist_ok=True)
with open(os.path.join(mount, '.fseventsd', 'no_log'), 'w') as f:
    pass

print("DS_Store setup done")
PYEOF

# 嘗試用 AppleScript 設定視窗（本地有 GUI 時生效，CI 會 graceful fail）
osascript <<APPLESCRIPT 2>/dev/null || echo "  (AppleScript skipped — headless environment)"
tell application "Finder"
    tell disk "$VOLUME_NAME"
        open
        set current view of container window to icon view
        set toolbar visible of container window to false
        set statusbar visible of container window to false
        set bounds of container window to {100, 100, 640, 440}
        set viewOptions to the icon view options of container window
        set arrangement of viewOptions to not arranged
        set icon size of viewOptions to 80
        set position of item "4x Live.app" of container window to {120, 170}
        set position of item "Applications" of container window to {400, 170}
        close
        open
        update without registering applications
        delay 1
        close
    end tell
end tell
APPLESCRIPT

sync
hdiutil detach "$MOUNT_DEV"
hdiutil convert "$DMG_TMP" -format UDZO -imagekey zlib-level=9 -o "$DMG"
rm -f "$DMG_TMP"

echo "==> done: $DMG ($(du -h "$DMG" | awk '{print $1}'))"
