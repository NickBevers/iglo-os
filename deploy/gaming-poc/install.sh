#!/usr/bin/env bash
# Experimental, opt-in only. Not registered as an Apps installer.
set -euo pipefail
POC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$POC_DIR/../lib.sh"
source "$POC_DIR/../storage.sh"
STORAGE=local-lvm BRIDGE=vmbr0 GAMES_BINDING="" ALLOW_PRIVILEGED=false CREATED_CT=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --storage) STORAGE="$2";shift 2;;
        --games-binding) GAMES_BINDING="$2";shift 2;;
        --allow-privileged) ALLOW_PRIVILEGED=true;shift;;
        *) die "Unknown option: $1";;
    esac
done
require_proxmox
[[ "$ALLOW_PRIVILEGED" == true ]] || die "Read docs/gaming-poc.md, then opt in with --allow-privileged"
[[ "$STORAGE" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$ ]] || die "Invalid container storage"
require_docker_lxc
require_binding "$GAMES_BINDING" games ro
[[ -d "$BINDING_PATH/roms" && -d "$BINDING_PATH/bios" ]] || die "Create roms and bios folders in the Games share first"
[[ -c /dev/dri/renderD128 && -c /dev/uinput && -c /dev/uhid ]] || die "Check Intel render, uinput and uhid devices on the host first"
[[ -f /etc/udev/rules.d/85-wolf.rules ]] || die "Review and install the upstream Wolf input rules first"
! container_exists gaming-poc || die "gaming-poc already exists"
available=$(awk '/^MemAvailable:/ {print int($2/1024)}' /proc/meminfo)
((available >= 10240)) || die "Need 6 GiB for the guest plus 4 GiB host headroom"
download_template
CT_ID=$(pvesh get /cluster/nextid)
[[ "$CT_ID" =~ ^[1-9][0-9]{2,8}$ ]] || die "Invalid new container ID"
cleanup() {
    code=$?
    if ((code!=0)) && [[ -n "$CREATED_CT" ]]; then
        pct stop "$CREATED_CT" >/dev/null 2>&1 || true
        if pct destroy "$CREATED_CT" --purge;then remove_binding_dependencies "$CREATED_CT";fi
    fi
    exit "$code"
}
trap cleanup EXIT
pct create "$CT_ID" "$TEMPLATE_STORAGE:vztmpl/$TEMPLATE" --hostname gaming-poc \
    --unprivileged 0 --features nesting=1 --cores 4 --memory 6144 --swap 0 \
    --rootfs "$STORAGE:96" --net0 "name=eth0,bridge=$BRIDGE,ip=dhcp" --onboot 0 \
    --tags 'iglo;gaming;poc' --description 'Experimental privileged Wolf gaming proof of concept; not a production installer'
CREATED_CT="$CT_ID"
bind_share "$CT_ID" 0 "$GAMES_BINDING" games ro /data/games
pct set "$CT_ID" --dev0 /dev/uinput --dev1 /dev/uhid
cat >> "/etc/pve/lxc/$CT_ID.conf" <<'EOF'
lxc.cgroup2.devices.allow: a
lxc.cap.drop:
lxc.mount.entry: /dev/dri dev/dri none bind,optional,create=dir
lxc.mount.entry: /run/udev mnt/udev none bind,optional,create=dir
lxc.mount.entry: /dev mnt/dev none bind,optional,create=dir
EOF
pct start "$CT_ID";wait_for_network "$CT_ID";install_docker "$CT_ID"
pct exec "$CT_ID" -- bash -euc 'apt-get install -y -q curl vainfo mesa-va-drivers mesa-vulkan-drivers vulkan-tools;install -d -m 0700 /opt/gaming-poc /etc/wolf/cfg;install -d -m 0755 /var/run/wolf'
for file in compose.yaml config.toml diagnose.sh;do pct push "$CT_ID" "$POC_DIR/$file" "/opt/gaming-poc/$file" --perms 0600;done
pct exec "$CT_ID" -- bash -euc '
    sed "s/IGLO_UUID/$(cat /proc/sys/kernel/random/uuid)/" /opt/gaming-poc/config.toml > /etc/wolf/cfg/config.toml
    chmod 0600 /etc/wolf/cfg/config.toml
    cd /opt/gaming-poc
    docker compose config --quiet
    docker pull ghcr.io/games-on-whales/es-de:edge@sha256:b718b0a0782a98c6f4a761bc43b2ac984d732f3e2f647ee4614c4986db6b194d
    docker compose up -d
'
CREATED_CT=""
log "POC created as $CT_ID at $(container_ip "$CT_ID"). Not performance-tested. Pair a PC and run the acceptance checklist before buying a TV client."
