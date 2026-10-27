#!/usr/bin/env bash
set -euo pipefail
# shellcheck disable=SC2034 # scoped-stack.sh reads it
APP=music
STACK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$STACK_DIR/../../scoped-stack.sh"
install_dependencies() {
    pct exec "$CT_ID" -- bash -euc '
        apt-get update -q
        apt-get install -y -q ffmpeg curl unzip ca-certificates
        tmp=$(mktemp -d);trap '\''rm -rf "$tmp"'\'' EXIT
        curl --fail --location --proto "=https" https://github.com/yt-dlp/yt-dlp/releases/download/2026.10.21/yt-dlp_linux -o "$tmp/yt-dlp"
        printf "d8bd33fca9d5b23bd5b9952b3fca36db5c46331933fda9f598a3da14ce2c243f  %s\n" "$tmp/yt-dlp" | sha256sum --check --strict
        curl --fail --location --proto "=https" https://github.com/denoland/deno/releases/download/v2.9.8/deno-x86_64-unknown-linux-gnu.zip -o "$tmp/deno.zip"
        printf "e890975bc2549807bbcc0b584ffafda72cc7c93349dd99c80483dd784f6c4609  %s\n" "$tmp/deno.zip" | sha256sum --check --strict
        unzip -q "$tmp/deno.zip" -d "$tmp"
        install -m 0755 "$tmp/yt-dlp" "$tmp/deno" /usr/local/bin/
        /usr/local/bin/yt-dlp --version
        /usr/local/bin/deno --version
    '
}
install_stack() {
    install_dependencies
    pct exec "$CT_ID" -- bash -euc '
        groupadd --gid 1000 iglo-music
        useradd --uid 1000 --gid 1000 --system --home-dir /var/lib/iglo-music --shell /usr/sbin/nologin iglo-music
        install -d -m 0700 -o iglo-music -g iglo-music /var/lib/iglo-music
        printf -- "-1\n" > /var/lib/iglo-music/admit-until
        install -m 0755 /opt/music/iglo-music /usr/local/bin/iglo-music
        install -m 0644 /opt/music/iglo-music.service /etc/systemd/system/iglo-music.service
        systemctl daemon-reload;systemctl enable --now iglo-music
        curl --fail --silent http://127.0.0.1:8788/jobs >/dev/null
    '
}
update_stack() {
    install_dependencies
    pct exec "$CT_ID" -- bash -euc '
        systemctl stop iglo-music
        install -m 0755 /opt/music/iglo-music /usr/local/bin/iglo-music
        install -m 0644 /opt/music/iglo-music.service /etc/systemd/system/iglo-music.service
        systemctl daemon-reload;systemctl start iglo-music
        curl --fail --silent http://127.0.0.1:8788/jobs >/dev/null
    '
}
scoped_main
