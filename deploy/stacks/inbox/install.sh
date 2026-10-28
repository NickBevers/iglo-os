#!/usr/bin/env bash
set -euo pipefail
# shellcheck disable=SC2034 # scoped-stack.sh reads it
APP=inbox
STACK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$STACK_DIR/../../scoped-stack.sh"

native_ready() {
    pct exec "$CT_ID" -- test -f /var/lib/iglo-inbox/worker/native-ready
}
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
            if curl --fail --silent --max-time 2 http://127.0.0.1:8789/jobs >/dev/null; then
                touch /var/lib/iglo-inbox/worker/native-ready
                exit 0
            fi
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
    if native_ready; then
        pct exec "$CT_ID" -- systemctl stop iglo-inbox
        if ! install_native; then
            pct exec "$CT_ID" -- systemctl start iglo-inbox || true
            die "Native Inbox update failed. Inspect the worker before retrying."
        fi
        return
    fi
    # An old FileFlows stack keeps its behavior until migration is requested.
    pct exec "$CT_ID" -- bash -euc '
        cd /opt/inbox
        docker compose stop
        trap '\''docker compose start'\'' EXIT
        install -d -m 0700 /var/backups/iglo-inbox
        tar -czf "/var/backups/iglo-inbox/data-$(date -u +%Y%m%dT%H%M%SZ).tar.gz" -C /var/lib/iglo-inbox data
        docker compose pull
        docker compose up -d
    '
}
rollback_native_migration() {
    pct exec "$CT_ID" -- bash -c 'systemctl disable --now iglo-inbox || true;rm -f /var/lib/iglo-inbox/worker/native-ready;cd /opt/inbox;docker compose start' || true
}
migrate_stack() {
    ! native_ready || die "Inbox already uses the native worker"
    # Load checked bindings again; no NAS path is supplied by the caller.
    local saved tags
    tags="$(pct config "$CT_ID" | sed -n 's/^tags: //p')"
    [[ ";$tags;" == *";iglo;"* && ";$tags;" == *";inbox;"* ]] || die "Could not read the managed Inbox tags"
    [[ ";$tags;" == *";inbox-native;"* ]] || tags="$tags;inbox-native"
    saved=$(mktemp)
    pct pull "$CT_ID" /etc/iglo-stack.conf "$saved"
    load_scoped_answers "$saved";rm -f "$saved"
    check_scoped_answers
    # Archive while FileFlows is stopped. Leave its files and compose stack
    # intact for deliberate rollback; migration never imports unknown flows.
    pct exec "$CT_ID" -- bash -euc '
        cd /opt/inbox
        docker compose stop
        trap '\''docker compose start'\'' ERR
        install -d -m 0700 /var/backups/iglo-inbox
        archive="/var/backups/iglo-inbox/fileflows-$(date -u +%Y%m%dT%H%M%SZ).tar.gz"
        tar -czf "$archive.partial" -C /var/lib/iglo-inbox data
        mv "$archive.partial" "$archive"
    '
    # Roll back any later failure, including copying files or setting tags.
    trap rollback_native_migration EXIT
    scoped_files
    install_native
    pct set "$CT_ID" --tags "$tags" --description "$(app_description "$APP, installed by Iglo-OS")"
    trap - EXIT
    pct exec "$CT_ID" -- bash -euc '
        # Only retire Docker when the dedicated guest contains no other
        # containers. Retain packages and app files for deliberate rollback.
        if [[ $(docker ps -aq | wc -l) -eq 1 && -z $(docker ps -q) ]]; then
            systemctl disable --now docker.service docker.socket containerd.service
        fi
    ' || log "Could not retire Docker. Native Inbox is ready; inspect the guest services."
    log "Native Inbox is ready with a paused queue. FileFlows data and its archive stay on SSD. Choose a profile and resume from Inbox work."
}
scoped_main
