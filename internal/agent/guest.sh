#!/bin/sh
# Runs inside an LXC through `pct exec`. Usage: guest.sh check|update
# Lines that start with "IGLO " are results for the agent. Everything else is log.
set -eu

mode="${1:-}"
# On the Proxmox host itself we only touch apt.
apt_only="${IGLO_APT_ONLY:-0}"
export DEBIAN_FRONTEND=noninteractive

has() {
    command -v "$1" >/dev/null 2>&1
}

apt_check() {
    apt-get update -q
    # "name/suite new arch [upgradable from: old]" -> "IGLO package name old new"
    apt list --upgradable 2>/dev/null |
        sed -n 's#^\([^/]*\)/[^ ]* \([^ ]*\) [^ ]* \[upgradable from: \([^]]*\)\]$#IGLO package \1 \3 \2#p'
}

apt_update() {
    apt-get update -q
    apt-get -y -q -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold dist-upgrade
}

# Prints "project|working_dir" for each running Compose project.
compose_projects() {
    docker ps --format '{{.Label "com.docker.compose.project"}}|{{.Label "com.docker.compose.project.working_dir"}}' |
        grep -v '^|' | grep -v '|$' | sort -u || true
}

# compose runs Docker Compose inside the project's own folder, like the
# installers do. It then reads COMPOSE_FILE and COMPOSE_PROFILES from .env,
# and relative COMPOSE_FILE paths resolve there; --project-directory alone
# does not do that for them. The config_files label is not used: an older
# version of this script passed those files in reverse order and wrote that
# order back into the label, so the GPU file of Photos lost to compose.yaml
# every other week.
compose() {
    project="$1"
    dir="$2"
    shift 2

    (cd "$dir" && docker compose -p "$project" "$@")
}

# The loops read from a here-document, not a pipe, so a failing command stops
# the whole script instead of only a subshell.
docker_check() {
    projects="$(compose_projects)"
    while IFS='|' read -r project dir; do
        [ -n "$project" ] || continue
        compose "$project" "$dir" pull -q --ignore-buildable
        for container in $(compose "$project" "$dir" ps -q); do
            image="$(docker inspect -f '{{.Config.Image}}' "$container")"
            running="$(docker inspect -f '{{.Image}}' "$container")"
            latest="$(docker image inspect -f '{{.Id}}' "$image")"
            if [ "$running" != "$latest" ]; then
                service="$(docker inspect -f '{{index .Config.Labels "com.docker.compose.service"}}' "$container")"
                echo "IGLO image $service $image"
            fi
        done
    done <<EOF
$projects
EOF
}

docker_update() {
    projects="$(compose_projects)"
    while IFS='|' read -r project dir; do
        [ -n "$project" ] || continue
        compose "$project" "$dir" pull -q --ignore-buildable
        compose "$project" "$dir" up -d
    done <<EOF
$projects
EOF
    docker image prune -f
}

case "$mode" in
    check)
        if has apt-get; then apt_check; else echo "IGLO unsupported no apt-get"; fi
        if [ "$apt_only" != 1 ] && has docker; then docker_check; fi
        ;;
    update)
        if has apt-get; then apt_update; else echo "IGLO unsupported no apt-get"; fi
        if [ "$apt_only" != 1 ] && has docker; then docker_update; fi
        ;;
    *)
        echo "usage: guest.sh check|update" >&2
        exit 2
        ;;
esac

echo "IGLO done"
