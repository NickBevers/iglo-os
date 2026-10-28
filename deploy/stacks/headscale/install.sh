#!/usr/bin/env bash
set -euo pipefail
STACK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$STACK_DIR/../../lib.sh"
MODE=install CT_ID="" ANSWERS="" CONFIRM="" CREATED_CT="" UPDATE_STARTED=false
HEADSCALE_HOSTNAME="" HEADSCALE_ADDRESS="" HEADSCALE_GATEWAY="" STORAGE="" RATE_MB=""
WORK="$(mktemp -d)"
cleanup() {
    local code=$?
    trap - EXIT
    if ((code != 0)) && [[ -n "$CREATED_CT" ]]; then
        log "Installation failed. Removing only the new controller guest."
        pct stop "$CREATED_CT" >/dev/null 2>&1 || true
        pct destroy "$CREATED_CT" --purge 1 || true
    fi
    [[ -z "$ANSWERS" ]] || rm -f "$ANSWERS"
    rm -rf "$WORK"
    if ((code != 0)) && [[ "$MODE" == update && "$UPDATE_STARTED" == false ]]; then code=3;fi
    exit "$code"
}
trap cleanup EXIT
while (($#)); do
    case "$1" in
        --answers) ANSWERS="$2";shift 2;;
        --ctid) CT_ID="$2";shift 2;;
        --confirm) CONFIRM="$2";shift 2;;
        --update) MODE=update;shift;;
        --remove) MODE=remove;shift;;
        *) die "Unknown controller option";;
    esac
done
load_answers() {
    python3 "$STACK_DIR/validate.py" answers "$1"
    local key value
    while IFS='=' read -r key value; do
        case "$key" in
            HEADSCALE_HOSTNAME|HEADSCALE_ADDRESS|HEADSCALE_GATEWAY|STORAGE|RATE_MB) printf -v "$key" '%s' "$value";;
            PREREQUISITES) :;; # validate.py checked this confirmation
            *) die "Unknown controller answer";;
        esac
    done < "$1"
}
copy_files() {
    pct exec "$CT_ID" -- install -d -m 0700 /opt/headscale
    local file
    for file in "$STACK_DIR"/*; do
        [[ -f "$file" ]] || continue
        [[ "$(basename "$file")" != install.sh ]] || continue
        pct push "$CT_ID" "$file" "/opt/headscale/$(basename "$file")" --perms 0600
    done
}
require_proxmox
command -v python3 >/dev/null || die "Install python3 on the host first"
if [[ "$MODE" != install ]]; then
    require_app_container "$CT_ID" headscale
    if [[ "$MODE" == remove ]]; then
        # A stopped guest can be removed without reading its filesystem.
        pct config "$CT_ID" > "$WORK/config"
        installed_host=$(python3 -c 'import sys; from urllib.parse import unquote; text=unquote(open(sys.argv[1]).read()).replace("\\n", "\n"); print(next((line.removeprefix("iglo-headscale-hostname: ").strip() for line in text.splitlines() if line.startswith("iglo-headscale-hostname: ")), ""))' "$WORK/config")
        [[ -n "$CONFIRM" && "$CONFIRM" == "$installed_host" ]] || die "Type the installed controller hostname to confirm removal"
        remove_app "$CT_ID" headscale
        exit 0
    fi
    require_running "$CT_ID"
    pct pull "$CT_ID" /etc/iglo-headscale.conf "$WORK/answers"
    load_answers "$WORK/answers"
    copy_files
    pct exec "$CT_ID" -- rm -f /run/iglo-headscale-update-result
    UPDATE_STARTED=true
    if ! pct exec "$CT_ID" -- bash -c 'bash /opt/headscale/guest.sh update; code=$?; printf "%s\n" "$code" > /run/iglo-headscale-update-result; exit "$code"'; then
        result=$(pct exec "$CT_ID" -- cat /run/iglo-headscale-update-result 2>/dev/null || true)
        case "$result" in 3|4) exit "$result";; *) exit 1;; esac
    fi
    if ! pct set "$CT_ID" --description "$(app_description "Headscale, installed by Iglo-OS")
iglo-headscale-hostname: $HEADSCALE_HOSTNAME"; then
        log "Controller update passed local health, but its version metadata could not be saved. Inspect the guest configuration."
    fi
    exit 0
fi
[[ -f "$ANSWERS" ]] || die "Use the Apps form with validated controller settings"
load_answers "$ANSWERS"
! container_exists headscale || die "A Headscale container already exists"
# Proxmox storage configuration API checked against pve-storage 9.1.13 source,
# 28 October 2026. Controller databases must stay on unshared local storage.
pvesh get "/storage/$STORAGE" --output-format json > "$WORK/storage.json"
local_path=$(python3 "$STACK_DIR/validate.py" storage "$WORK/storage.json")
if [[ -n "$local_path" ]]; then
    filesystem=$(findmnt --target "$local_path" --noheadings --output FSTYPE)
    case "$filesystem" in ext2|ext3|ext4|xfs|btrfs|zfs) :;; *) die "Controller directory storage must use a local disk filesystem";; esac
fi
ip -j -4 address show dev vmbr0 > "$WORK/bridge.json"
python3 "$STACK_DIR/validate.py" network "$WORK/bridge.json" "$HEADSCALE_ADDRESS"
if command -v arping >/dev/null && arping -V 2>&1 | grep -qi iputils; then
    arping -D -I vmbr0 -c 2 -w 3 "${HEADSCALE_ADDRESS%/*}" >/dev/null 2>&1 || die "The static IP is already in use, or its duplicate check failed"
else
    log "iputils arping is unavailable. Confirm this static IP is unused and outside the DHCP pool."
fi
download_template
pvesh get /cluster/resources --type vm --output-format json > "$WORK/resources.json"
python3 "$STACK_DIR/validate.py" budget "$WORK/resources.json" /proc/meminfo "$(hostname -s)"
CT_ID="$(pvesh get /cluster/nextid)"
[[ "$CT_ID" =~ ^[1-9][0-9]{2,8}$ ]] || die "Invalid new container ID"
pct create "$CT_ID" "$TEMPLATE_STORAGE:vztmpl/$TEMPLATE" --hostname headscale \
    --unprivileged 1 --features nesting=1 --cores 1 --memory 512 --swap 0 \
    --rootfs "$STORAGE:8" --net0 "name=eth0,bridge=vmbr0,ip=$HEADSCALE_ADDRESS,gw=$HEADSCALE_GATEWAY,ip6=manual,rate=$RATE_MB,firewall=1" \
    --onboot 1 --tags "iglo;headscale" \
    --description "$(app_description "Headscale, installed by Iglo-OS")
iglo-headscale-hostname: $HEADSCALE_HOSTNAME"
CREATED_CT="$CT_ID"
pct start "$CT_ID"
wait_for_network "$CT_ID"
pct push "$CT_ID" "$ANSWERS" /etc/iglo-headscale.conf --perms 0600
copy_files
pct exec "$CT_ID" -- bash /opt/headscale/guest.sh install
CREATED_CT=""
echo "IGLO app headscale $CT_ID ${HEADSCALE_ADDRESS%/*}"
