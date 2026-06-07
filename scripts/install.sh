#!/bin/sh
set -eu

REPO="parseablehq/pb"
BINARY_NAME="pb"
INSTALL_DIR="${INSTALL_DIR:-"$HOME/.local/bin"}"
VERSION="${VERSION:-latest}"

need_cmd() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "error: required command not found: $1" >&2
		exit 1
	fi
}

download() {
	url="$1"
	output="$2"

	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$url" -o "$output"
	elif command -v wget >/dev/null 2>&1; then
		wget -q "$url" -O "$output"
	else
		echo "error: curl or wget is required" >&2
		exit 1
	fi
}

checksum() {
	file="$1"

	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$file" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$file" | awk '{print $1}'
	else
		echo "error: sha256sum or shasum is required" >&2
		exit 1
	fi
}

detect_user_shell() {
	if [ -n "${SHELL:-}" ]; then
		printf '%s\n' "${SHELL##*/}"
	else
		printf '%s\n' "sh"
	fi
}

print_path_instructions() {
	install_dir="$1"
	shell_name="$(detect_user_shell)"

	echo ""
	case "$shell_name" in
	bash)
		echo "$install_dir is not in your PATH. Add it with:"
		echo ""
		echo "  echo 'export PATH=\"$install_dir:\$PATH\"' >> ~/.bashrc"
		echo "  . ~/.bashrc"
		;;
	zsh)
		echo "$install_dir is not in your PATH. Add it with:"
		echo ""
		echo "  echo 'export PATH=\"$install_dir:\$PATH\"' >> ~/.zshrc"
		echo "  source ~/.zshrc"
		;;
	fish)
		echo "$install_dir is not in your PATH. Add it with:"
		echo ""
		echo "  mkdir -p ~/.config/fish"
		echo "  echo 'fish_add_path $install_dir' >> ~/.config/fish/config.fish"
		echo "  source ~/.config/fish/config.fish"
		;;
	*)
		echo "$install_dir is not in your PATH. Add it to your shell startup file:"
		echo ""
		echo "  export PATH=\"$install_dir:\$PATH\""
		;;
	esac
}

install_binary() {
	src="$1"
	dst="$2"

	if mkdir -p "$INSTALL_DIR" 2>/dev/null && mv "$src" "$dst" 2>/dev/null; then
		return
	fi

	if ! command -v sudo >/dev/null 2>&1; then
		echo "error: cannot write to $INSTALL_DIR and sudo is not available" >&2
		exit 1
	fi

	sudo mkdir -p "$INSTALL_DIR"
	sudo mv "$src" "$dst"
}

case "$(uname -s)" in
Darwin) os="darwin" ;;
Linux) os="linux" ;;
*)
	echo "error: unsupported OS. Download the Windows archive from the releases page if you are on Windows." >&2
	exit 1
	;;
esac

case "$(uname -m)" in
x86_64 | amd64) arch="amd64" ;;
arm64 | aarch64) arch="arm64" ;;
*)
	echo "error: unsupported architecture: $(uname -m)" >&2
	exit 1
	;;
esac

need_cmd tar
need_cmd awk

if [ "$VERSION" = "latest" ]; then
	if command -v curl >/dev/null 2>&1; then
		latest_url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest")"
	elif command -v wget >/dev/null 2>&1; then
		latest_url="$(wget -qO- --server-response "https://github.com/$REPO/releases/latest" 2>&1 | awk '/^  Location: / { url=$2 } END { print url }')"
	else
		echo "error: curl or wget is required" >&2
		exit 1
	fi
	VERSION="${latest_url##*/}"
fi

VERSION="${VERSION#v}"
asset="${BINARY_NAME}_${VERSION}_${os}_${arch}.tar.gz"
checksums="${BINARY_NAME}_${VERSION}_checksums.txt"
base_url="https://github.com/$REPO/releases/download/v$VERSION"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT INT TERM

echo "Downloading $asset..."
download "$base_url/$asset" "$tmpdir/$asset"
download "$base_url/$checksums" "$tmpdir/$checksums"

expected="$(awk -v file="$asset" '$2 == file { print $1 }' "$tmpdir/$checksums")"
if [ -z "$expected" ]; then
	echo "error: checksum for $asset not found in $checksums" >&2
	exit 1
fi

actual="$(checksum "$tmpdir/$asset")"
if [ "$actual" != "$expected" ]; then
	echo "error: checksum verification failed for $asset" >&2
	exit 1
fi

tar -xzf "$tmpdir/$asset" -C "$tmpdir"
chmod +x "$tmpdir/$BINARY_NAME"

install_binary "$tmpdir/$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"

echo "$BINARY_NAME installed to $INSTALL_DIR/$BINARY_NAME"
case ":$PATH:" in
*":$INSTALL_DIR:"*) ;;
*) print_path_instructions "$INSTALL_DIR" ;;
esac
