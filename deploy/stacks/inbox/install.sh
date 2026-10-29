#!/usr/bin/env bash
set -euo pipefail
# shellcheck disable=SC2034 # scoped-stack.sh reads it
APP=inbox
STACK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$STACK_DIR/../../scoped-stack.sh"

install_native() {
    pct exec "$CT_ID" -- bash -euc '
        apt-get update -q
        apt-get install -y -q ffmpeg curl util-linux
        getent group 1000 >/dev/null || groupadd --gid 1000 iglo-inbox
        id iglo-inbox >/dev/null 2>&1 || useradd --uid 1000 --gid 1000 --system --home-dir /var/lib/iglo-inbox/worker --shell /usr/sbin/nologin iglo-inbox
        install -d -m 0700 -o iglo-inbox -g 1000 /var/lib/iglo-inbox/worker
        install -d -m 0755 /var/lib/iglo-inbox/admission
        install -d -m 0775 -o 1000 -g 1000 /data/videos/home-videos
        if [[ ! -f /var/lib/iglo-inbox/admission/admit-until ]]; then printf -- "-1\n" > /var/lib/iglo-inbox/admission/admit-until; fi
        chmod 0644 /var/lib/iglo-inbox/admission/admit-until
        install -m 0755 /opt/inbox/iglo-inbox /usr/local/bin/iglo-inbox
        install -m 0644 /opt/inbox/iglo-inbox.service /etc/systemd/system/iglo-inbox.service
        systemctl daemon-reload
        systemctl enable --now iglo-inbox
        for _ in $(seq 1 30); do
            if curl --fail --silent --max-time 2 http://127.0.0.1:8789/jobs >/dev/null; then exit 0; fi
            sleep 1
        done
        exit 1
    '
}
install_stack() {
    install_native
    log "Open Inbox work in Iglo-OS. Review the profile and resume the paused queue. Originals stay read only."
}
update_stack() {
    pct exec "$CT_ID" -- systemctl stop iglo-inbox
    if ! install_native; then
        pct exec "$CT_ID" -- systemctl start iglo-inbox || true
        die "Inbox update failed. Inspect the worker before retrying."
    fi
}
scoped_main
