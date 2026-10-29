# shellcheck shell=bash
# The Photos backups. scoped-stack.sh sources this on the Proxmox host for
# install.sh --backup and --verify, and before an update. It needs lib.sh,
# storage.sh and secrets.sh.
#
# restic keeps the Immich database dump and the originals together in one
# snapshot, in a repository on the Backups share. When a disk of the host is
# set, a second repository there holds the copy off the NAS. restic runs on
# the host, so the Photos container keeps running and never sees the password.
# Immich v3.3.5 allows a backup while the server runs when the database comes
# first and the files second (docs/docs/administration/backup-and-restore.md,
# "Backup ordering", checked 29 October 2026).

# restic is BSD-2-Clause. The digest is of restic_0.19.1_linux_amd64.bz2 in the
# SHA256SUMS of the v0.19.1 release, checked 29 October 2026.
RESTIC_VERSION=0.19.1
RESTIC_SHA256=f415415624dcc452f2a02b8c33641791a8c6d6d3b65bbb3543fcf9a25151585c
RESTIC="${IGLO_RESTIC:-/usr/local/lib/iglo/bin/restic-$RESTIC_VERSION}"
PHOTOS_BACKUP_CONF="${IGLO_PHOTOS_BACKUP_CONF:-/etc/iglo/photos-backup.conf}"
PHOTOS_BACKUP_DIR="${IGLO_PHOTOS_BACKUP_DIR:-/var/lib/iglo/photos-backup}"
PHOTOS_BACKUP_STATE="$PHOTOS_BACKUP_DIR/state"
PHOTOS_DUMP_DIR="$PHOTOS_BACKUP_DIR/database"
# One backup or check at a time on this host, also by hand and before an update.
PHOTOS_LOCK="${IGLO_PHOTOS_LOCK:-/run/lock/iglo-photos-backup.lock}"
PHOTOS_PASSWORD=photos_restic_password
# The agent writes this file with the ID of the backup disk when the folder off
# the NAS is saved. Without it, the disk is missing: an empty mount point never
# gets a repository.
OFFSITE_MARKER=.iglo-photos-disk
# Every snapshot gets this host name. A backup is tagged pending until restic
# finished it without errors, and only snapshots tagged iglo-photos count for
# the retention and Verify recovery.
TAG=iglo-photos PENDING_TAG=iglo-photos-pending
export RESTIC_HOST=iglo-photos RESTIC_CACHE_DIR="${RESTIC_CACHE_DIR:-/var/cache/iglo/restic}"

# The settings, as the agent writes them. Missing ones keep these defaults.
BACKUP_TIME=01:00 OFFSITE_PATH="" OFFSITE_ID="" KEEP_DAILY=7 KEEP_WEEKLY=4 KEEP_MONTHLY=12
PHOTO_ROOT="" BACKUP_SHARE="" NAS_REPO="" STEP="" PHOTOS_STAGE=""
OFFSITE_PROBLEM="" OFFSITE_MOUNT="" SOURCE_MOUNT="" NAS_MOUNT="" SOURCE_LOST="" NAS_LOST="" VERIFY_DETAIL=""
# Immich v3.3.5 writes .immich into each of its folders when the server starts
# (server/src/services/storage.service.ts and StorageFolder in enum.ts, checked
# 29 October 2026). thumbs and encoded-video are on the SSD of the container
# (compose.override.yaml), so these four are on the Photos share.
IMMICH_FOLDERS="library upload profile backups"
REPO_SNAPSHOT="" REPO_BYTES="" REPO_COUNT=""
declare -A PHOTOS_STATE=()
PHOTOS_STATE_KEYS="BACKUP_STARTED BACKUP_FINISHED BACKUP_RESULT BACKUP_MESSAGE SNAPSHOT NAS_BYTES NAS_SNAPSHOTS OFFSITE_RESULT OFFSITE_MESSAGE OFFSITE_FINISHED OFFSITE_BYTES OFFSITE_SNAPSHOTS OFFSITE_DISK VERIFY_FINISHED VERIFY_RESULT VERIFY_MESSAGE VERIFY_SNAPSHOT"

load_backup_settings() {
    local key value
    if [[ -f "$PHOTOS_BACKUP_CONF" ]]; then
        while IFS='=' read -r key value || [[ -n "$key" ]]; do
            case "$key" in
                BACKUP_TIME|OFFSITE_PATH|OFFSITE_ID|KEEP_DAILY|KEEP_WEEKLY|KEEP_MONTHLY) printf -v "$key" '%s' "$value";;
                "") ;;
                *) die "Unknown Photos backup setting: $key";;
            esac
        done < "$PHOTOS_BACKUP_CONF"
    fi
    check_backup_settings
}

# check_backup_settings checks the settings again: the agent checked them
# when it saved them.
check_backup_settings() {
    local keep
    [[ "$BACKUP_TIME" =~ ^([01][0-9]|2[0-3]):[0-5][0-9]$ ]] || die "Invalid Photos backup time"
    for keep in "$KEEP_DAILY" "$KEEP_WEEKLY" "$KEEP_MONTHLY"; do
        [[ "$keep" =~ ^[0-9]{1,3}$ ]] && ((10#$keep <= 500)) || die "Keep 0 to 500 Photos backups of each kind"
    done
    ((10#$KEEP_DAILY + 10#$KEEP_WEEKLY + 10#$KEEP_MONTHLY > 0)) || die "Keep at least one Photos backup"
    [[ -z "$OFFSITE_PATH" ]] || offsite_path_valid "$OFFSITE_PATH" || die "Invalid folder for the copy off the NAS"
    [[ -z "$OFFSITE_ID" || "$OFFSITE_ID" =~ ^[a-f0-9]{32}$ ]] || die "Invalid ID of the backup disk"
}

# offsite_path_valid checks the form of a folder: absolute, plain characters,
# no . or .. parts, and not the root or the NAS shares of Iglo-OS.
offsite_path_valid() {
    local path="$1" part parts
    [[ "$path" =~ ^/[A-Za-z0-9._/-]{1,200}$ && "$path" != */ && "$path" != *//* ]] || return 1
    IFS=/ read -r -a parts <<< "${path#/}"
    for part in "${parts[@]}"; do
        [[ "$part" != . && "$part" != .. ]] || return 1
    done
    [[ "$path" != /mnt/iglo && "$path" != /mnt/iglo/* ]]
}

# mount_disks prints the physical disks under the file system SOURCE of type
# FSTYPE. For ZFS, those are the disks of the vdevs of its pool: zpool list -v
# -H -P -L prints the real path of each vdev (zpool-list.8 of OpenZFS 2.4.4,
# the zfs-linux 2.4.4-pve1 of Proxmox VE 9, checked 29 October 2026).
mount_disks() {
    local source="${1%%\[*}" fstype="$2" device
    if [[ "$fstype" == zfs ]]; then
        zpool list -v -H -P -L -o name "${source%%/*}" | awk '{for (i = 1; i <= NF; i++) if ($i ~ /^\/dev\//) print $i}' |
            while read -r device; do
                lsblk --list --noheadings --inverse --paths --output NAME,TYPE "$device"
            done | awk '$2 == "disk" {print $1}' | sort -u
        return
    fi
    lsblk --list --noheadings --inverse --paths --output NAME,TYPE "$source" | awk '$2 == "disk" {print $1}' | sort -u
}

# mount_of prints the mounted file system that holds $1: its target, type and source.
mount_of() {
    findmnt --raw --noheadings --output TARGET,FSTYPE,SOURCE --target "$1" | head -n 1
}

# real_folder says whether $1 is a folder, with no link anywhere in its path.
real_folder() {
    [[ -d "$1" && ! -L "$1" && "$(realpath -e "$1" 2>/dev/null)" == "$1" ]]
}

# offsite_check sets OFFSITE_PROBLEM to why folder $1 can't hold the copy off
# the NAS, or to nothing, and OFFSITE_MOUNT to its file system. It must be a
# folder on a mounted local disk that is not a disk of the system and not a
# network share like the NAS. Its repository folder iglo-photos, when there is
# one, must be a real folder on that same file system. The agent checks the
# same when the folder is saved, and this checks again before every use.
offsite_check() {
    local path="$1" repo="$1/iglo-photos" target fstype source root_target root_fstype root_source disks root_disks
    OFFSITE_PROBLEM="" OFFSITE_MOUNT=""
    problem() { OFFSITE_PROBLEM="$1"; }
    offsite_path_valid "$path" || { problem "use a folder like /mnt/backup-disk";return 1; }
    real_folder "$path" || { problem "the folder does not exist";return 1; }
    read -r target fstype source < <(mount_of "$path") || true
    read -r root_target root_fstype root_source < <(mount_of /) || true
    [[ -n "$target" && "$root_target" == / ]] || { problem "its disk is not mounted";return 1; }
    case "$fstype" in
        cifs|smb3|smbfs|nfs|nfs4|fuse.sshfs|9p) problem "it is on a network share, like the NAS";return 1;;
        ext4|ext3|xfs|btrfs|zfs|exfat|vfat|ntfs3|f2fs) ;;
        *) problem "its disk is not mounted, or it has the file system $fstype";return 1;;
    esac
    [[ "$target" != / ]] || { problem "it is on the root disk, not a disk of its own";return 1; }
    disks=$(mount_disks "$source" "$fstype") || true
    root_disks=$(mount_disks "$root_source" "$root_fstype") || true
    [[ -n "$disks" ]] || { problem "could not find the disk of $source";return 1; }
    [[ -n "$root_disks" ]] || { problem "could not find the disk of the system";return 1; }
    if grep -qxF -f <(printf '%s\n' "$root_disks") <<< "$disks"; then
        problem "it is on the same disk as the system"
        return 1
    fi
    OFFSITE_MOUNT="$target $fstype $source"
    if [[ -e "$repo" || -L "$repo" ]]; then
        real_folder "$repo" || { problem "its iglo-photos folder is a link or not a folder";return 1; }
        [[ "$(mount_of "$repo")" == "$OFFSITE_MOUNT" ]] || { problem "its iglo-photos folder is on another file system";return 1; }
    fi
    if [[ -n "$BACKUP_SHARE" ]]; then
        case "$(realpath -m "$repo")/" in "$(realpath -m "$BACKUP_SHARE")"/*) problem "it is on the Backups share";return 1;; esac
    fi
}

# The guards check, right before each restic command in its own mount
# namespace and again right after it outside, that the mounts are still the
# ones that were checked at the start. They run in a child bash too, so they
# set no state there; the check outside sets SOURCE_LOST, NAS_LOST or
# OFFSITE_PROBLEM.

# offsite_guard checks the disk off the NAS: the same file system, with the
# marker of Iglo-OS in it.
offsite_guard() {
    local marker="$OFFSITE_PATH/$OFFSITE_MARKER"
    if [[ -n "$OFFSITE_ID" && -f "$marker" && ! -L "$marker" && "$(head -c 64 "$marker")" == "$OFFSITE_ID" && "$(mount_of "$OFFSITE_PATH")" == "$OFFSITE_MOUNT" ]]; then
        return 0
    fi
    OFFSITE_PROBLEM="the backup disk is not there, or not the disk saved in Backup settings"
    echo "error: $OFFSITE_PROBLEM" >&2
    return 1
}

# nas_guard checks that the Backups share is still the mounted share.
nas_guard() {
    [[ -n "$NAS_MOUNT" && "$(mount_of "$BACKUP_SHARE")" == "$NAS_MOUNT" ]] && return 0
    NAS_LOST=1
    echo "error: the Backups share is not mounted anymore" >&2
    return 1
}

# source_guard checks that the Photos share is still the mounted share, with
# the folder markers of Immich in it. An empty mount point has none.
source_guard() {
    local folder
    if [[ -n "$SOURCE_MOUNT" && "$(mount_of "$PHOTO_ROOT")" == "$SOURCE_MOUNT" ]]; then
        for folder in $IMMICH_FOLDERS; do
            [[ -f "$PHOTO_ROOT/$folder/.immich" ]] || { SOURCE_LOST=1;echo "error: $PHOTO_ROOT/$folder/.immich is missing" >&2;return 1; }
        done
        return 0
    fi
    SOURCE_LOST=1
    echo "error: the Photos share is not mounted anymore" >&2
    return 1
}

nas_source_guard() { nas_guard && source_guard; }
offsite_source_guard() { offsite_guard && source_guard; }

# photos_paths finds the shares of the Photos container on the host.
photos_paths() {
    if [[ -z "$LIBRARY_BINDING" ]]; then
        local saved
        saved=$(mktemp)
        pct pull "$CT_ID" /etc/iglo-stack.conf "$saved"
        load_scoped_answers "$saved";rm -f "$saved"
    fi
    require_binding "$LIBRARY_BINDING" photos ro
    PHOTO_ROOT="$BINDING_PATH"
    require_binding "$BACKUP_BINDING" backups rw
    BACKUP_SHARE="$BINDING_PATH"
    NAS_REPO="$BINDING_PATH/immich-restic"
    [[ "$LIBRARY_BINDING" != "$BACKUP_BINDING" ]] || die "Backups need a separate binding"
    # require_binding checked that both are the SMB shares of their bindings.
    # The guards compare against these.
    SOURCE_MOUNT=$(mount_of "$PHOTO_ROOT")
    NAS_MOUNT=$(mount_of "$BACKUP_SHARE")
    [[ "$SOURCE_MOUNT" == "$PHOTO_ROOT "* && "$NAS_MOUNT" == "$BACKUP_SHARE "* ]] || die "The Photos or Backups share is not mounted"
}

# ensure_restic installs the pinned restic once, after checking its digest.
ensure_restic() {
    [[ ! -x "$RESTIC" ]] || return 0
    log "Downloading restic $RESTIC_VERSION"
    command -v bunzip2 >/dev/null || { apt-get update -q && apt-get install -y -q bzip2; }
    local work
    work=$(mktemp -d)
    curl --fail --silent --show-error --location --proto '=https' --retry 3 --connect-timeout 15 --max-time 300 \
        "https://github.com/restic/restic/releases/download/v$RESTIC_VERSION/restic_${RESTIC_VERSION}_linux_amd64.bz2" -o "$work/restic.bz2" ||
        { rm -rf "$work";die "Could not download restic"; }
    printf '%s  %s\n' "$RESTIC_SHA256" "$work/restic.bz2" | sha256sum --check --status ||
        { rm -rf "$work";die "The restic download has the wrong checksum"; }
    bunzip2 "$work/restic.bz2"
    install -d -m 0755 "$(dirname "$RESTIC")"
    install -m 0755 "$work/restic" "$RESTIC"
    rm -rf "$work"
}

# The guards run in a child bash inside the mount namespace.
export -f mount_of offsite_guard nas_guard source_guard nas_source_guard offsite_source_guard
export RESTIC IMMICH_FOLDERS OFFSITE_MARKER

# restic_in runs restic with the password on stdin, which restic reads when
# stdin is not a terminal (internal/ui/termstatus/status.go of v0.19.1). It
# runs in a private mount namespace (unshare of util-linux), after guard $1
# passed in it. There the mounts can't change anymore: when a share or the
# disk goes away on the host, restic still sees the one it started with, and
# never an empty mount point. restic's local backend makes missing folders
# itself (internal/backend/local/local.go of v0.19.1), so on an empty mount
# point it would write to the root disk. A failed guard exits with 97.
restic_in() {
    local guard="$1"
    shift
    export OFFSITE_PATH OFFSITE_ID OFFSITE_MOUNT SOURCE_MOUNT NAS_MOUNT PHOTO_ROOT BACKUP_SHARE
    "$PASSWORD_SOURCE" |
        unshare --mount --propagation private bash -c '"$0" || exit 97; exec nice -n 10 "$RESTIC" "$@"' "$guard" "$@"
}

# run_guarded runs restic_in with guard $1, then the guard again outside the
# namespace: a mount that went away during the command fails it with 97.
run_guarded() {
    local guard="$1" code=0
    shift
    restic_in "$guard" "$@" || code=$?
    "$guard" || return 97
    return "$code"
}

state_load() {
    local key value
    [[ -f "$PHOTOS_BACKUP_STATE" ]] || return 0
    while IFS='=' read -r key value; do
        [[ " $PHOTOS_STATE_KEYS " != *" $key "* ]] || PHOTOS_STATE[$key]="$value"
    done < "$PHOTOS_BACKUP_STATE"
}

# state_set keeps one line of plain text, so the agent can show it.
state_set() {
    local value="${2//[^A-Za-z0-9 ._:\/()+,-]/}"
    PHOTOS_STATE[$1]="${value:0:240}"
}

# state_save writes the state for the agent. The state has no secrets.
state_save() {
    local key
    install -d -m 0700 "$PHOTOS_BACKUP_DIR"
    for key in $PHOTOS_STATE_KEYS; do
        [[ -z "${PHOTOS_STATE[$key]:-}" ]] || printf '%s=%s\n' "$key" "${PHOTOS_STATE[$key]}"
    done > "$PHOTOS_BACKUP_STATE.new"
    mv -f "$PHOTOS_BACKUP_STATE.new" "$PHOTOS_BACKUP_STATE"
}

now_utc() { date -u +%Y-%m-%dT%H:%M:%SZ; }

# The password of the repositories is the one you typed when you installed
# Photos, and keep in your password manager. The host seals it (secrets.sh),
# and restic gets it on stdin from one of these.
NEW_PASSWORD=""
PASSWORD_SOURCE=sealed_password
sealed_password() { host_secret_get "$PHOTOS_PASSWORD"; }
typed_password() { printf '%s\n' "$NEW_PASSWORD"; }

require_password() {
    host_secret_exists "$PHOTOS_PASSWORD" || die "Photos has no backup password. Set one with iglo-photos-key set on the host."
}

# backup_password_valid is ValidBackupPassword of the agent: 12 to 128
# characters, on one line. printf is a builtin, so the password is in no
# command line.
backup_password_valid() {
    local bytes
    bytes=$(printf '%s' "$1" | wc -c)
    ((${#1} >= 12 && bytes <= 128)) && [[ "$1" != *[[:cntrl:]]* ]]
}

# opens_repository says whether NEW_PASSWORD opens repository $1, with guard
# $2. restic cat config needs the password (exit code 12 when it is wrong).
opens_repository() {
    local code=0
    PASSWORD_SOURCE=typed_password
    run_guarded "$2" --repo "$1" cat config >/dev/null 2>&1 || code=$?
    PASSWORD_SOURCE=sealed_password
    return "$code"
}

# check_new_password checks NEW_PASSWORD: its form, and that it opens the
# Photos repositories that exist, on the Backups share $1 and off the NAS. It
# changes nothing, so a wrong password stops before anything is sealed.
check_new_password() {
    local share="$1" repo
    backup_password_valid "$NEW_PASSWORD" || die "The backup password must be 12 to 128 characters, on one line"
    ensure_restic
    repo="$share/immich-restic"
    if [[ -f "$repo/config" ]]; then
        BACKUP_SHARE="$share" NAS_MOUNT=$(mount_of "$share")
        log "Checking that the backup password opens the Photos backups in $repo"
        opens_repository "$repo" nas_guard || die "The backup password does not open the Photos backups in $repo. Use the password of those backups"
    fi
    load_backup_settings
    if [[ -n "$OFFSITE_PATH" ]]; then
        if offsite_check "$OFFSITE_PATH" && offsite_guard 2>/dev/null; then
            if [[ -f "$OFFSITE_PATH/iglo-photos/config" ]]; then
                log "Checking that the backup password opens the copy off the NAS"
                opens_repository "$OFFSITE_PATH/iglo-photos" offsite_guard || die "The backup password does not open the copy off the NAS in $OFFSITE_PATH. Use the password of those backups"
            fi
        else
            log "The copy off the NAS is not there, so the password is not checked against it"
        fi
    fi
}

# seal_new_password seals NEW_PASSWORD on the host, on stdin, and forgets it.
seal_new_password() {
    printf '%s' "$NEW_PASSWORD" | host_secret_put "$PHOTOS_PASSWORD" || die "Could not seal the backup password"
    NEW_PASSWORD=""
    log "The backup password is sealed on this host"
}

# photos_lock takes the lock of the Photos backups, or stops. It holds until
# the script ends, so a second backup, a check or the backup of an update
# never runs at the same time.
photos_lock() {
    exec 8>"$PHOTOS_LOCK"
    flock -n 8 || die "Another Photos backup or check runs on this host. Try again when it is done"
}

# repo_ready makes repository $1 when it doesn't exist, after guard $2 passed
# in the namespace of restic init, which makes the folder. It refuses a folder
# with other files in it.
repo_ready() {
    [[ ! -f "$1/config" ]] || return 0
    if [[ -d "$1" && -n "$(ls -A "$1")" ]]; then
        echo "error: $1 has files that are not a restic repository" >&2
        return 1
    fi
    "$2" || return 1
    log "Making the restic repository $1"
    run_guarded "$2" --repo "$1" init
}

# push_recovery gives the container the recovery.sh of this release. A
# container of an older release may not have its commands yet.
push_recovery() {
    pct push "$CT_ID" "$STACK_DIR/recovery.sh" /opt/photos/recovery.sh --perms 0600
}

# dump_database writes the database dump and what verify needs next to it.
# Immich keeps running.
dump_database() {
    local info key value tables="" server="" postgres=""
    STEP="dumping the database"
    log "Dumping the Immich database"
    push_recovery
    # The dump goes to a folder of this run only, and moves to the fixed path
    # that the snapshots keep once it is complete.
    install -d -m 0700 "$PHOTOS_BACKUP_DIR"
    PHOTOS_STAGE=$(mktemp -d "$PHOTOS_BACKUP_DIR/.stage.XXXXXXXX")
    info=$(pct exec "$CT_ID" -- bash /opt/photos/recovery.sh info)
    while IFS='=' read -r key value; do
        case "$key" in TABLES) tables="$value";; SERVER_IMAGE) server="$value";; POSTGRES_IMAGE) postgres="$value";; esac
    done < <(printf '%s\n' "$info")
    [[ "$tables" =~ ^[1-9][0-9]*$ ]] || die "The database reported no tables"
    [[ "$postgres" =~ ^ghcr.io/immich-app/postgres:[A-Za-z0-9._-]+@sha256:[a-f0-9]{64}$ ]] || die "Unsupported database image"
    [[ "$server" =~ ^ghcr.io/immich-app/immich-server:[A-Za-z0-9._@:-]+$ ]] || die "Unsupported Immich image"
    pct exec "$CT_ID" -- bash /opt/photos/recovery.sh dump > "$PHOTOS_STAGE/database.sql"
    # pg_dump ends a plain dump with this line, so a cut dump is refused.
    tail -n 5 "$PHOTOS_STAGE/database.sql" | grep -q '^-- PostgreSQL database dump complete' || die "The database dump is incomplete"
    printf '%s\n' "$tables" > "$PHOTOS_STAGE/tables.txt"
    printf '%s\n' "$server" > "$PHOTOS_STAGE/version.txt"
    printf '%s\n' "$postgres" > "$PHOTOS_STAGE/postgres.txt"
    rm -rf "$PHOTOS_DUMP_DIR"
    mv "$PHOTOS_STAGE" "$PHOTOS_DUMP_DIR"
    PHOTOS_STAGE=""
}

# forget_stale_pending forgets the pending snapshots of repository $1, with
# guard $2, that are older than a day: backups that never finished. Their data
# goes with the next prune.
forget_stale_pending() {
    local output line when="" id cutoff stale=() out="$PHOTOS_BACKUP_DIR/.restic-output"
    cutoff=$(($(date +%s) - 86400))
    run_guarded "$2" --repo "$1" snapshots --json --tag "$PENDING_TAG" > "$out" || return 1
    output=$(cat "$out")
    # Each snapshot has its time before its ID (internal/data/snapshot.go and
    # cmd/restic/cmd_snapshots.go of v0.19.1).
    while IFS= read -r line; do
        case "$line" in
            '"time":"'*) when=$(date -d "$(cut -d'"' -f4 <<< "$line")" +%s 2>/dev/null) || when="";;
            '"id":"'*)
                id=$(cut -d'"' -f4 <<< "$line")
                [[ -z "$when" ]] || ((when >= cutoff)) || stale+=("$id")
                when="";;
        esac
    done < <(grep -o '"time":"[^"]*"\|"id":"[a-f0-9]\{64\}"' <<< "$output")
    ((${#stale[@]} == 0)) && return 0
    log "Forgetting ${#stale[@]} unfinished snapshots older than a day"
    run_guarded "$2" --repo "$1" forget "${stale[@]}"
}

# backup_to backs up the dump and the originals into repository $1, forgets
# old snapshots with the retention, and prunes. Guard $2 checks the repository
# and guard $3 the repository and the Photos share, before and after each
# restic command. It sets REPO_SNAPSHOT, REPO_BYTES and REPO_COUNT. It returns
# instead of exiting, so a failed copy off the NAS leaves the backup on the
# NAS. Output goes through a file: a command substitution would lose what the
# guards found.
backup_to() {
    local repo="$1" guard="$2" both="$3" code pending out="$PHOTOS_BACKUP_DIR/.restic-output"
    REPO_SNAPSHOT="" REPO_BYTES="" REPO_COUNT=""
    repo_ready "$repo" "$guard" || return 1
    # Derivatives are rebuilt after a restore, and Immich's own dumps are in
    # the snapshot already. SMB gives no stable inode numbers, so restic
    # compares sizes and times only.
    run_guarded "$both" --repo "$repo" backup --json --quiet --tag "$PENDING_TAG" --ignore-inode \
        --exclude "$PHOTO_ROOT/thumbs" --exclude "$PHOTO_ROOT/encoded-video" --exclude "$PHOTO_ROOT/backups" \
        "$PHOTOS_DUMP_DIR" "$PHOTO_ROOT" > "$out" && code=0 || code=$?
    # The check of the Photos share after the backup decides: an empty mount
    # point backs up fine, so its snapshot must never count.
    if ((code == 97)); then
        echo "error: a share or the disk went away, so the snapshot stays pending" >&2
        return 1
    fi
    if ((code == 3)); then
        echo "error: some originals could not be read, so the snapshot is incomplete and stays pending" >&2
        return 1
    fi
    ((code == 0)) || return 1
    pending=$(grep -o '"snapshot_id":"[a-f0-9]\{64\}"' "$out" | tail -n 1 | cut -d'"' -f4)
    [[ -n "$pending" ]] || { echo "error: restic made no snapshot" >&2;return 1; }
    # restic tag saves the snapshot again with the new tags, under a new ID
    # (cmd/restic/cmd_tag.go of v0.19.1).
    run_guarded "$guard" --repo "$repo" tag --json --set "$TAG" "$pending" > "$out" || return 1
    REPO_SNAPSHOT=$(grep -o '"new_snapshot_id":"[a-f0-9]\{64\}"' "$out" | tail -n 1 | cut -d'"' -f4 | cut -c1-8)
    [[ -n "$REPO_SNAPSHOT" ]] || { echo "error: restic did not mark snapshot ${pending:0:8} as finished" >&2;return 1; }
    forget_stale_pending "$repo" "$guard" || return 1
    log "Snapshot $REPO_SNAPSHOT is saved. Forgetting old snapshots: $KEEP_DAILY daily, $KEEP_WEEKLY weekly, $KEEP_MONTHLY monthly"
    run_guarded "$guard" --repo "$repo" forget --tag "$TAG" --keep-daily "$((10#$KEEP_DAILY))" \
        --keep-weekly "$((10#$KEEP_WEEKLY))" --keep-monthly "$((10#$KEEP_MONTHLY))" --prune || return 1
    run_guarded "$guard" --repo "$repo" stats --mode raw-data --json > "$out" || return 1
    REPO_BYTES=$(grep -o '"total_size":[0-9]*' "$out" | head -n 1 | cut -d: -f2)
    REPO_COUNT=$(grep -o '"snapshots_count":[0-9]*' "$out" | head -n 1 | cut -d: -f2)
}

# lost_message says which share went away, for die.
lost_message() {
    if [[ -n "$SOURCE_LOST" ]]; then
        echo "The Photos share is not mounted anymore, or its Immich folders are missing. Nothing was counted as backed up"
    elif [[ -n "$NAS_LOST" ]]; then
        echo "The Backups share is not mounted anymore. Nothing was counted as backed up"
    else
        echo "$1"
    fi
}

# photos_backup_failed records a backup that stopped. An update stops with it.
photos_backup_failed() {
    local code=$?
    rm -rf "$PHOTOS_DUMP_DIR" "$PHOTOS_BACKUP_DIR/.restic-output" ${PHOTOS_STAGE:+"$PHOTOS_STAGE"}
    state_set BACKUP_RESULT failed
    state_set BACKUP_MESSAGE "The backup failed while ${STEP:-starting}"
    state_set BACKUP_FINISHED "$(now_utc)"
    state_save
    exit "$((code == 0 ? 1 : code))"
}

# photos_backup backs up Photos to the NAS, and off the NAS when a disk is set.
photos_backup() {
    # A refused second run leaves the state of the running one alone.
    photos_lock
    state_load
    state_set BACKUP_STARTED "$(now_utc)"
    trap photos_backup_failed EXIT
    # Folders of runs that stopped without their trap, like at a power cut.
    rm -rf "$PHOTOS_BACKUP_DIR"/.stage.* "$PHOTOS_BACKUP_DIR/.restic-output"
    STEP="reading the settings"
    load_backup_settings
    photos_paths
    STEP="installing restic"
    ensure_restic
    require_password
    dump_database

    STEP="backing up to the NAS"
    log "Backing up the database and the originals to the NAS"
    backup_to "$NAS_REPO" nas_guard nas_source_guard || die "$(lost_message "The backup to the NAS failed")"
    state_set SNAPSHOT "$REPO_SNAPSHOT"
    state_set NAS_BYTES "$REPO_BYTES"
    state_set NAS_SNAPSHOTS "$REPO_COUNT"

    STEP="copying off the NAS"
    # The result off the NAS belongs to this disk. The agent shows no old copy
    # of another disk.
    state_set OFFSITE_DISK "$OFFSITE_ID"
    if [[ -z "$OFFSITE_PATH" ]]; then
        state_set OFFSITE_RESULT off
        state_set OFFSITE_MESSAGE ""
    elif ! offsite_check "$OFFSITE_PATH" || ! offsite_guard 2>/dev/null; then
        [[ -n "$OFFSITE_PROBLEM" ]] || OFFSITE_PROBLEM="the backup disk is not there, or not the disk saved in Backup settings"
        log "No copy off the NAS: $OFFSITE_PATH cannot be used, because $OFFSITE_PROBLEM"
        state_set OFFSITE_RESULT missing
        state_set OFFSITE_MESSAGE "$OFFSITE_PATH cannot be used: $OFFSITE_PROBLEM"
    else
        log "Backing up off the NAS, to $OFFSITE_PATH"
        OFFSITE_PROBLEM=""
        if backup_to "$OFFSITE_PATH/iglo-photos" offsite_guard offsite_source_guard; then
            state_set OFFSITE_RESULT ok
            state_set OFFSITE_MESSAGE ""
            state_set OFFSITE_FINISHED "$(now_utc)"
            state_set OFFSITE_BYTES "$REPO_BYTES"
            state_set OFFSITE_SNAPSHOTS "$REPO_COUNT"
        elif [[ -n "$SOURCE_LOST" ]]; then
            STEP="copying off the NAS"
            die "$(lost_message "")"
        elif [[ -n "$OFFSITE_PROBLEM" ]]; then
            log "The backup disk went away during the copy. The backup on the NAS is fine"
            state_set OFFSITE_RESULT missing
            state_set OFFSITE_MESSAGE "$OFFSITE_PATH cannot be used: $OFFSITE_PROBLEM"
        else
            log "The copy off the NAS failed. The backup on the NAS is fine"
            state_set OFFSITE_RESULT failed
            state_set OFFSITE_MESSAGE "The copy to $OFFSITE_PATH failed, see the log"
        fi
    fi
    # An update goes on after this, so the trap goes.
    trap - EXIT
    rm -rf "$PHOTOS_DUMP_DIR" "$PHOTOS_BACKUP_DIR/.restic-output"
    state_set BACKUP_RESULT ok
    state_set BACKUP_MESSAGE ""
    state_set BACKUP_FINISHED "$(now_utc)"
    state_save
    log "Photos is backed up in snapshot ${PHOTOS_STATE[SNAPSHOT]}"
}

photos_verify_failed() {
    local code=$?
    state_set VERIFY_RESULT failed
    state_set VERIFY_MESSAGE "${VERIFY_DETAIL:-The check failed while ${STEP:-starting}}"
    state_set VERIFY_FINISHED "$(now_utc)"
    state_save
    exit "$((code == 0 ? 1 : code))"
}

# photos_verify checks the repositories, and restores the database of the
# latest snapshot into a disposable database in the Photos container.
photos_verify() {
    local snapshot tables image
    photos_lock
    state_load
    trap photos_verify_failed EXIT
    STEP="reading the settings"
    load_backup_settings
    photos_paths
    ensure_restic
    STEP="looking for the backups on the NAS"
    require_password
    [[ -f "$NAS_REPO/config" ]] || die "There is no Photos backup on the NAS yet. Back up Photos first"

    # A random 5% of the data each time, so a check doesn't read the whole
    # library from the NAS.
    STEP="checking the backups on the NAS"
    log "Checking the backups on the NAS, and reading 5% of their data"
    run_guarded nas_guard --repo "$NAS_REPO" check --read-data-subset=5%
    STEP="reading the latest snapshot"
    snapshot=$(run_guarded nas_guard --repo "$NAS_REPO" snapshots --json --tag "$TAG" latest | grep -o '"id":"[a-f0-9]\{64\}"' | head -n 1 | cut -d'"' -f4)
    [[ "$snapshot" =~ ^[a-f0-9]{64}$ ]] || die "There is no Photos snapshot on the NAS"
    tables=$(run_guarded nas_guard --repo "$NAS_REPO" dump "$snapshot" "$PHOTOS_DUMP_DIR/tables.txt")
    image=$(run_guarded nas_guard --repo "$NAS_REPO" dump "$snapshot" "$PHOTOS_DUMP_DIR/postgres.txt")
    STEP="restoring into a disposable database"
    log "Restoring the database of snapshot ${snapshot:0:8} into a disposable database"
    push_recovery
    run_guarded nas_guard --repo "$NAS_REPO" dump "$snapshot" "$PHOTOS_DUMP_DIR/database.sql" |
        pct exec "$CT_ID" -- bash /opt/photos/recovery.sh verify "$image" "$tables"

    # A copy off the NAS that is set must be checked: when it can't be, the
    # check fails, so it never reads as checked.
    if [[ -n "$OFFSITE_PATH" ]]; then
        STEP="checking the copy off the NAS"
        if ! offsite_check "$OFFSITE_PATH" || ! offsite_guard 2>/dev/null; then
            VERIFY_DETAIL="The backups on the NAS are fine, but the copy off the NAS is missing: $OFFSITE_PROBLEM"
            die "$VERIFY_DETAIL"
        fi
        if [[ ! -f "$OFFSITE_PATH/iglo-photos/config" ]]; then
            VERIFY_DETAIL="The backups on the NAS are fine, but the copy off the NAS is missing: there is no copy on the disk yet"
            die "$VERIFY_DETAIL"
        fi
        log "Checking the copy off the NAS, and reading 5% of its data"
        if ! run_guarded offsite_guard --repo "$OFFSITE_PATH/iglo-photos" check --read-data-subset=5%; then
            VERIFY_DETAIL="The backups on the NAS are fine, but the check of the copy off the NAS failed${OFFSITE_PROBLEM:+: $OFFSITE_PROBLEM}"
            die "$VERIFY_DETAIL"
        fi
    fi
    trap - EXIT
    state_set VERIFY_RESULT ok
    state_set VERIFY_MESSAGE ""
    state_set VERIFY_SNAPSHOT "${snapshot:0:8}"
    state_set VERIFY_FINISHED "$(now_utc)"
    state_save
    log "The Photos backups are checked. Live data was not changed"
}
