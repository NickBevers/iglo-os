#!/usr/bin/env bash
# Run inside the Photos guest by the backups on the host (backup.sh). It
# never changes the live database or the originals.
# info: the table count and the images, for the backup.
# dump: the database as SQL on stdout. Immich keeps running: pg_dump reads one
#   consistent snapshot of the database.
# verify IMAGE TABLES: restores the SQL on stdin into a disposable PostgreSQL
#   container without a network or host folders, and compares the table count.
set -euo pipefail
umask 077
cd "${IGLO_PHOTOS_DIR:-/opt/photos}"
exec 9>/run/lock/iglo-photos-recovery.lock
flock -n 9 || { echo "Another Photos recovery operation is running" >&2;exit 1; }
verify_container=""
cleanup() {
    local code=$?
    if [[ -n "$verify_container" ]]; then docker rm -fv "$verify_container" >/dev/null 2>&1 || true; fi
    exit "$code"
}
trap cleanup EXIT
TABLES_QUERY="SELECT count(*) FROM pg_catalog.pg_class WHERE relkind='r' AND relnamespace='public'::regnamespace"
IMAGE_PATTERN='^ghcr.io/immich-app/postgres:[A-Za-z0-9._-]+@sha256:[a-f0-9]{64}$'
case "${1:-}" in
    info)
        tables=$(docker compose exec -T database psql --username=postgres --dbname=immich --tuples-only --no-align --command="$TABLES_QUERY")
        [[ "$tables" =~ ^[1-9][0-9]*$ ]] || { echo "The database has no tables" >&2;exit 1; }
        server=$(docker inspect --format '{{.Config.Image}}' immich_server)
        postgres=$(docker inspect --format '{{.Config.Image}}' immich_postgres)
        [[ "$postgres" =~ $IMAGE_PATTERN ]] || { echo "Unsupported database image" >&2;exit 1; }
        printf 'TABLES=%s\nSERVER_IMAGE=%s\nPOSTGRES_IMAGE=%s\n' "$tables" "$server" "$postgres"
        ;;
    dump)
        # The command of the Immich v3.3.5 backup guide, checked 29 October 2026.
        docker compose exec -T database pg_dump --username=postgres --dbname=immich --clean --if-exists
        ;;
    verify)
        image="${2:-}" expected="${3:-}"
        [[ "$image" =~ $IMAGE_PATTERN ]] || { echo "Unsupported database image" >&2;exit 1; }
        [[ "$expected" =~ ^[1-9][0-9]*$ ]] || { echo "Invalid table count" >&2;exit 1; }
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
        docker exec -i "$verify_container" psql --username=postgres --dbname=immich --set=ON_ERROR_STOP=on >/dev/null
        actual=$(docker exec "$verify_container" psql --username=postgres --dbname=immich --tuples-only --no-align --command="$TABLES_QUERY")
        [[ "$actual" == "$expected" ]] || { echo "Restored table count differs" >&2;exit 1; }
        echo "Restored $actual tables into an isolated temporary database. Live data was not changed."
        ;;
    *) echo "Usage: recovery.sh info | dump | verify IMAGE TABLES" >&2;exit 1;;
esac
