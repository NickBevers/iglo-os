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
    BINDING_PATH="/mnt/iglo/shares/$id" BINDING_READ_ONLY="$read_only"
    # --raw escapes the spaces of a share like "Videos (Umbrel)" as \x20, and
    # printf %b turns them back.
    actual=$(findmnt --mountpoint "$BINDING_PATH" --raw --noheadings --output FSTYPE,SOURCE) || die "The $role share is unavailable"
    read -r fs mounted_source <<< "$actual"
    mounted_source=$(printf '%b' "$mounted_source")
    [[ "$fs" == cifs && "$mounted_source" == "$source" ]] || die "The $role share has the wrong mounted source"
    BINDING_UNIT="mnt-iglo-shares-$id.mount"
}

bind_share() {
    local ctid="$1" index="$2" id="$3" role="$4" access="$5" destination="$6"
    require_binding "$id" "$role" "$access"
    local options="$BINDING_PATH,mp=$destination,backup=0"
    [[ "$access" != ro ]] || options+=",ro=1"
    pct set "$ctid" "--mp$index" "$options"
    # Proxmox starts a container with pve-container@, or pve-container-debug@
    # in debug mode (pve-container 7d99e49, vm_start). The share mounts first in both.
    local unit
    for unit in pve-container pve-container-debug; do
        install -d -m 0755 "/etc/systemd/system/$unit@$ctid.service.d"
        printf '[Unit]\nRequires=%s\nAfter=%s\n' "$BINDING_UNIT" "$BINDING_UNIT" >"/etc/systemd/system/$unit@$ctid.service.d/iglo-$id.conf"
    done
    systemctl daemon-reload
}

# make_share_folders makes the folders that are missing on the share of the
# last require_binding. The mount gives them to the containers. It never
# changes folders that exist.
make_share_folders() {
    local folder
    for folder in "$@"; do
        if [[ ! -d "$BINDING_PATH/$folder" ]]; then
            [[ "$BINDING_READ_ONLY" == false ]] || die "Make the folder $folder on the share first, or connect the share with write access"
            log "Making the folder $folder on the share"
            mkdir -m 0775 "$BINDING_PATH/$folder"
        fi
        keep_library_folder "$BINDING_PATH/$folder"
    done
}

# LIBRARY_PLACEHOLDER is a hidden file in an empty library folder. Jellyfin
# skips a library folder that is empty in its scans ("is inaccessible or
# empty, skipping"), so the folder never gets into its database. When Sonarr
# or Radarr later report the first import, Jellyfin finds no known folder for
# the path and adds nothing until the next full scan, up to 12 hours later.
# Any entry makes the folder count, hidden ones too (Jellyfin v12.4:
# Folder.IsLibraryFolderAccessible, DirectoryService.IsAccessible, and
# ManagedFileSystem with AttributesToSkip 0; checked 7 November 2026). Don't
# name it .ignore: that makes Jellyfin skip the folder.
LIBRARY_PLACEHOLDER=".iglo-library"
LIBRARY_PLACEHOLDER_TEXT="Iglo-OS keeps this file so Jellyfin knows this library while it is empty. You can leave it."

# keep_library_folder puts the placeholder in folder $1 when it is empty and
# the share of the last require_binding has write access. A folder with
# anything in it needs none.
keep_library_folder() {
    [[ "$BINDING_READ_ONLY" == false && -d "$1" ]] || return 0
    [[ -z "$(ls -A "$1")" ]] || return 0
    printf '%s\n' "$LIBRARY_PLACEHOLDER_TEXT" >"$1/$LIBRARY_PLACEHOLDER"
}

# free_mount_index prints the first unused mount point number of a container.
free_mount_index() {
    local index=0
    while pct config "$1" | grep -q "^mp$index:"; do
        index=$((index + 1))
    done
    echo "$index"
}

remove_binding_dependencies() {
    local ctid="$1"
    [[ "$ctid" =~ ^[1-9][0-9]{2,8}$ ]] || die "Invalid container"
    rm -f "/etc/systemd/system/pve-container@$ctid.service.d/iglo-"*.conf \
        "/etc/systemd/system/pve-container-debug@$ctid.service.d/iglo-"*.conf
    systemctl daemon-reload
}
