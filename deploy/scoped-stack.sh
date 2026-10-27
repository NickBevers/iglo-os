#!/usr/bin/env bash
# Sourced by a stack's install.sh after APP and STACK_DIR are set.
set -euo pipefail
source "$STACK_DIR/../../lib.sh"
source "$STACK_DIR/../../storage.sh"
STORAGE=local-lvm BRIDGE=vmbr0 CT_ID="" ANSWERS="" MODE=install
LIBRARY_BINDING="" OUTPUT_BINDING="" BACKUP_BINDING="" CORES=2 MEMORY_MIB=2048 DISK_GB=64 PRIVILEGED=false
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
            STORAGE|LIBRARY_BINDING|OUTPUT_BINDING|BACKUP_BINDING|CORES|MEMORY_MIB|DISK_GB|PRIVILEGED) printf -v "$key" '%s' "$value";;
            *) die "Unknown answer: $key";;
        esac
    done < "$1"
}
check_scoped_answers() {
    [[ "$STORAGE" =~ ^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$ ]] || die "Invalid container storage"
    [[ "$CORES" =~ ^[1-4]$ ]] || die "Use 1–4 cores"
    [[ "$DISK_GB" =~ ^[0-9]{1,4}$ ]] && ((DISK_GB >= 32 && DISK_GB <= 2048)) || die "Use a 32–2048 GB local disk"
    [[ "$MEMORY_MIB" =~ ^[0-9]{3,4}$ ]] || die "Invalid memory limit"
    [[ "$PRIVILEGED" == true && "$APP" == gaming || "$PRIVILEGED" == false && "$APP" != gaming ]] || die "Only Gaming runs privileged, after confirmation"
    if [[ "$APP" == photos ]]; then
        ((MEMORY_MIB == 8192)) || die "Photos uses an 8192 MiB container"
        require_binding "$LIBRARY_BINDING" photos rw
        require_binding "$BACKUP_BINDING" backups rw
        [[ "$LIBRARY_BINDING" != "$BACKUP_BINDING" ]] || die "Backups need a separate binding"
    elif [[ "$APP" == music ]]; then
        ((MEMORY_MIB >= 1024 && MEMORY_MIB <= 2048)) || die "Use 1024–2048 MiB"
        require_binding "$LIBRARY_BINDING" music rw
    elif [[ "$APP" == inbox ]]; then
        ((MEMORY_MIB >= 512 && MEMORY_MIB <= 2048)) || die "Use 512–2048 MiB"
        require_binding "$LIBRARY_BINDING" inbox ro
        require_binding "$OUTPUT_BINDING" videos rw
    elif [[ "$APP" == gaming ]]; then
        ((CORES >= 2 && DISK_GB >= 64)) || die "Gaming needs 2–4 cores and at least 64 GB"
        ((MEMORY_MIB >= 4096 && MEMORY_MIB <= 8192)) || die "Use 4096–8192 MiB"
        require_binding "$LIBRARY_BINDING" games ro
        [[ -d "$BINDING_PATH/roms" && -d "$BINDING_PATH/bios" ]] || die "Create roms and bios folders in the Games share first"
        [[ -c /dev/dri/renderD128 && -c /dev/uinput && -c /dev/uhid ]] || die "Check the Intel render, uinput and uhid devices on the host first"
        [[ -f /etc/udev/rules.d/85-wolf.rules ]] || die "Review and install the upstream Wolf input rules on the host first"
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
# render_device gives Photos or Inbox the render node of the host GPU, when
# there is one. A render node only computes and encodes video; it can't show
# anything on a screen. The stack loads compose.gpu.yaml when it sees it.
render_device() {
    [[ -c /dev/dri/renderD128 ]] || return 0
    log "Passing the GPU (/dev/dri/renderD128) to the container"
    pct set "$1" --dev0 /dev/dri/renderD128,mode=0666
}
# gaming_devices gives the Gaming container the GPU and virtual input devices,
# as in the Wolf Proxmox quickstart.
gaming_devices() {
    pct set "$1" --dev0 /dev/uinput --dev1 /dev/uhid
    cat >> "/etc/pve/lxc/$1.conf" <<'CONF'
lxc.cgroup2.devices.allow: a
lxc.cap.drop:
lxc.mount.entry: /dev/dri dev/dri none bind,optional,create=dir
lxc.mount.entry: /run/udev mnt/udev none bind,optional,create=dir
lxc.mount.entry: /dev mnt/dev none bind,optional,create=dir
CONF
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
    [[ "$APP" == music ]] || require_docker_lxc
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
    # Gaming is privileged and only runs while you play, so it doesn't start at boot.
    local unprivileged=1 onboot=1
    if [[ "$APP" == gaming ]]; then unprivileged=0 onboot=0; fi
    pct create "$CT_ID" "$TEMPLATE_STORAGE:vztmpl/$TEMPLATE" --hostname "$APP" \
        --unprivileged "$unprivileged" --features nesting=1 --cores "$CORES" --memory "$MEMORY_MIB" --swap 0 \
        --rootfs "$STORAGE:$DISK_GB" --net0 "name=eth0,bridge=$BRIDGE,ip=dhcp" --onboot "$onboot" \
        --tags "iglo;$APP" --description "$(app_description "$APP, installed by Iglo-OS")"
    CREATED_CT="$CT_ID"
    case "$APP" in
        photos) bind_share "$CT_ID" 0 "$LIBRARY_BINDING" photos rw /data/photos;bind_share "$CT_ID" 1 "$BACKUP_BINDING" backups rw /data/backups;render_device "$CT_ID";;
        music) bind_share "$CT_ID" 0 "$LIBRARY_BINDING" music rw /data/music;;
        inbox) bind_share "$CT_ID" 0 "$LIBRARY_BINDING" inbox ro /data/inbox;bind_share "$CT_ID" 1 "$OUTPUT_BINDING" videos rw /data/videos;render_device "$CT_ID";;
        gaming) bind_share "$CT_ID" 0 "$LIBRARY_BINDING" games ro /data/games;gaming_devices "$CT_ID";;
    esac
    pct start "$CT_ID";wait_for_network "$CT_ID"
    [[ "$APP" == music ]] || install_docker "$CT_ID"
    pct push "$CT_ID" "$ANSWERS" /etc/iglo-stack.conf --perms 0600
    scoped_files
    install_stack
    CREATED_CT=""
    echo "IGLO app $APP $CT_ID $(container_ip "$CT_ID")"
}
