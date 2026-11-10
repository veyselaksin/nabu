#!/bin/sh
# Download the ONNX Runtime shared library that onnxruntime_go v1.38.0 expects
# (ORT 1.31.0, MIT license) into $1 (default .ort/), verifying its SHA-256.
# Prints the library path. Used by `make ort` and api/Dockerfile.
#
# Checksums are GitHub's asset digests for
# https://github.com/microsoft/onnxruntime/releases/tag/v1.31.0
set -eu
VERSION=1.31.0
DEST=${1:-.ort}
os=$(uname -s)
arch=$(uname -m)
case "$os/$arch" in
  Linux/x86_64)
    pkg=onnxruntime-linux-x64-$VERSION
    sum=cc5c72baf5ae5c8238a6841f0897227be2d02826b9cf98eaf02fdefaeeb03a57
    lib=libonnxruntime.so.$VERSION ;;
  Linux/aarch64|Linux/arm64)
    pkg=onnxruntime-linux-aarch64-$VERSION
    sum=c5b8b3cca31f3d643a3b313b8f42e2d0f16a4fcefd02cda343d97bd5306afaab
    lib=libonnxruntime.so.$VERSION ;;
  Darwin/arm64)
    pkg=onnxruntime-osx-arm64-$VERSION
    sum=1031b36dd3bdaa8d976a6130d9cf2a05e9be320b93ae804f8427b91dc3432afc
    lib=libonnxruntime.$VERSION.dylib ;;
  *)
    echo "fetch-ort: no pinned ONNX Runtime $VERSION build for $os/$arch;" >&2
    echo "download it from the release page and set NABU_ORT_LIB." >&2
    exit 1 ;;
esac
if [ -f "$DEST/lib/$lib" ]; then
  echo "$DEST/lib/$lib"
  exit 0
fi
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
url=https://github.com/microsoft/onnxruntime/releases/download/v$VERSION/$pkg.tgz
echo "fetch-ort: downloading $url" >&2
curl -fsSL -o "$tmp/ort.tgz" "$url"
if command -v sha256sum >/dev/null 2>&1; then
  got=$(sha256sum "$tmp/ort.tgz" | cut -d' ' -f1)
else
  got=$(shasum -a 256 "$tmp/ort.tgz" | cut -d' ' -f1)
fi
if [ "$got" != "$sum" ]; then
  echo "fetch-ort: checksum mismatch for $pkg.tgz: got $got, want $sum" >&2
  exit 1
fi
tar -xzf "$tmp/ort.tgz" -C "$tmp"
mkdir -p "$DEST/lib"
cp "$tmp/$pkg/lib/$lib" "$DEST/lib/$lib"
cp "$tmp/$pkg/LICENSE" "$DEST/LICENSE"
[ -f "$tmp/$pkg/ThirdPartyNotices.txt" ] && cp "$tmp/$pkg/ThirdPartyNotices.txt" "$DEST/"
echo "$DEST/lib/$lib"
