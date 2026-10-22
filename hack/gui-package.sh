#!/usr/bin/env bash
# Packages the Wails build output of gui/ into the files attached to a GitHub
# release. CI runs it after `wails build` on each platform
# (.github/workflows/gui-release.yml); it also works after `make gui`.
#
#   hack/gui-package.sh <version> <darwin|windows|linux>
#
# Files are written to gui/build/dist/.
set -euo pipefail

version=${1:?usage: hack/gui-package.sh <version> <darwin|windows|linux>}
platform=${2:?usage: hack/gui-package.sh <version> <darwin|windows|linux>}

root="$(cd "$(dirname "$0")/.." && pwd)"
bin="$root/gui/build/bin"
out="$root/gui/build/dist"
name="forgeai-$version"
mkdir -p "$out"

case "$platform" in
darwin)
  app="$bin/forgeai.app"
  # No Developer ID is available, so the bundle is signed ad hoc. This seals
  # its resources; Gatekeeper still asks for approval on first launch.
  codesign --force --deep --sign - "$app"
  stage="$(mktemp -d)"
  trap 'rm -rf "$stage"' EXIT
  cp -R "$app" "$stage/"
  ln -s /Applications "$stage/Applications"
  hdiutil create -quiet -volname "forgeai $version" -srcfolder "$stage" \
    -ov -format UDZO "$out/$name-macos-universal.dmg"
  ;;
windows)
  shopt -s nullglob
  installers=("$bin"/*-installer.exe)
  if (( ${#installers[@]} != 1 )); then
    echo "expected one NSIS installer in $bin, found ${#installers[@]}" >&2
    exit 1
  fi
  cp "${installers[0]}" "$out/$name-windows-amd64-setup.exe"
  cp "$bin/forgeai.exe" "$out/$name-windows-amd64-portable.exe"
  ;;
linux)
  tar -C "$bin" -czf "$out/$name-linux-amd64.tar.gz" forgeai

  pkg="$(mktemp -d)"
  trap 'rm -rf "$pkg"' EXIT
  install -Dm755 "$bin/forgeai" "$pkg/usr/bin/forgeai"
  install -Dm644 "$root/gui/build/appicon.png" \
    "$pkg/usr/share/icons/hicolor/1024x1024/apps/forgeai.png"
  install -d "$pkg/usr/share/applications"
  cat > "$pkg/usr/share/applications/forgeai.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=forgeai
Comment=Desktop client for forgeai
Exec=/usr/bin/forgeai
Icon=forgeai
Terminal=false
Categories=Development;
EOF
  install -d "$pkg/DEBIAN"
  cat > "$pkg/DEBIAN/control" <<EOF
Package: forgeai
Version: $version
Section: devel
Priority: optional
Architecture: amd64
Maintainer: Taizeng Wu <wutaizeng@gmail.com>
Depends: libgtk-3-0, libwebkit2gtk-4.1-0
Description: Desktop client for forgeai
 Manages forgeai sandboxes, jobs, volumes and images in a project namespace
 through a kubeconfig context.
EOF
  dpkg-deb --root-owner-group --build "$pkg" "$out/$name-linux-amd64.deb"
  ;;
*)
  echo "unknown platform: $platform" >&2
  exit 1
  ;;
esac
