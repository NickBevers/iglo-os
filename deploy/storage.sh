#!/usr/bin/env bash
# Shared by the scoped-stack installers. Never source user-provided files.
require_binding() {
    local id="$1" role="$2" access="$3" key value actual
    [[ "$id" =~ ^[a-z][a-z0-9]{0,31}$ ]] || die "Invalid share binding ID"
    local found_role="" read_only="" source=""
    [[ -f "/etc/iglo/storage/$id.conf" ]] || die "Connect the $role share in Storage first"
    while IFS='=' read -r key value; do
        case "$key" in ROLE) found_role="$value";; READ_ONLY) read_only="$value";; SOURCE) source="$value";; esac
    done < "/etc/iglo/storage/$id.conf"
    [[ "$found_role" == "$role" ]] || die "The selected share must have the $role role"
    [[ "$access" == ro || "$read_only" == false ]] || die "The $role share needs write access"
    BINDING_PATH="/mnt/iglo/shares/$id"
    actual=$(findmnt --mountpoint "$BINDING_PATH" --noheadings --output FSTYPE,SOURCE) || die "The $role share is unavailable"
    read -r fs mounted_source <<< "$actual"
    [[ "$fs" == cifs && "$mounted_source" == "$source" ]] || die "The $role share has the wrong mounted source"
    BINDING_UNIT="mnt-iglo-shares-$id.mount"
}

bind_share() {
    local ctid="$1" index="$2" id="$3" role="$4" access="$5" destination="$6"
    require_binding "$id" "$role" "$access"
    local options="$BINDING_PATH,mp=$destination,backup=0"
    [[ "$access" != ro ]] || options+=",ro=1"
    pct set "$ctid" "--mp$index" "$options"
    install -d -m 0755 "/etc/systemd/system/pve-container@$ctid.service.d"
    printf '[Unit]\nRequires=%s\nAfter=%s\n' "$BINDING_UNIT" "$BINDING_UNIT" > "/etc/systemd/system/pve-container@$ctid.service.d/iglo-$id.conf"
    systemctl daemon-reload
}

remove_binding_dependencies() {
    local ctid="$1"
    [[ "$ctid" =~ ^[1-9][0-9]{2,8}$ ]] || die "Invalid container"
    rm -f "/etc/systemd/system/pve-container@$ctid.service.d/iglo-"*.conf
    systemctl daemon-reload
}
