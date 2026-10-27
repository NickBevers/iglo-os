#!/usr/bin/env bash
set -euo pipefail
# shellcheck disable=SC2034 # scoped-stack.sh reads it
APP=photos
STACK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$STACK_DIR/../../scoped-stack.sh"

install_stack() {
    pct exec "$CT_ID" -- bash -euc '
        cd /opt/photos
        apt-get install -y -q rsync curl
        install -d -m 0700 /var/lib/immich/{postgres,thumbs,encoded-video,model-cache}
        umask 077
        printf "IMMICH_VERSION=v3.3.5\nUPLOAD_LOCATION=/data/photos\nDB_DATA_LOCATION=/var/lib/immich/postgres\nDB_USERNAME=postgres\nDB_DATABASE_NAME=immich\nDB_PASSWORD=%s\n" "$(openssl rand -hex 32)" > .env
        # With the GPU of the host, Immich uses it for video and machine learning.
        if [[ -c /dev/dri/renderD128 ]]; then
            echo "COMPOSE_FILE=compose.yaml:compose.override.yaml:compose.gpu.yaml" >> .env
        fi
        docker compose config --quiet
        docker compose up -d --wait --wait-timeout 600
        curl --fail --silent http://127.0.0.1:2283/api/server/ping >/dev/null
    '
    log "Open Immich to create the first admin account. Then connect its queue API key in Background work."
}
update_stack() {
    pct exec "$CT_ID" -- bash -euc '
        cd /opt/photos
        sed -i "s/^IMMICH_VERSION=.*/IMMICH_VERSION=v3.3.5/" .env
        docker compose config --quiet
        docker compose pull
        docker compose up -d --wait --wait-timeout 600
        curl --fail --silent http://127.0.0.1:2283/api/server/ping >/dev/null
    '
}
scoped_main
