#!/bin/sh
# Installs nodebench as a command, for servers you benchmark more than once:
#
#   curl -fsSL https://raw.githubusercontent.com/instantnodeeu/nodebench/main/install.sh | sh
#
# As root it goes to /usr/local/bin, otherwise to ~/.local/bin. BINDIR picks
# another directory, VERSION=v0.1.0 a tag instead of main. Run it again to update.
set -eu

REPO=instantnodeeu/nodebench
NAME=nodebench

die() { printf 'install: %s\n' "$*" >&2; exit 1; }

# fetch <url> <file>
fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --retry 3 -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO "$2" "$1"
	else
		die "needs curl or wget"
	fi
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

version=${VERSION:-main}
url=https://raw.githubusercontent.com/$REPO/$version/$NAME.sh
fetch "$url" "$tmp/$NAME" || die "download failed: $url"
# a proxy or captive portal can hand back html with a 200
head -n1 "$tmp/$NAME" | grep -q '^#!.*bash' || die "$url is not the script"

if [ -z "${BINDIR:-}" ]; then
	if [ "$(id -u)" = 0 ] || [ -w /usr/local/bin ]; then BINDIR=/usr/local/bin; else BINDIR=$HOME/.local/bin; fi
fi
mkdir -p "$BINDIR"
cp "$tmp/$NAME" "$BINDIR/.$NAME.new"
chmod 755 "$BINDIR/.$NAME.new"
mv -f "$BINDIR/.$NAME.new" "$BINDIR/$NAME"

echo "installed $NAME ($version) to $BINDIR/$NAME"
case :$PATH: in
	*:"$BINDIR":*) ;;
	*) echo "$BINDIR is not in your PATH, add it or run $BINDIR/$NAME" ;;
esac
echo "run this again to update, rm $BINDIR/$NAME to remove"
