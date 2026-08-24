#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "unsupported_platform: macOS product build requires Darwin" >&2
  exit 1
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PROJECT="$REPO_ROOT/macos/DevBoardApp/DevBoardApp.xcodeproj"
BUILD_ROOT="$REPO_ROOT/macos/build"
DERIVED_DATA="$REPO_ROOT/macos/.derived-data"
DIST_DIR="$REPO_ROOT/dist"
ARM_HELPER="$BUILD_ROOT/devboard-arm64"
INTEL_HELPER="$BUILD_ROOT/devboard-x86_64"
UNIVERSAL_HELPER="$BUILD_ROOT/devboard-bootstrap"
MODEL_SELF_TEST="$BUILD_ROOT/models-decode-self-test"
DMG_OUTPUT="$DIST_DIR/DevBoard.dmg"
DMG_STAGE_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/devboard-dmg-stage.XXXXXX")"
DMG_STAGE="$DMG_STAGE_ROOT/DevBoard"
DMG_MOUNT="$(mktemp -d "${TMPDIR:-/tmp}/devboard-dmg-mount.XXXXXX")"
DMG_MOUNTED=0

cleanup() {
  if [[ "$DMG_MOUNTED" == 1 ]]; then
    hdiutil detach "$DMG_MOUNT" >/dev/null || true
  fi
  rm -rf -- "$DMG_STAGE_ROOT" "$DMG_MOUNT"
}
trap cleanup EXIT

rm -rf -- "$BUILD_ROOT" "$DERIVED_DATA"
mkdir -p "$BUILD_ROOT" "$DIST_DIR"

echo "==> Building Go product helper for arm64"
(cd "$REPO_ROOT" && GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o "$ARM_HELPER" ./cmd/devboard)
echo "==> Building Go product helper for x86_64"
(cd "$REPO_ROOT" && GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o "$INTEL_HELPER" ./cmd/devboard)
lipo -create "$ARM_HELPER" "$INTEL_HELPER" -output "$UNIVERSAL_HELPER"
chmod 0755 "$UNIVERSAL_HELPER"

HELPER_ARCHES="$(lipo -archs "$UNIVERSAL_HELPER")"
contains_arch() { case " $1 " in *" $2 "*) return 0 ;; *) return 1 ;; esac; }
contains_arch "$HELPER_ARCHES" arm64
contains_arch "$HELPER_ARCHES" x86_64

echo "==> Verifying Swift product-result decoding"
xcrun swiftc \
  "$REPO_ROOT/macos/DevBoardApp/DevBoardApp/Models.swift" \
  "$REPO_ROOT/macos/DevBoardApp/DevBoardApp/NodeController.swift" \
  "$REPO_ROOT/macos/DevBoardApp/Tests/ModelsDecodeSelfTest.swift" \
  -o "$MODEL_SELF_TEST"
"$MODEL_SELF_TEST"

echo "==> Building SwiftUI application"
xcodebuild \
  -project "$PROJECT" \
  -scheme DevBoard \
  -configuration Release \
  -derivedDataPath "$DERIVED_DATA" \
  ARCHS="arm64 x86_64" \
  ONLY_ACTIVE_ARCH=NO \
  CODE_SIGN_IDENTITY="-" \
  CODE_SIGNING_ALLOWED=YES \
  build

APP="$DERIVED_DATA/Build/Products/Release/DevBoard.app"
APP_HELPER="$APP/Contents/Resources/devboard-bootstrap"
mkdir -p "$(dirname "$APP_HELPER")"
cp "$UNIVERSAL_HELPER" "$APP_HELPER"
chmod 0755 "$APP_HELPER"

echo "==> Ad-hoc signing dogfood app"
codesign --force --deep --sign - "$APP_HELPER"
codesign --force --deep --sign - "$APP"
codesign --verify --deep --strict "$APP"

APP_ARCHES="$(lipo -archs "$APP/Contents/MacOS/DevBoard")"
HELPER_ARCHES="$(lipo -archs "$APP_HELPER")"
contains_arch "$APP_ARCHES" arm64
contains_arch "$APP_ARCHES" x86_64
contains_arch "$HELPER_ARCHES" arm64
contains_arch "$HELPER_ARCHES" x86_64

OUTPUT="$DIST_DIR/DevBoard-macos-universal.zip"
rm -f -- "$OUTPUT"
ditto -c -k --sequesterRsrc --keepParent "$APP" "$OUTPUT"
test -s "$OUTPUT"

echo "==> Checking packaged app has no source/config/secret artifacts"
if find "$APP" -type f \( -name '*.go' -o -name '*.yaml' -o -name '*.yml' -o -name '*.env' -o -name '*.log' -o -name '*token*' -o -name '*key*' -o -name '*cookie*' -o -name '*Playwright*' -o -name '*DerivedData*' \) -print -quit | grep -q .; then
  echo "package_contains_sensitive_or_source_artifact" >&2
  exit 1
fi

echo "==> Creating INTERNAL DOGFOOD DMG"
mkdir -p "$DMG_STAGE"
ditto "$APP" "$DMG_STAGE/DevBoard.app"
ln -s /Applications "$DMG_STAGE/Applications"
rm -f -- "$DMG_OUTPUT"
hdiutil create -volname DevBoard -srcfolder "$DMG_STAGE" -ov -format UDZO "$DMG_OUTPUT" >/dev/null
hdiutil verify "$DMG_OUTPUT"

echo "==> Verifying read-only DMG manifest"
hdiutil attach -nobrowse -readonly -mountpoint "$DMG_MOUNT" "$DMG_OUTPUT" >/dev/null
DMG_MOUNTED=1
test -d "$DMG_MOUNT/DevBoard.app"
test -L "$DMG_MOUNT/Applications"
test "$(readlink "$DMG_MOUNT/Applications")" = "/Applications"
if find "$DMG_MOUNT" -type f \( -name '*.go' -o -name '*.yaml' -o -name '*.yml' -o -name '*.env' -o -name '*.log' -o -name '*token*' -o -name '*key*' -o -name '*cookie*' -o -name '*Playwright*' -o -name '*DerivedData*' \) -print -quit | grep -q .; then
  echo "dmg_contains_sensitive_or_source_artifact" >&2
  exit 1
fi
hdiutil detach "$DMG_MOUNT" >/dev/null
DMG_MOUNTED=0

echo "==> INTERNAL DOGFOOD artifact summary"
echo "DMG=$DMG_OUTPUT"
echo "DMG_SHA256=$(shasum -a 256 "$DMG_OUTPUT" | awk '{print $1}')"
echo "DMG_BYTES=$(stat -f %z "$DMG_OUTPUT")"
echo "APP_ARCHES=$APP_ARCHES"
echo "HELPER_ARCHES=$HELPER_ARCHES"
echo "SIGNATURE=$(codesign -dv --verbose=4 "$APP" 2>&1 | awk -F= '/^Signature=/{print $2; exit}')"
echo "distribution=INTERNAL DOGFOOD; Developer ID/notarization/staple intentionally not attempted"
echo "==> Wrote $OUTPUT and $DMG_OUTPUT"
