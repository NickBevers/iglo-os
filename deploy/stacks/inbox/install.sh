#!/usr/bin/env bash
set -euo pipefail
APP=inbox
STACK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$STACK_DIR/../../scoped-stack.sh"
install_stack() {
    pct exec "$CT_ID" -- bash -euc '
        apt-get install -y -q curl
        install -d -m 0775 -o 1000 -g 1000 /var/lib/iglo-inbox/{data,logs,common,temp,admission}
        install -d -m 0775 /data/videos/home-videos
        printf -- "-1\n" > /var/lib/iglo-inbox/admission/admit-until
        cd /opt/inbox
        docker compose config --quiet
        docker compose up -d
    '
    wait_inbox
    log "Open FileFlows: configure authentication, FFmpeg DockerMod, one runner, native schedule and /opt/inbox/pre-execute.cs. Sources are read only."
}
update_stack() {
    pct exec "$CT_ID" -- bash -euc '
        cd /opt/inbox
        # Stop before copying the application database. Keep this export on SSD
        # for guest backups; NAS originals/output remain separate.
        docker compose stop
        trap '\''docker compose start'\'' EXIT
        install -d -m 0700 /var/backups/iglo-inbox
        tar -czf "/var/backups/iglo-inbox/data-$(date -u +%Y%m%dT%H%M%SZ).tar.gz" -C /var/lib/iglo-inbox data
        docker compose pull
        docker compose up -d
    '
    wait_inbox
}
wait_inbox() {
    local url="http://$(container_ip "$CT_ID"):19200"
    for _ in $(seq 1 90); do if curl --fail --silent --max-time 5 "$url" >/dev/null; then return;fi;sleep 2;done
    die "FileFlows did not answer on port 19200"
}
scoped_main
