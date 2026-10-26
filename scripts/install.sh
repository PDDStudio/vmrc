#!/bin/sh
# install.sh — install a released vmrc binary on Linux or macOS.
#
# Downloads the matching archive from this repo's GitHub Releases, verifies it
# against the release checksums.txt (SHA-256), and places the binary on your PATH.
# After the first install, the binary updates itself (`vmrc update --self` /
# `vmrc-agent update`).
#
# Quick use:
#   curl -fsSL https://raw.githubusercontent.com/pddstudio/vmrc/main/scripts/install.sh | sh
#   # install the agent instead of the CLI:
#   curl -fsSL .../install.sh | VMRC_BINARY=vmrc-agent sh
#
# Env:
#   VMRC_BINARY    vmrc (default) or vmrc-agent
#   VMRC_VERSION   release tag to install (default: latest, e.g. v0.2.0)
#   VMRC_BIN_DIR   install dir (default: /usr/local/bin, else $HOME/.local/bin)
#   GITHUB_TOKEN   optional GitHub token (raises the API rate limit)
set -eu

REPO="pddstudio/vmrc"
BINARY="${VMRC_BINARY:-vmrc}"
VERSION="${VMRC_VERSION:-}"

case "$BINARY" in
	vmrc|vmrc-agent) ;;
	*) echo "error: VMRC_BINARY must be 'vmrc' or 'vmrc-agent' (got '$BINARY')" >&2; exit 1 ;;
esac

# --- detect OS/arch (goreleaser GOOS/GOARCH names) ---------------------------
os=$(uname -s)
case "$os" in
	Linux)  os=linux ;;
	Darwin) os=darwin ;;
	*) echo "error: unsupported OS '$os'. On Windows use scripts/install.ps1." >&2; exit 1 ;;
esac

arch=$(uname -m)
case "$arch" in
	x86_64|amd64)   arch=amd64 ;;
	arm64|aarch64)  arch=arm64 ;;
	*) echo "error: unsupported architecture '$arch'" >&2; exit 1 ;;
esac

# --- http helper -------------------------------------------------------------
if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL ${GITHUB_TOKEN:+-H "Authorization: Bearer $GITHUB_TOKEN"} "$1"; }
	download() { curl -fsSL ${GITHUB_TOKEN:+-H "Authorization: Bearer $GITHUB_TOKEN"} -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -qO- ${GITHUB_TOKEN:+--header="Authorization: Bearer $GITHUB_TOKEN"} "$1"; }
	download() { wget -q ${GITHUB_TOKEN:+--header="Authorization: Bearer $GITHUB_TOKEN"} -O "$2" "$1"; }
else
	echo "error: need curl or wget" >&2; exit 1
fi

# --- resolve the release ------------------------------------------------------
if [ -n "$VERSION" ]; then
	api="https://api.github.com/repos/$REPO/releases/tags/$VERSION"
else
	api="https://api.github.com/repos/$REPO/releases/latest"
fi

echo "Querying $REPO releases ..."
json=$(fetch "$api") || { echo "error: failed to query GitHub releases API" >&2; exit 1; }

# Pull out all asset download URLs (no jq dependency).
urls=$(printf '%s\n' "$json" | grep -o '"browser_download_url":[[:space:]]*"[^"]*"' | sed 's/.*"\(https[^"]*\)"/\1/')
[ -n "$urls" ] || { echo "error: no assets found for the release" >&2; exit 1; }

# Match <binary>_<ver>_<os>_<arch>.tar.gz exactly. The '_' separator means 'vmrc'
# won't match a 'vmrc-agent_...' asset and vice versa.
asset_url=$(printf '%s\n' "$urls" | grep -E "/${BINARY}_[^/]*_${os}_${arch}\.tar\.gz$" | head -n1 || true)
checksums_url=$(printf '%s\n' "$urls" | grep -E '/checksums\.txt$' | head -n1 || true)

[ -n "$asset_url" ] || { echo "error: no $BINARY asset for ${os}/${arch} in this release" >&2; exit 1; }
[ -n "$checksums_url" ] || { echo "error: release has no checksums.txt; refusing to install unverified" >&2; exit 1; }

asset_name=$(basename "$asset_url")

# --- download + verify --------------------------------------------------------
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "Downloading $asset_name ..."
download "$asset_url" "$tmp/$asset_name"
download "$checksums_url" "$tmp/checksums.txt"

if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
	sha256() { shasum -a 256 "$1" | awk '{print $1}'; }
else
	echo "error: need sha256sum or shasum to verify the download" >&2; exit 1
fi

want=$(grep " $asset_name\$" "$tmp/checksums.txt" | awk '{print $1}' | head -n1 || true)
[ -n "$want" ] || { echo "error: $asset_name not listed in checksums.txt" >&2; exit 1; }
got=$(sha256 "$tmp/$asset_name")
if [ "$want" != "$got" ]; then
	echo "error: checksum mismatch for $asset_name" >&2
	echo "  expected $want" >&2
	echo "  got      $got" >&2
	exit 1
fi
echo "Checksum OK."

# --- extract + install --------------------------------------------------------
tar -xzf "$tmp/$asset_name" -C "$tmp"
binpath="$tmp/$BINARY"
[ -f "$binpath" ] || { echo "error: $BINARY not found inside the archive" >&2; exit 1; }

dir="${VMRC_BIN_DIR:-/usr/local/bin}"
mkdir -p "$dir" 2>/dev/null || true

if [ -w "$dir" ]; then
	install -m 0755 "$binpath" "$dir/$BINARY"
elif command -v sudo >/dev/null 2>&1; then
	echo "note: $dir is not writable; installing with sudo"
	sudo install -m 0755 "$binpath" "$dir/$BINARY"
else
	echo "error: $dir is not writable and sudo is unavailable." >&2
	echo "       Re-run with VMRC_BIN_DIR set to a writable dir, e.g.:" >&2
	echo "       curl -fsSL .../install.sh | VMRC_BIN_DIR=\"\$HOME/.local/bin\" sh" >&2
	exit 1
fi

echo
echo "$BINARY installed to $dir/$BINARY"
"$dir/$BINARY" --version 2>/dev/null || true

# PATH hint.
case ":$PATH:" in
	*":$dir:"*) ;;
	*) echo; echo "note: $dir is not on your PATH. Add it, e.g.:"; echo "  export PATH=\"$dir:\$PATH\"" ;;
esac

# Agent next-step.
if [ "$BINARY" = vmrc-agent ]; then
	echo
	echo "Next: enroll this VM on your tailnet and install the agent service."
	echo "Per-user service (default; can capture the desktop) — run as the user the"
	echo "agent should run as, WITHOUT sudo:"
	echo "  TS_AUTHKEY=tskey-... vmrc-agent setup --allow-login you@example.com"
	if [ "$(uname -s)" = Linux ]; then
		echo "  (a systemd user service starts at login; to start it at boot run once:"
		echo "   loginctl enable-linger \"\$USER\")"
	fi
	echo "Or a machine-wide boot service running as root:"
	echo "  sudo TS_AUTHKEY=tskey-... vmrc-agent setup --system --allow-login you@example.com"
fi
