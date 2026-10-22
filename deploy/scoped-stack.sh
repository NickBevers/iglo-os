#!/usr/bin/env bash
# Sourced by a stack's install.sh after APP and STACK_DIR are set.
set -euo pipefail
source "$STACK_DIR/../../lib.sh"
source "$STACK_DIR/../../storage.sh"
STORAGE=local-lvm BRIDGE=vmbr0 CT_ID="" ANSWERS="" MODE=install
LIBRARY_BINDING="" OUTPUT_BINDING="" BACKUP_BINDING="" CORES=2 MEMORY_MIB=2048 DISK_GB=64
CREATED_CT=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --answers) ANSWERS="$2";shift 2;;
        --ctid) CT_ID="$2";shift 2;;
        --update) MODE=update;shift;;
        --remove) MODE=remove;shift;;
        --backup) MODE=backup;shift;;
        --verify) MODE=verify;shift;;
        *) die "Unknown option: $1";;
    esac
done

load_scoped_answers() {
    local key value
    while IFS='=' read -r key value || [[ -n "$key" ]]; do
        case "$key" in
            STORAGE|LIBRARY_BINDING|OUTPUT_BINDING|BACKUP_BINDING|CORES|MEMORY_MIB|DISK_GB) printf -v "$key" '%s' "$value";;
            *) die "Unknown answer: $key";;
        esac
    done < "$1"
}
check_scoped_answers() {
    [[ "$STORAGE" =~ ^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$ ]] || die "Invalid container storage"
    [[ "$CORES" =~ ^[1-4]$ ]] || die "Use 1–4 cores"
    [[ "$DISK_GB" =~ ^[0-9]{1,4}$ ]] && ((DISK_GB >= 32 && DISK_GB <= 2048)) || die "Use a 32–2048 GB local disk"
    [[ "$MEMORY_MIB" =~ ^[0-9]{3,4}$ ]] || die "Invalid memory limit"
    if [[ "$APP" == photos ]]; then
        ((MEMORY_MIB == 8192)) || die "Photos uses an 8192 MiB container"
        require_binding "$LIBRARY_BINDING" photos rw
        require_binding "$BACKUP_BINDING" backups rw
        [[ "$LIBRARY_BINDING" != "$BACKUP_BINDING" ]] || die "Backups need a separate binding"
    elif [[ "$APP" == music ]]; then
        ((MEMORY_MIB >= 512 && MEMORY_MIB <= 2048)) || die "Use 512–2048 MiB"
        require_binding "$LIBRARY_BINDING" music rw
    elif [[ "$APP" == inbox ]]; then
        ((MEMORY_MIB >= 512 && MEMORY_MIB <= 2048)) || die "Use 512–2048 MiB"
        require_binding "$LIBRARY_BINDING" inbox ro
        require_binding "$OUTPUT_BINDING" videos rw
    else die "Unknown scoped stack"; fi
}
scoped_cleanup() {
    local code=$?
    if ((code != 0)) && [[ -n "$CREATED_CT" ]]; then
        log "Installation failed; removing the new container, keeping all NAS files"
        pct stop "$CREATED_CT" >/dev/null 2>&1 || true
        if pct destroy "$CREATED_CT" --purge; then remove_binding_dependencies "$CREATED_CT"; fi
    fi
    [[ -z "$ANSWERS" ]] || rm -f "$ANSWERS"
    exit "$code"
}
scoped_files() {
    pct exec "$CT_ID" -- install -d -m 0700 "/opt/$APP"
    for file in "$STACK_DIR"/*; do
        [[ "$(basename "$file")" != install.sh ]] || continue
        pct push "$CT_ID" "$file" "/opt/$APP/$(basename "$file")" --perms 0600
    done
}
scoped_main() {
    require_proxmox
    if [[ "$MODE" == backup || "$MODE" == verify ]]; then
        [[ "$APP" == photos ]] || die "Recovery is only supported for Photos"
        require_app_container "$CT_ID" photos;require_running "$CT_ID"
        pct exec "$CT_ID" -- bash /opt/photos/recovery.sh "$MODE"
        return
    fi
    if [[ "$MODE" == remove ]]; then
        require_app_container "$CT_ID" "$APP"
        remove_app "$CT_ID" "$APP"
        remove_binding_dependencies "$CT_ID"
        return
    fi
    require_docker_lxc
    if [[ "$MODE" == update ]]; then
        require_app_container "$CT_ID" "$APP";require_running "$CT_ID"
        local saved
        saved=$(mktemp)
        pct pull "$CT_ID" /etc/iglo-stack.conf "$saved"
        load_scoped_answers "$saved";rm -f "$saved"
        check_scoped_answers
        # Full matched backup before upgrading Photos, not just a VM snapshot.
        if [[ "$APP" == photos ]]; then pct exec "$CT_ID" -- bash /opt/photos/recovery.sh backup; fi
        scoped_files
        update_stack
        pct set "$CT_ID" --description "$(app_description "$APP, installed by Iglo-OS")"
        return
    fi
    [[ -f "$ANSWERS" ]] || die "Use the Apps page or --answers with validated settings"
    load_scoped_answers "$ANSWERS"
    check_scoped_answers
    ! container_exists "$APP" || die "A container named $APP already exists"
    local available
    available=$(awk '/^MemAvailable:/ {print int($2/1024)}' /proc/meminfo)
    ((available >= MEMORY_MIB + 4096)) || die "Not enough available RAM to keep 4 GiB of host headroom"
    download_template
    CT_ID="$(pvesh get /cluster/nextid)"
    [[ "$CT_ID" =~ ^[1-9][0-9]{2,8}$ ]] || die "Invalid new container ID"
    trap scoped_cleanup EXIT
    pct create "$CT_ID" "$TEMPLATE_STORAGE:vztmpl/$TEMPLATE" --hostname "$APP" \
        --unprivileged 1 --features nesting=1 --cores "$CORES" --memory "$MEMORY_MIB" --swap 0 \
        --rootfs "$STORAGE:$DISK_GB" --net0 "name=eth0,bridge=$BRIDGE,ip=dhcp" --onboot 1 \
        --tags "iglo;$APP" --description "$(app_description "$APP, installed by Iglo-OS")"
    CREATED_CT="$CT_ID"
    case "$APP" in
        photos) bind_share "$CT_ID" 0 "$LIBRARY_BINDING" photos rw /data/photos;bind_share "$CT_ID" 1 "$BACKUP_BINDING" backups rw /data/backups;;
        music) bind_share "$CT_ID" 0 "$LIBRARY_BINDING" music rw /data/music;;
        inbox) bind_share "$CT_ID" 0 "$LIBRARY_BINDING" inbox ro /data/inbox;bind_share "$CT_ID" 1 "$OUTPUT_BINDING" videos rw /data/videos;;
    esac
    pct start "$CT_ID";wait_for_network "$CT_ID";install_docker "$CT_ID"
    pct push "$CT_ID" "$ANSWERS" /etc/iglo-stack.conf --perms 0600
    scoped_files
    install_stack
    CREATED_CT=""
    echo "IGLO app $APP $CT_ID $(container_ip "$CT_ID")"
}
