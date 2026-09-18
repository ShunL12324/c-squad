#!/usr/bin/env bash
# Build a signed, static APT repository from Debian packages. GNUPGHOME must
# contain the signing key; importing or creating a key is the caller's job.
set -euo pipefail
if [ "$#" -ne 3 ]; then
  echo 'Usage: build-apt-repo.sh PACKAGE_DIR OUTPUT_DIR SIGNING_KEY_FINGERPRINT' >&2
  exit 2
fi
packages=$(cd "$1" && pwd)
output=$2
key=$3
mkdir -p "$output"
output=$(cd "$output" && pwd)
if [ -n "$(ls -A "$output")" ]; then
  echo 'Output directory must be empty; refusing to replace an existing repository.' >&2
  exit 1
fi
for tool in apt-ftparchive dpkg-deb gpg gzip; do
  command -v "$tool" >/dev/null || { echo "Missing release tool: $tool" >&2; exit 1; }
done
shopt -s nullglob
archives=("$packages"/*.deb)
if [ "${#archives[@]}" -eq 0 ]; then
  echo 'No Debian packages found.' >&2
  exit 1
fi
mkdir -p "$output/pool/main/c/csquad"
for package in "${archives[@]}"; do
  name=$(dpkg-deb --field "$package" Package)
  arch=$(dpkg-deb --field "$package" Architecture)
  if [ "$name" != csquad ] || [[ "$arch" != amd64 && "$arch" != arm64 ]]; then
    echo "Unexpected package: $name ($arch)" >&2
    exit 1
  fi
  cp "$package" "$output/pool/main/c/csquad/"
done
cd "$output"
for arch in amd64 arm64; do
  directory="dists/stable/main/binary-$arch"
  mkdir -p "$directory"
  apt-ftparchive --arch "$arch" packages pool > "$directory/Packages"
  if [ ! -s "$directory/Packages" ]; then
    echo "Missing packages for $arch" >&2
    exit 1
  fi
  gzip -n -9 -c "$directory/Packages" > "$directory/Packages.gz"
done
apt-ftparchive \
  -o APT::FTPArchive::Release::Origin=C-Squad \
  -o APT::FTPArchive::Release::Label=C-Squad \
  -o APT::FTPArchive::Release::Suite=stable \
  -o APT::FTPArchive::Release::Codename=stable \
  -o APT::FTPArchive::Release::Architectures='amd64 arm64' \
  -o APT::FTPArchive::Release::Components=main \
  release dists/stable > dists/stable/Release
# CI uses a dedicated unencrypted signing key in an isolated GNUPGHOME.
gpg --batch --yes --local-user "$key" --clearsign --output dists/stable/InRelease dists/stable/Release
gpg --batch --yes --local-user "$key" --armor --detach-sign --output dists/stable/Release.gpg dists/stable/Release
gpg --batch --armor --export "$key" > key.asc
if [ ! -s key.asc ]; then
  echo 'Signing key export is empty.' >&2
  exit 1
fi
printf 'Signed APT repository: %s\n' "$output"
