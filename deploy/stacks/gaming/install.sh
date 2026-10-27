#!/usr/bin/env bash
# Experimental Gaming app: Wolf with ES-DE in a privileged container, from the
# Wolf Proxmox quickstart. The Apps page only starts it after an explicit
# confirmation of the privileged container. See docs/gaming.md.
set -euo pipefail
APP=gaming
STACK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$STACK_DIR/../../scoped-stack.sh"
ESDE_IMAGE=ghcr.io/games-on-whales/es-de:edge@sha256:b718b0a0782a98c6f4a761bc43b2ac984d732f3e2f647ee4614c4986db6b194d
install_stack() {
    pct exec "$CT_ID" -- bash -euc '
        apt-get install -y -q curl vainfo mesa-va-drivers mesa-vulkan-drivers vulkan-tools
        install -d -m 0700 /etc/wolf/cfg
        install -d -m 0755 /var/run/wolf
        # hidraw, for virtual pads and Wii Remotes, gets its device number when the
        # kernel loads the driver. 244 is the number of the Wolf quickstart.
        hidraw=$(sed -n "s/^ *\([0-9][0-9]*\) hidraw\$/\1/p" /proc/devices)
        sed -e "s/IGLO_UUID/$(cat /proc/sys/kernel/random/uuid)/" -e "s/IGLO_HIDRAW_MAJOR/${hidraw:-244}/" \
            /opt/gaming/config.toml > /etc/wolf/cfg/config.toml
        chmod 0600 /etc/wolf/cfg/config.toml
    '
    start_wolf
}
update_stack() {
    # The Wolf config keeps its pairings, so only the images and compose file change.
    start_wolf
}
start_wolf() {
    pct exec "$CT_ID" -- bash -euc "
        cd /opt/gaming
        docker compose config --quiet
        docker pull $ESDE_IMAGE
        docker compose up -d --remove-orphans
    "
}
scoped_main
