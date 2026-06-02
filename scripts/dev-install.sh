#!/bin/sh
# dev-install.sh — build the vmrc host CLI from this checkout and symlink it onto
# your PATH. The symlink points at the repo's build output, so a later `make build`
# (or re-running this script) is picked up live without re-installing.
#
# For installing a *released* binary (no source checkout), use scripts/install.sh.
#
# Env:
#   VMRC_BIN_DIR   target dir for the symlink (default: first writable of
#                  $HOME/.local/bin, /usr/local/bin; else $HOME/.local/bin is created)
set -eu

# Resolve the repo root from this script's location so it works from any CWD.
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
cd "$repo_dir"

bin="$repo_dir/bin/vmrc"

echo "Building vmrc from $repo_dir ..."
if command -v make >/dev/null 2>&1; then
	make vmrc
else
	# Fallback: replicate the Makefile's stamped build without make.
	version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
	commit=$(git rev-parse --short HEAD 2>/dev/null || echo none)
	date=$(date -u +%Y-%m-%dT%H:%M:%SZ)
	go build -trimpath \
		-ldflags "-s -w -X main.version=$version -X main.commit=$commit -X main.date=$date" \
		-o "$bin" ./cmd/vmrc
fi

[ -x "$bin" ] || { echo "error: expected build output at $bin" >&2; exit 1; }

# Choose an install directory.
if [ -n "${VMRC_BIN_DIR:-}" ]; then
	dir="$VMRC_BIN_DIR"
	mkdir -p "$dir"
else
	dir=""
	for candidate in "$HOME/.local/bin" /usr/local/bin; do
		if [ -d "$candidate" ] && [ -w "$candidate" ]; then
			dir="$candidate"
			break
		fi
	done
	if [ -z "$dir" ]; then
		dir="$HOME/.local/bin"
		mkdir -p "$dir"
	fi
fi

ln -sf "$bin" "$dir/vmrc"
echo "Linked $dir/vmrc -> $bin"

# Warn if the install dir isn't on PATH.
case ":$PATH:" in
	*":$dir:"*) ;;
	*)
		echo
		echo "note: $dir is not on your PATH. Add it, e.g.:"
		echo "  export PATH=\"$dir:\$PATH\""
		;;
esac

echo
"$dir/vmrc" --version
