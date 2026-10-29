#!/usr/bin/env bash
# deploy.sh puts this commit on your Proxmox host: "make deploy HOST=root@pve".
# It runs on your own machine, not on the host. It checks the tree, runs every
# check of AGENTS.md, builds and signs the bundle, copies it over SSH, and has
# the host agent install it. A host without Iglo-OS gets the first install.
#
# The private key never leaves this machine. IGLO_SIGNING_KEY_CMD prints it;
# the default decrypts ~/.config/iglo/signing-key.age with age.
# Written for bash 3.2, because macOS ships that.
set -euo pipefail

HOST="${1:-}"
KEY_CMD="${IGLO_SIGNING_KEY_CMD:-age -d $HOME/.config/iglo/signing-key.age}"
REMOTE_DIR=/root/iglo-deploy
ADMIN_SOCKET=/run/iglo-agent/admin.sock

die() {
    echo "deploy: $*" >&2
    exit 1
}
step() { echo "==> $*"; }

[[ -n "$HOST" ]] || die "give the host, like: make deploy HOST=root@pve"
cd "$(dirname "$0")/.."

step "Checking the tree"
# Everything that goes into the bundle must be committed, also new files.
if [[ -n "$(git status --porcelain -- cmd internal web dev deploy Makefile go.mod go.sum)" ]]; then
    die "commit or stash your changes first; the bundle must match a commit"
fi
VERSION="$(git describe --tags --exact-match HEAD 2>/dev/null || true)"
[[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "tag this commit with a newer version first, like: git tag -a v0.1.0 -m v0.1.0"

step "Running the checks of AGENTS.md"
make web
make test
go test -race ./internal/... ./cmd/...
unformatted="$(gofmt -l cmd internal dev web/embed.go)"
[[ -z "$unformatted" ]] || die "run gofmt on: $unformatted"
(cd web && npx prettier --check "**/*.{ts,tsx}")
git ls-files '*.sh' | while read -r script; do bash -n "$script"; done
make shellcheck

step "Building $VERSION"
make bundle VERSION="$VERSION"
BUNDLE="dist/iglo-$VERSION-linux-amd64.tar.gz"
[[ -f "$BUNDLE" ]] || die "make bundle made no $BUNDLE"

step "Signing"
# The key lives only in this one command's environment.
key="$(sh -c "$KEY_CMD")" || die "could not read the signing key with: $KEY_CMD"
IGLO_SIGNING_KEY="$key" go run ./cmd/iglo-release sign -version "$VERSION" "$BUNDLE"
key=""

step "Copying to $HOST"
ssh "$HOST" "install -d -m 0700 $REMOTE_DIR"
scp -q "$BUNDLE" "$BUNDLE.sig" "$HOST:$REMOTE_DIR/"
remote="$REMOTE_DIR/$(basename "$BUNDLE")"

if ssh "$HOST" "test -S $ADMIN_SOCKET"; then
    step "Upgrading"
    # The agent checks the signature and that the version is newer, and
    # rolls back when the new version doesn't work.
    ssh "$HOST" "iglo-agent upgrade $remote $remote.sig && rm -f $remote $remote.sig"
elif ssh "$HOST" "test -x /usr/local/bin/iglo-agent"; then
    die "the host runs an agent without the upgrade socket; upgrade it once by hand: tar xzf $remote, then ./install.sh --upgrade"
else
    step "First install"
    # The first install asks for the admin account, so it needs a terminal.
    ssh -t "$HOST" "cd $REMOTE_DIR && tar xzf $remote && cd iglo-$VERSION-linux-amd64 && ./install.sh"
fi

step "Done: $HOST runs $VERSION"
