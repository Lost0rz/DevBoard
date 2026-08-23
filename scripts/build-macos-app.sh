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
echo "==> Wrote $OUTPUT"
