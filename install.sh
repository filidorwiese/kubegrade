#!/bin/sh
# Installs the latest kubegrade release for this machine after verifying
# its sha256 against the published checksums. Rerun to update.
#
#   curl -fsSL https://raw.githubusercontent.com/filidorwiese/kubegrade/main/install.sh | sh
#
# KUBEGRADE_INSTALL_DIR overrides the target directory; the default is
# /usr/local/bin when writable, else ~/.local/bin.
set -eu

base=https://github.com/filidorwiese/kubegrade/releases/latest/download
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
asset="kubegrade_${os}_${arch}"

dir=${KUBEGRADE_INSTALL_DIR:-}
if [ -z "$dir" ]; then
	dir=/usr/local/bin
	[ -w "$dir" ] || dir="$HOME/.local/bin"
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
cd "$tmp"
echo "downloading $asset"
curl -fsSL -o "$asset" "$base/$asset"
curl -fsSL -o checksums.txt "$base/checksums.txt"

# macOS ships shasum, most Linux ships sha256sum; either verifies the line.
if command -v sha256sum >/dev/null; then
	grep " $asset\$" checksums.txt | sha256sum -c --quiet -
else
	grep " $asset\$" checksums.txt | shasum -a 256 -c --quiet -
fi

mkdir -p "$dir"
chmod +x "$asset"
mv "$asset" "$dir/kubegrade"
echo "installed kubegrade $("$dir/kubegrade" version) to $dir"
case ":$PATH:" in
*":$dir:"*) ;;
*) echo "add $dir to your PATH" ;;
esac
