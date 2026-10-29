#!/usr/bin/env bash
# Optional Linux-container test of the Photos backups: install.sh --backup and
# --verify with backup.sh, recovery.sh and the pinned restic, on real restic
# repositories in temporary folders. Proxmox, Docker, systemd-creds, ZFS and the
# mounts are faked, but the Photos share is a real tmpfs, so a share that goes
# away during a run is real. It needs CAP_SYS_ADMIN for mount and unshare. No
# host Docker socket is passed into the fixture, and it has no network: the
# restic release is downloaded and checked first.
set -euo pipefail
if [[ "${IGLO_RECOVERY_FIXTURE:-0}" != 1 ]]; then
    ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
    version=$(sed -n 's/^RESTIC_VERSION=//p' "$ROOT/deploy/stacks/photos/backup.sh")
    digest=$(sed -n 's/^RESTIC_SHA256=//p' "$ROOT/deploy/stacks/photos/backup.sh")
    cache="${XDG_CACHE_HOME:-$HOME/.cache}/iglo"
    if [[ ! -x "$cache/restic-$version" ]]; then
        mkdir -p "$cache"
        curl --fail --silent --show-error --location --proto '=https' \
            "https://github.com/restic/restic/releases/download/v$version/restic_${version}_linux_amd64.bz2" -o "$cache/restic.bz2"
        printf '%s  %s\n' "$digest" "$cache/restic.bz2" | shasum -a 256 --check --status
        bunzip2 -f "$cache/restic.bz2"
        chmod 0755 "$cache/restic"
        mv "$cache/restic" "$cache/restic-$version"
    fi
    exec docker run --rm --network none --platform linux/amd64 --entrypoint bash \
        --cap-add SYS_ADMIN --security-opt seccomp=unconfined --security-opt apparmor=unconfined \
        -e IGLO_RECOVERY_FIXTURE=1 \
        -v "$ROOT/dev/check-photos-recovery.sh:/check.sh:ro" \
        -v "$ROOT/deploy:/deploy:ro" \
        -v "$cache/restic-$version:/restic:ro" \
        ghcr.io/immich-app/postgres:14-vectorchord0.4.3-pgvectors0.2.0@sha256:bcf63357191b76a916ae5eb93464d65c07511da41e3bf7a8416db519b40b1c23 /check.sh
fi

base=$(mktemp -d)
log="$base/log"
export IGLO_RESTIC="$base/bin/restic" IGLO_PHOTOS_BACKUP_CONF="$base/photos-backup.conf" IGLO_PHOTOS_BACKUP_DIR="$base/state"
export IGLO_PHOTOS_LOCK="$base/backup.lock" RESTIC_HOST=iglo-photos
export RESTIC_CACHE_DIR="$base/cache" FIXTURE="$base" PATH="$base/bin:$PATH"
state="$base/state/state"
photos=/mnt/iglo/shares/photos1 backups=/mnt/iglo/shares/backups1 disk="$base/usb/disk"
mkdir -p "$base/bin" /etc/iglo/storage "$photos" "$backups" "$disk" /run/lock
printf 'ROLE=photos\nREAD_ONLY=false\nSOURCE=//nas/photos\n' > /etc/iglo/storage/photos1.conf
printf 'ROLE=backups\nREAD_ONLY=false\nSOURCE=//nas/backups\n' > /etc/iglo/storage/backups1.conf
# fill_photos mounts the Photos share and writes its files, with the folder
# markers that Immich writes when it starts.
fill_photos() {
    mount -t tmpfs photos "$photos"
    mkdir -p "$photos/upload/user" "$photos/thumbs/user" "$photos/library" "$photos/profile" "$photos/backups"
    for folder in library upload profile backups thumbs; do : > "$photos/$folder/.immich"; done
    printf 'original image fixture\n' > "$photos/upload/user/test.jpg"
    printf 'thumbnail\n' > "$photos/thumbs/user/test.webp"
}
fill_photos

fake() { cat > "$base/bin/$1";chmod +x "$base/bin/$1"; }
fake pveversion <<'EOF'
#!/bin/sh
echo 'pve-manager/9.0.3/0123456789abcdef (running kernel: 6.14.8-2-pve)'
EOF
fake dpkg-query <<'EOF'
#!/bin/sh
echo 6.0.5-2
EOF
# The shares are SMB mounts. A USB disk is mounted at $FIXTURE/usb, until
# $FIXTURE/unplugged exists. $FIXTURE/zfs is a ZFS pool on the system disk.
# Everything else is on the root disk.
fake findmnt <<'EOF'
#!/bin/bash
case "$*" in
    *"--mountpoint /mnt/iglo/shares/photos1 "*) echo 'cifs //nas/photos';;
    *"--mountpoint /mnt/iglo/shares/backups1 "*) echo 'cifs //nas/backups';;
    *"--target /mnt/iglo/shares/photos1"*) if [[ -e "$FIXTURE/photos-gone" ]]; then echo '/ ext4 /dev/vda1'; else echo '/mnt/iglo/shares/photos1 cifs //nas/photos'; fi;;
    *"--target /mnt/iglo/shares/backups1"*) if [[ -e "$FIXTURE/nas-gone" ]]; then echo '/ ext4 /dev/vda1'; else echo '/mnt/iglo/shares/backups1 cifs //nas/backups'; fi;;
    *"--target $FIXTURE/usb"*) if [[ -e "$FIXTURE/unplugged" ]]; then echo '/ ext4 /dev/vda1'; else echo "$FIXTURE/usb ext4 /dev/sdb1"; fi;;
    *"--target $FIXTURE/zfs"*) echo "$FIXTURE/zfs zfs bak/photos";;
    *"--target "*) echo '/ ext4 /dev/vda1';;
    *) exit 1;;
esac
EOF
fake lsblk <<'EOF'
#!/bin/bash
case "${*: -1}" in
    /dev/sdb1) printf '/dev/sdb1 part\n/dev/sdb disk\n';;
    /dev/vda1) printf '/dev/vda1 part\n/dev/vda disk\n';;
    /dev/vda2) printf '/dev/vda2 part\n/dev/vda disk\n';;
    *) exit 1;;
esac
EOF
# zpool list -v -H -P -L -o name: the pool, then its vdevs.
fake zpool <<'EOF'
#!/bin/bash
[[ "${*: -1}" == bak ]] && printf 'bak\n\t/dev/vda2\n'
EOF
# Sealing is faked: the file holds the plain value.
fake systemd-creds <<'EOF'
#!/bin/bash
case "$1" in
    encrypt) cat > "$4";;
    decrypt) cat "$3";;
esac
EOF
fake openssl <<'EOF'
#!/bin/bash
if [[ "$2" == -base64 ]]; then head -c "$3" /dev/urandom | base64 -w0; else head -c "$3" /dev/urandom | od -An -tx1 | tr -d ' \n'; fi
EOF
fake pct <<'EOF'
#!/bin/bash
case "$1" in
    config) echo 'tags: iglo;photos';;
    status) echo 'status: running';;
    push) ;;
    pull) printf 'STORAGE=local-lvm\nLIBRARY_BINDING=photos1\nOUTPUT_BINDING=\nBACKUP_BINDING=backups1\nCORES=4\nMEMORY_MIB=8192\nDISK_GB=64\nPRIVILEGED=false\n' > "$4";;
    exec)
        shift 3
        # recovery.sh is in /opt/photos of the container.
        if [[ "$1" == bash && "$2" == /opt/photos/recovery.sh ]]; then
            shift 2
            IGLO_PHOTOS_DIR="$FIXTURE" exec bash /deploy/stacks/photos/recovery.sh "$@"
        fi
        exit 1;;
esac
EOF
# Docker in the Photos container: a database with 3 tables. With FAIL_DUMP=1,
# pg_dump stops early. The disposable database keeps what it restores.
fake docker <<'EOF'
#!/bin/bash
case "$*" in
    'compose exec -T database pg_dump '*)
        echo 'CREATE TABLE asset (id integer);'
        echo "INSERT INTO asset VALUES (${DUMP_ROW:-1});"
        [[ "${FAIL_DUMP:-0}" == 1 ]] && exit 0
        printf -- '--\n-- PostgreSQL database dump complete\n--\n\n';;
    'compose exec -T database psql '*) echo 3;;
    'inspect --format {{.Config.Image}} immich_server') echo 'ghcr.io/immich-app/immich-server:v3.3.5';;
    'inspect --format {{.Config.Image}} immich_postgres') echo 'ghcr.io/immich-app/postgres:14-vectorchord0.4.3-pgvectors0.2.0@sha256:bcf63357191b76a916ae5eb93464d65c07511da41e3bf7a8416db519b40b1c23';;
    'exec -i '*) cat > "$FIXTURE/restored.sql";;
    'exec '*psql*) echo 3;;
esac
EOF
# The real restic, run by backup.sh in its own mount namespace. With
# UNREADABLE=1, a backup ends like restic with files it could not read: a
# snapshot and exit code 3. With UNPLUG_DURING=folder, the disk in that folder
# goes away while a backup or check there runs. With DETACH_PHOTOS=1, the
# Photos share is unmounted on the host (the first mount namespace) while a
# backup runs. With NAS_GONE=1, the Backups share goes away during a backup.
fake restic <<'EOF'
#!/bin/bash
if [[ -n "${UNPLUG_DURING:-}" && " $* " == *" --repo $UNPLUG_DURING/"* && (" $* " == *" backup "* || " $* " == *" check "*) ]]; then
    rm -f "$UNPLUG_DURING/.iglo-photos-disk"
fi
if [[ " $* " == *" backup "* ]]; then
    if [[ "${DETACH_PHOTOS:-0}" == 1 ]]; then
        nsenter --mount=/proc/1/ns/mnt umount -l /mnt/iglo/shares/photos1
        touch "$FIXTURE/photos-gone"
    fi
    [[ "${NAS_GONE:-0}" != 1 ]] || touch "$FIXTURE/nas-gone"
    if [[ "${UNREADABLE:-0}" == 1 ]]; then /restic "$@" >/dev/null;exit 3; fi
fi
exec /restic "$@"
EOF
install=/deploy/stacks/photos/install.sh
value() { sed -n "s/^$1=//p" "$state"; }
password_file=/etc/iglo/credentials/host/photos_restic_password.cred
restic_ls() { /restic --repo "$1" ls --tag "${2:-iglo-photos}" latest < "$password_file"; }
# count prints how many snapshots with tag $2 repository $1 has.
count() { { /restic --repo "$1" snapshots --json --tag "$2" < "$password_file" | grep -o '"id":"[a-f0-9]\{64\}"' || true; } | wc -l; }
# pending makes a pending snapshot at time $1 with a dump of row $2, like a
# backup that did not finish.
pending() {
    mkdir -p "$base/state/database"
    printf 'INSERT INTO asset VALUES (%s);\n' "$2" > "$base/state/database/database.sql"
    printf '3\n' > "$base/state/database/tables.txt"
    /restic --repo "$backups/immich-restic" backup --quiet --tag iglo-photos-pending --time "$1" "$base/state/database" "$photos" < "$password_file" >/dev/null
    rm -rf "$base/state/database"
}
fail() { echo "FAIL: $*" >&2;cat "$log" >&2;exit 1; }
id=0123456789abcdef0123456789abcdef
settings() { printf 'BACKUP_TIME=01:00\nOFFSITE_PATH=%s\nOFFSITE_ID=%s\nKEEP_DAILY=1\nKEEP_WEEKLY=0\nKEEP_MONTHLY=0\n' "$1" "$2" > "$IGLO_PHOTOS_BACKUP_CONF"; }
# The agent writes the marker of the disk when the folder is saved.
printf '%s\n' "$id" > "$disk/.iglo-photos-disk"
settings "$disk" "$id"

password="correct horse battery staple"
# Without a sealed password, a backup stops and says how to set one.
if bash "$install" --backup --ctid 170 >"$base/run" 2>&1; then fail 'backed up without a password'; fi
cat "$base/run" >>"$log"
grep -q 'Photos has no backup password. Set one with iglo-photos-key set on the host.' "$base/run" || fail "no clear message: $(cat "$base/run")"

# iglo-photos-key set reads the password twice on stdin, checks it and seals it.
key() { IGLO_LIB_DIR=/deploy bash /deploy/iglo-photos-key set; }
if printf 'short\nshort\n' | key >>"$log" 2>&1; then fail 'set took a short password'; fi
if printf '%s\n%s\n' "$password" "${password}x" | key >>"$log" 2>&1; then fail 'set took two different passwords'; fi
[[ ! -e "$password_file" ]] || fail 'a refused password was sealed'
printf '%s\n%s\n' "$password" "$password" | key >>"$log" 2>&1 || fail 'set'
[[ "$(cat "$password_file")" == "$password" ]] || fail 'set did not seal the password'

# A cut dump fails the backup, and makes no snapshot.
if FAIL_DUMP=1 bash "$install" --backup --ctid 170 >>"$log" 2>&1; then fail 'accepted a cut dump'; fi
[[ "$(value BACKUP_RESULT)" == failed && "$(value BACKUP_MESSAGE)" == *'dumping the database'* ]] || fail 'no failed state'
[[ ! -e "$backups/immich-restic/config" && ! -e "$base/state/database" ]] || fail 'a cut dump reached a repository'
[[ -z "$(find "$base/state" -name '.stage.*')" ]] || fail 'the staging folder stayed'

# A second run waits for none: it stops, and leaves the state of the first.
# The sleep holds the lock itself, so killing it frees the lock.
(exec 7>"$IGLO_PHOTOS_LOCK";flock 7;exec sleep 30) &
holder=$!
sleep 1
cp "$state" "$base/state.before"
if bash "$install" --backup --ctid 170 >"$base/second" 2>&1; then fail 'two backups ran at once'; fi
grep -q 'Another Photos backup or check runs' "$base/second" || fail "no clear refusal: $(cat "$base/second")"
if bash "$install" --verify --ctid 170 >"$base/second" 2>&1; then fail 'a check ran during a backup'; fi
cmp -s "$state" "$base/state.before" || fail 'a refused run changed the state'
kill "$holder";wait "$holder" 2>/dev/null || true

# A backup saves the dump and the originals in one snapshot, on the NAS and off
# it. It first removes the staging folders of runs that stopped without their trap.
mkdir -p "$base/state/.stage.abandoned"
printf 'old dump\n' > "$base/state/.stage.abandoned/database.sql"
bash "$install" --backup --ctid 170 >>"$log" 2>&1 || fail 'backup'
[[ "$(value BACKUP_RESULT)" == ok && "$(value OFFSITE_RESULT)" == ok && "$(value NAS_SNAPSHOTS)" == 1 ]] || fail "state: $(cat "$state")"
[[ "$(value NAS_BYTES)" =~ ^[1-9][0-9]*$ && "$(value SNAPSHOT)" =~ ^[a-f0-9]{8}$ ]] || fail 'sizes'
listing=$(restic_ls "$backups/immich-restic")
grep -q "$photos/upload/user/test.jpg" <<< "$listing" || fail 'originals missing'
grep -q "$base/state/database/database.sql" <<< "$listing" || fail 'dump missing'
grep -q "$base/state/database/tables.txt" <<< "$listing" || fail 'table count missing'
! grep -q thumbs <<< "$listing" || fail 'derivatives backed up'
grep -q "$photos/upload/user/test.jpg" <<< "$(restic_ls "$disk/iglo-photos")" || fail 'no copy off the NAS'
[[ "$(count "$backups/immich-restic" iglo-photos-pending)" == 0 ]] || fail 'a finished backup stayed pending'
[[ ! -e "$base/state/.stage.abandoned" ]] || fail 'an abandoned staging folder stayed'
[[ "$(value OFFSITE_DISK)" == "$id" ]] || fail 'the copy off the NAS does not name its disk'
[[ ! -e "$base/state/database" && -z "$(find "$base/state" -name '.stage.*')" ]] || fail 'the dump stayed on the host'

# The retention keeps one daily snapshot.
printf 'new photo\n' > "$photos/upload/user/new.jpg"
DUMP_ROW=2 bash "$install" --backup --ctid 170 >>"$log" 2>&1 || fail 'second backup'
[[ "$(value NAS_SNAPSHOTS)" == 1 && "$(value OFFSITE_SNAPSHOTS)" == 1 ]] || fail "retention: $(cat "$state")"
grep -q new.jpg <<< "$(restic_ls "$backups/immich-restic")" || fail 'second snapshot misses the new photo'

# Verify restores the dump of the latest finished snapshot, never of a pending one.
pending "$(date -u -d '-3 days' '+%Y-%m-%d %H:%M:%S')" 98
pending "$(date -u '+%Y-%m-%d %H:%M:%S')" 99
bash "$install" --verify --ctid 170 >>"$log" 2>&1 || fail 'verify'
[[ "$(value VERIFY_RESULT)" == ok ]] || fail 'verify state'
grep -q 'INSERT INTO asset VALUES (2);' "$base/restored.sql" || fail "restored another dump: $(cat "$base/restored.sql")"

# A backup with unreadable originals fails and stays pending. The next backup
# forgets the pending snapshots older than a day, and keeps the newer ones.
if UNREADABLE=1 DUMP_ROW=3 bash "$install" --backup --ctid 170 >>"$log" 2>&1; then fail 'accepted an incomplete snapshot'; fi
[[ "$(count "$backups/immich-restic" iglo-photos)" == 1 && "$(count "$backups/immich-restic" iglo-photos-pending)" == 3 ]] || fail 'incomplete snapshot counted'
DUMP_ROW=4 bash "$install" --backup --ctid 170 >>"$log" 2>&1 || fail 'backup after an incomplete one'
[[ "$(count "$backups/immich-restic" iglo-photos-pending)" == 2 ]] || fail "old pending snapshots stayed: $(count "$backups/immich-restic" iglo-photos-pending)"

# A Photos share unmounted on the host during the backup: restic still reads
# the share in its own mount namespace, but the snapshot never counts, and the
# run fails.
nas_before=$(count "$backups/immich-restic" iglo-photos)
if DETACH_PHOTOS=1 bash "$install" --backup --ctid 170 >"$base/run" 2>&1; then cat "$base/run" >>"$log";fail 'a lost Photos share counted as backed up'; fi
cat "$base/run" >>"$log"
grep -q 'The Photos share is not mounted anymore' "$base/run" || fail "no clear message: $(cat "$base/run")"
[[ "$(value BACKUP_RESULT)" == failed && "$(count "$backups/immich-restic" iglo-photos)" == "$nas_before" ]] || fail 'promoted a snapshot of a lost share'
grep -q "$photos/upload/user/test.jpg" <<< "$(restic_ls "$backups/immich-restic" iglo-photos-pending)" || fail 'the namespace did not keep the share'
[[ -z "$(ls -A "$photos")" ]] || fail 'the share is still mounted on the host'
rm "$base/photos-gone"
fill_photos

# A Photos share without the markers of Immich, like an empty mount point.
rm "$photos/profile/.immich"
if bash "$install" --backup --ctid 170 >"$base/run" 2>&1; then cat "$base/run" >>"$log";fail 'backed up a share without its markers'; fi
cat "$base/run" >>"$log"
grep -q 'its Immich folders are missing' "$base/run" || fail "no clear message: $(cat "$base/run")"
: > "$photos/profile/.immich"

# The Backups share goes away during the backup: the run fails.
if NAS_GONE=1 bash "$install" --backup --ctid 170 >"$base/run" 2>&1; then cat "$base/run" >>"$log";fail 'a lost Backups share counted as backed up'; fi
cat "$base/run" >>"$log"
grep -q 'The Backups share is not mounted anymore' "$base/run" || fail "no clear message: $(cat "$base/run")"
[[ "$(count "$backups/immich-restic" iglo-photos)" == "$nas_before" ]] || fail 'promoted a snapshot on a lost Backups share'
rm "$base/nas-gone"

# An unplugged disk leaves the backup on the NAS, and writes nothing to the root disk.
touch "$base/unplugged"
before=$(find "$base/usb" | wc -l)
bash "$install" --backup --ctid 170 >>"$log" 2>&1 || fail 'backup with the disk unplugged'
[[ "$(value BACKUP_RESULT)" == ok && "$(value OFFSITE_RESULT)" == missing && "$(value OFFSITE_MESSAGE)" == *'root disk'* ]] || fail "unplugged: $(cat "$state")"
[[ "$(find "$base/usb" | wc -l)" == "$before" ]] || fail 'wrote to the root disk'
# Verify never reports a missing copy off the NAS as checked.
if bash "$install" --verify --ctid 170 >>"$log" 2>&1; then fail 'verify passed without the copy off the NAS'; fi
[[ "$(value VERIFY_RESULT)" == failed && "$(value VERIFY_MESSAGE)" == *'copy off the NAS is missing'* ]] || fail "verify without the disk: $(cat "$state")"
rm "$base/unplugged"
# Nor one that goes away during its check.
if UNPLUG_DURING="$disk" bash "$install" --verify --ctid 170 >>"$log" 2>&1; then fail 'verify passed while the disk went away'; fi
[[ "$(value VERIFY_RESULT)" == failed && "$(value VERIFY_MESSAGE)" == *'copy off the NAS failed'* ]] || fail "verify with the disk going away: $(cat "$state")"
printf '%s\n' "$id" > "$disk/.iglo-photos-disk"
bash "$install" --verify --ctid 170 >>"$log" 2>&1 || fail 'verify with the disk back'

# A disk unplugged during the copy: the snapshot there never counts as finished.
offsite_before=$(count "$disk/iglo-photos" iglo-photos)
UNPLUG_DURING="$disk" bash "$install" --backup --ctid 170 >>"$log" 2>&1 || fail 'backup with the disk unplugged during the copy'
[[ "$(value BACKUP_RESULT)" == ok && "$(value OFFSITE_RESULT)" == missing ]] || fail "unplugged during the copy: $(cat "$state")"
[[ "$(count "$disk/iglo-photos" iglo-photos)" == "$offsite_before" ]] || fail 'a copy without its disk counted'
printf '%s\n' "$id" > "$disk/.iglo-photos-disk"

# A folder without the marker of the saved disk never gets a repository.
mkdir -p "$base/usb/fresh"
settings "$base/usb/fresh" "$id"
bash "$install" --backup --ctid 170 >>"$log" 2>&1 || fail 'backup with an unknown disk'
[[ "$(value OFFSITE_RESULT)" == missing && ! -e "$base/usb/fresh/iglo-photos" ]] || fail "unknown disk: $(cat "$state")"

# A repository folder that links to the NAS is refused.
mkdir -p "$base/usb/linked"
printf '%s\n' "$id" > "$base/usb/linked/.iglo-photos-disk"
ln -s "$backups/immich-restic" "$base/usb/linked/iglo-photos"
settings "$base/usb/linked" "$id"
nas_before=$(count "$backups/immich-restic" iglo-photos)
bash "$install" --backup --ctid 170 >>"$log" 2>&1 || fail 'backup with a linked repository'
[[ "$(value OFFSITE_RESULT)" == missing && "$(value OFFSITE_MESSAGE)" == *'link'* ]] || fail "linked repository: $(cat "$state")"
[[ "$(count "$backups/immich-restic" iglo-photos)" == "$nas_before" ]] || fail 'the copy off the NAS went to the NAS'

# A ZFS pool on the system disk is not off the system.
mkdir -p "$base/zfs"
printf '%s\n' "$id" > "$base/zfs/.iglo-photos-disk"
settings "$base/zfs" "$id"
bash "$install" --backup --ctid 170 >>"$log" 2>&1 || fail 'backup with a ZFS folder'
[[ "$(value OFFSITE_RESULT)" == missing && "$(value OFFSITE_MESSAGE)" == *'same disk as the system'* && ! -e "$base/zfs/iglo-photos" ]] || fail "zfs: $(cat "$state")"
settings "$disk" "$id"

# A corrupt repository fails the check.
find "$backups/immich-restic/data" -type f -exec sh -c 'printf corrupt >> "$1"' sh {} \;
if bash "$install" --verify --ctid 170 >>"$log" 2>&1; then fail 'accepted a corrupt repository'; fi
[[ "$(value VERIFY_RESULT)" == failed ]] || fail 'no failed check'

# Without its password, a backup never makes a new one for an old repository.
rm -r /etc/iglo/credentials/host
if bash "$install" --backup --ctid 170 >>"$log" 2>&1; then fail 'backed up without a password'; fi
[[ ! -e /etc/iglo/credentials/host ]] || fail 'sealed a new password'

# set refuses a password that does not open the backups that exist, like after
# a reset TPM, and takes the right one.
wrong="a long but wrong password"
if printf '%s\n%s\n' "$wrong" "$wrong" | key >"$base/run" 2>&1; then cat "$base/run" >>"$log";fail 'set took a wrong password'; fi
cat "$base/run" >>"$log"
grep -q 'does not open the Photos backups' "$base/run" || fail "no clear refusal: $(cat "$base/run")"
[[ ! -e "$password_file" ]] || fail 'sealed a wrong password'
printf '%s\n%s\n' "$password" "$password" | key >>"$log" 2>&1 || fail 'set with the right password'
[[ "$(cat "$password_file")" == "$password" ]] || fail 'the right password is not sealed'

# A new Photos on a host with these backups: the installer checks the typed
# password before it makes anything, removes the answers file at once, and
# never replaces a sealed password that works.
answers="$base/photos.answers"
answer() { printf 'STORAGE=local-lvm\nLIBRARY_BINDING=photos1\nOUTPUT_BINDING=\nBACKUP_BINDING=backups1\nCORES=4\nMEMORY_MIB=8192\nDISK_GB=64\nPRIVILEGED=false\nBACKUP_PASSWORD=%s\n' "$1" > "$answers"; }
answer "$wrong"
bash "$install" --answers "$answers" >"$base/run" 2>&1 || true
cat "$base/run" >>"$log"
grep -q 'does not open the Photos backups' "$base/run" || fail "the installer took a wrong password: $(cat "$base/run")"
[[ ! -e "$answers" ]] || fail 'the answers file with the password stayed'
[[ "$(cat "$password_file")" == "$password" ]] || fail 'the installer replaced a working password'
rm -r /etc/iglo/credentials/host
answer "$password"
bash "$install" --answers "$answers" >"$base/run" 2>&1 || true
cat "$base/run" >>"$log"
grep -q 'The backup password is sealed on this host' "$base/run" || fail "the installer did not seal the password: $(cat "$base/run")"
[[ "$(cat "$password_file")" == "$password" && ! -e "$answers" ]] || fail 'the installer password'
! grep -qF "$wrong" "$log" || fail 'the wrong password is in the log'

! grep -qF "$password" "$log" || fail 'the password is in the log'
echo 'PASS: Photos backups with restic: typed backup password with iglo-photos-key set and the installer, refused when it does not open existing backups, snapshot of dump and originals, one run at a time, abandoned staging folders, pending snapshots, retention, private mount namespace with a Photos share lost during the backup, Immich markers, Backups share lost during the backup, copy off the NAS, disk marker and its result, unplugged disk before and during the copy and the check, linked repository, ZFS on the system disk, verify, corruption and password checks (Proxmox, Docker and most mounts faked)'
