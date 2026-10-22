#!/usr/bin/env bash
# Run inside the Photos guest. Never overwrites a live database or originals.
# backup: stop writers, copy originals + dump, checksum, publish, restart.
# verify: checksum and restore into an isolated disposable PostgreSQL instance.
set -euo pipefail
umask 077
cd "${IGLO_PHOTOS_DIR:-/opt/photos}"
PHOTO_ROOT="${IGLO_PHOTO_ROOT:-/data/photos}"
BACKUP_ROOT="${IGLO_PHOTO_BACKUPS:-/data/backups}/immich"
exec 9>/run/lock/iglo-photos-recovery.lock
flock -n 9 || { echo "Another Photos recovery operation is running" >&2;exit 1; }
for path in "$PHOTO_ROOT" "$(dirname "$BACKUP_ROOT")"; do
    [[ "$(findmnt --mountpoint "$path" --noheadings --output FSTYPE)" == cifs ]] || { echo "Required SMB share is unavailable: $path" >&2;exit 1; }
done
resume=() verify_container="" partial=""
cleanup() {
    local code=$?
    if [[ -n "$verify_container" ]]; then docker rm -fv "$verify_container" >/dev/null 2>&1 || true; fi
    if ((${#resume[@]})); then
        if ! docker compose start "${resume[@]}"; then echo "Photos needs attention: services could not restart" >&2;code=1;fi
    fi
    if [[ -n "$partial" ]]; then echo "Incomplete backup kept for inspection: $partial" >&2;fi
    exit "$code"
}
trap cleanup EXIT
case "${1:-}" in
    backup)
        install -d -m 0700 "$BACKUP_ROOT"
        # Preserve the previous running state: never start a manually stopped service.
        while IFS= read -r service; do
            case "$service" in immich-server|immich-machine-learning) resume+=("$service");; esac
        done < <(docker compose ps --status running --services)
        ((${#resume[@]} == 0)) || docker compose stop "${resume[@]}"
        partial=$(mktemp -d "$BACKUP_ROOT/.partial.XXXXXXXX")
        docker compose exec -T database pg_dump --username=postgres --dbname=immich --clean --if-exists | gzip > "$partial/database.sql.gz"
        gzip -t "$partial/database.sql.gz"
        docker compose exec -T database psql --username=postgres --dbname=immich --tuples-only --no-align --command="SELECT count(*) FROM pg_catalog.pg_class WHERE relkind='r' AND relnamespace='public'::regnamespace" > "$partial/tables.txt"
        grep -E '^[1-9][0-9]*$' "$partial/tables.txt" >/dev/null
        docker inspect --format '{{.Config.Image}}' immich_server > "$partial/version.txt"
        docker inspect --format '{{.Config.Image}}' immich_postgres > "$partial/postgres.txt"
        mkdir "$partial/originals"
        # Derivatives can be rebuilt; originals/profile photos/markers are preserved.
        rsync -rlt --exclude=/thumbs/ --exclude=/encoded-video/ --exclude=/backups/ "$PHOTO_ROOT/" "$partial/originals/"
        (
            cd "$partial"
            find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 -r sha256sum > SHA256SUMS
        )
        name="$(date -u +%Y%m%dT%H%M%SZ)-${partial##*.}"
        mv "$partial" "$BACKUP_ROOT/$name";partial=""
        printf '%s\n' "$name" > "$BACKUP_ROOT/latest.tmp";mv "$BACKUP_ROOT/latest.tmp" "$BACKUP_ROOT/latest"
        echo "Matched database and originals backup: $BACKUP_ROOT/$name"
        ;;
    verify)
        name="${2:-$(cat "$BACKUP_ROOT/latest")}"
        [[ "$name" =~ ^[0-9]{8}T[0-9]{6}Z-[A-Za-z0-9]+$ ]] || { echo "Invalid backup name" >&2;exit 1; }
        cd "$BACKUP_ROOT/$name"
        sha256sum --check --strict SHA256SUMS
        gzip -t database.sql.gz
        image=$(cat postgres.txt)
        [[ "$image" =~ ^ghcr.io/immich-app/postgres:[A-Za-z0-9._-]+@sha256:[a-f0-9]{64}$ ]] || { echo "Unsupported database image" >&2;exit 1; }
        expected=$(cat tables.txt);[[ "$expected" =~ ^[1-9][0-9]*$ ]] || exit 1
        verify_container="iglo-photos-verify-$(openssl rand -hex 8)"
        docker run --detach --name "$verify_container" --network none --memory 1536m --cpus 1 \
            --mount type=volume,destination=/var/lib/postgresql/data \
            -e "POSTGRES_PASSWORD=$(openssl rand -hex 32)" -e POSTGRES_DB=immich "$image" >/dev/null
        ready=false
        for _ in $(seq 1 90); do
            if docker exec "$verify_container" pg_isready --username=postgres --dbname=immich >/dev/null 2>&1; then ready=true;break;fi
            sleep 1
        done
        [[ "$ready" == true ]] || { echo "Recovery database did not start" >&2;exit 1; }
        gzip -dc database.sql.gz | docker exec -i "$verify_container" psql --username=postgres --dbname=immich --set=ON_ERROR_STOP=on >/dev/null
        actual=$(docker exec "$verify_container" psql --username=postgres --dbname=immich --tuples-only --no-align --command="SELECT count(*) FROM pg_catalog.pg_class WHERE relkind='r' AND relnamespace='public'::regnamespace")
        [[ "$actual" == "$expected" ]] || { echo "Restored table count differs" >&2;exit 1; }
        date -u +%FT%TZ > verified.txt
        echo "Verified checksums and restored $actual tables into an isolated temporary database. Live data was not changed."
        ;;
    *) echo "Usage: recovery.sh backup | verify [backup-name]" >&2;exit 1;;
esac
