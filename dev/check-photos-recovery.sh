#!/usr/bin/env bash
# Optional Linux-container test of the real recovery shell program, using fake
# Docker/SMB/copy commands. No host Docker socket is passed into the fixture.
set -euo pipefail
if [[ "${IGLO_RECOVERY_FIXTURE:-0}" != 1 ]]; then
    ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
    exec docker run --rm --network none --entrypoint bash \
        -e IGLO_RECOVERY_FIXTURE=1 \
        -v "$ROOT/dev/check-photos-recovery.sh:/check.sh:ro" \
        -v "$ROOT/deploy/stacks/photos/recovery.sh:/recovery.sh:ro" \
        ghcr.io/immich-app/postgres:14-vectorchord0.4.3-pgvectors0.2.0@sha256:bcf63357191b76a916ae5eb93464d65c07511da41e3bf7a8416db519b40b1c23 /check.sh
fi
base=$(mktemp -d)
export IGLO_PHOTOS_DIR="$base/config" IGLO_PHOTO_ROOT="$base/photos" IGLO_PHOTO_BACKUPS="$base/backups" MOCK_LOG="$base/log"
mkdir -p "$base/bin" "$IGLO_PHOTOS_DIR" "$IGLO_PHOTO_ROOT/upload" "$IGLO_PHOTO_BACKUPS"
export PATH="$base/bin:$PATH"
printf 'original image fixture\n' > "$IGLO_PHOTO_ROOT/upload/test.jpg"
cat > "$base/bin/findmnt" <<'EOF'
#!/bin/sh
echo cifs
EOF
cat > "$base/bin/rsync" <<'EOF'
#!/bin/bash
set -eu
args=("$@");src="${args[${#args[@]}-2]}";dest="${args[${#args[@]}-1]}"
cp -a "$src/." "$dest/"
EOF
cat > "$base/bin/docker" <<'EOF'
#!/bin/bash
set -euo pipefail
echo "$*" >> "$MOCK_LOG"
case "$*" in
    'compose ps --status running --services') printf 'immich-server\nimmich-machine-learning\n';;
    'compose exec -T database pg_dump '*) [[ "${FAIL_DUMP:-0}" != 1 ]] || exit 1;echo 'CREATE TABLE probe (id integer);';;
    'compose exec -T database psql '*) echo 1;;
    'inspect --format '{{.Config.Image}}' immich_server') echo 'ghcr.io/immich-app/immich-server:v3.3.5';;
    'inspect --format '{{.Config.Image}}' immich_postgres') echo 'ghcr.io/immich-app/postgres:14-vectorchord0.4.3-pgvectors0.2.0@sha256:bcf63357191b76a916ae5eb93464d65c07511da41e3bf7a8416db519b40b1c23';;
    'exec -i '*) cat >/dev/null;;
    'exec '*psql*) echo 1;;
esac
EOF
chmod +x "$base/bin/"*
if FAIL_DUMP=1 bash /recovery.sh backup;then echo 'accepted failed dump' >&2;exit 1;fi
test ! -e "$IGLO_PHOTO_BACKUPS/immich/latest"
grep -q 'compose start immich-server immich-machine-learning' "$MOCK_LOG"
bash /recovery.sh backup
name=$(cat "$IGLO_PHOTO_BACKUPS/immich/latest")
cmp "$IGLO_PHOTO_ROOT/upload/test.jpg" "$IGLO_PHOTO_BACKUPS/immich/$name/originals/upload/test.jpg"
bash /recovery.sh verify
test -f "$IGLO_PHOTO_BACKUPS/immich/$name/verified.txt"
printf 'corruption\n' >> "$IGLO_PHOTO_BACKUPS/immich/$name/originals/upload/test.jpg"
if bash /recovery.sh verify;then echo 'accepted corrupted originals' >&2;exit 1;fi
echo 'PASS: recovery shell failure cleanup, matched export, verification and corruption rejection (Docker/SMB commands mocked)'
