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

ARM_BIN="bin/4x-darwin-arm64"
AMD_BIN="bin/4x-darwin-amd64"

for f in "$ARM_BIN" "$AMD_BIN"; do
  if [ ! -f "$f" ]; then
    echo "ERROR: required binary $f not found; cross-compile it first" >&2
    exit 1
  fi
done

rm -rf "$BUILD"
mkdir -p "$MACOS_DIR" "$DIST"

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

# 3. 寫 Info.plist
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
    <key>LSMinimumSystemVersion</key>
    <string>13.0</string>
    <key>NSHighResolutionCapable</key>
    <true/>
</dict>
</plist>
PLIST

# 4. hdiutil 產 dmg
echo "==> hdiutil: creating dmg"
DMG="$DIST/4x-Live.dmg"
rm -f "$DMG"
hdiutil create -volname "4x Live" -srcfolder "$APP" -ov -format UDZO "$DMG"

echo "==> done: $DMG"
