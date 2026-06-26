#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLIENT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
PIN_FILE="$CLIENT_DIR/tools/odin.version"

if [[ ! -f "$PIN_FILE" ]]; then
	echo "bootstrap_odin: pin file not found: $PIN_FILE"
	exit 1
fi

PINNED_VERSION="$(tr -d '[:space:]' < "$PIN_FILE")"
if [[ -z "$PINNED_VERSION" ]]; then
	echo "bootstrap_odin: empty pin version in $PIN_FILE"
	exit 1
fi

INSTALL_DIR="$CLIENT_DIR/.tools/odin/$PINNED_VERSION"
ODIN_BIN="$INSTALL_DIR/odin"

check_installed_version() {
	if [[ ! -x "$ODIN_BIN" ]]; then
		return 1
	fi
	local line version tag
	line="$("$ODIN_BIN" version 2>/dev/null || true)"
	version="${line##* }"
	tag="${version%%:*}"
	[[ "$tag" == "$PINNED_VERSION" ]]
}

download_release() {
	local os arch api_url release_json asset_url

	case "$(uname -s)" in
		Darwin) os="macos" ;;
		Linux) os="linux" ;;
		*)
			echo "bootstrap_odin: unsupported OS $(uname -s)"
			echo "Manual install: download Odin $PINNED_VERSION and place it under $INSTALL_DIR"
			return 1
			;;
	esac

	case "$(uname -m)" in
		x86_64|amd64) arch="amd64" ;;
		arm64|aarch64) arch="arm64" ;;
		*)
			echo "bootstrap_odin: unsupported arch $(uname -m)"
			echo "Manual install: download Odin $PINNED_VERSION and place it under $INSTALL_DIR"
			return 1
			;;
	esac

	api_url="https://api.github.com/repos/odin-lang/Odin/releases/tags/$PINNED_VERSION"
	if ! release_json="$(curl -fsSL "$api_url")"; then
		echo "bootstrap_odin: failed to fetch release metadata for $PINNED_VERSION"
		echo "Manual install: download Odin release and install under $INSTALL_DIR"
		return 1
	fi

	asset_url="$(
		RELEASE_JSON="$release_json" python3 - "$os" "$arch" <<'PY'
import json
import os
import sys

os_name = sys.argv[1]
arch = sys.argv[2]
raw = os.environ.get("RELEASE_JSON", "")
if not raw:
    print("")
    raise SystemExit(0)

try:
    data = json.loads(raw)
except json.JSONDecodeError:
    print("")
    raise SystemExit(0)

needle = f"odin-{os_name}-{arch}"
assets = [a.get("browser_download_url", "") for a in data.get("assets", [])]
for url in assets:
    if needle in url and (url.endswith(".tar.gz") or url.endswith(".zip")):
        print(url)
        break
else:
    print("")
PY
		)"

	if [[ -z "$asset_url" ]]; then
		echo "bootstrap_odin: no release asset found for $PINNED_VERSION ($os/$arch)"
		echo "Manual install: download Odin release and install under $INSTALL_DIR"
		return 1
	fi

	local tmpdir archive extract_dir odin_path bundle_root
	tmpdir="$(mktemp -d)"
	archive="$tmpdir/odin-archive"
	extract_dir="$tmpdir/extract"
	mkdir -p "$extract_dir"
	trap 'rm -rf "$tmpdir"' RETURN

	if ! curl -fL "$asset_url" -o "$archive"; then
		echo "bootstrap_odin: failed to download $asset_url"
		echo "Manual install: download Odin release and install under $INSTALL_DIR"
		return 1
	fi

	case "$asset_url" in
		*.tar.gz)
			tar -xzf "$archive" -C "$extract_dir"
			;;
		*.zip)
			unzip -q "$archive" -d "$extract_dir"
			if [[ -f "$extract_dir/dist.tar.gz" ]]; then
				mkdir -p "$extract_dir/dist"
				tar -xzf "$extract_dir/dist.tar.gz" -C "$extract_dir/dist"
			fi
			;;
		*)
			echo "bootstrap_odin: unsupported archive format in $asset_url"
			return 1
			;;
	esac

	odin_path="$(find "$extract_dir" -type f -name odin | head -n1 || true)"
	if [[ -z "$odin_path" ]]; then
		echo "bootstrap_odin: odin binary not found after extraction"
		return 1
	fi

	bundle_root="$(cd "$(dirname "$odin_path")" && pwd)"
	rm -rf "$INSTALL_DIR"
	mkdir -p "$INSTALL_DIR"
	cp -R "$bundle_root"/. "$INSTALL_DIR"/
	chmod +x "$ODIN_BIN"
}

if check_installed_version; then
	echo "bootstrap_odin: pinned Odin already installed at $ODIN_BIN"
else
	echo "bootstrap_odin: installing Odin $PINNED_VERSION into $INSTALL_DIR"
	download_release
fi

echo "bootstrap_odin: local PATH hint:"
echo "  export PATH=\"$INSTALL_DIR:\$PATH\""

make -C "$CLIENT_DIR" doctor ODIN="$ODIN_BIN"
