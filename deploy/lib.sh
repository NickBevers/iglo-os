# Shared helpers for the installers. Source it, don't run it.
# shellcheck shell=bash

TEMPLATE_STORAGE="local"
# IGLO_VERSION is the release of these scripts. The Apps page compares it
# with the version in the description of a container.
IGLO_VERSION="$(cat "$(dirname "${BASH_SOURCE[0]}")/VERSION" 2>/dev/null || echo unknown)"

log() { printf '\033[1;32m==>\033[0m %s\n' "$*"; }

die() {
    printf '\033[1;31merror:\033[0m %s\n' "$*" >&2
    exit 1
}

# ask VAR "Question" [default] [secret]. Reads from the terminal, so it also
# works when the script itself comes from a pipe.
ask() {
    local var="$1" question="$2" default="${3:-}" secret="${4:-}" answer
    local prompt="$question"
    [[ -n "$default" ]] && prompt+=" [$default]"

    if [[ -n "$secret" ]]; then
        read -r -s -p "$prompt: " answer </dev/tty
        echo
    else
        read -r -p "$prompt: " answer </dev/tty
    fi

    printf -v "$var" '%s' "${answer:-$default}"
}

require_proxmox() {
    [[ $EUID -eq 0 ]] || die "run this as root on the Proxmox host"
    command -v pveversion >/dev/null || die "pveversion not found, this is not a Proxmox VE host"

    local major
    major="$(pveversion | sed -E 's#^pve-manager/([0-9]+).*#\1#')"
    ((major >= 9)) || die "Proxmox VE 9 or newer is required, found: $(pveversion)"

    [[ "$(dpkg --print-architecture)" == "amd64" ]] || die "only amd64 hosts are supported for now"
}

container_exists() {
    pct list | awk 'NR > 1 { print $NF }' | grep -qx "$1"
}

# Sets TEMPLATE to the newest Debian 13 template and downloads it if needed.
download_template() {
    log "Downloading Debian 13 template"
    pveam update >/dev/null

    TEMPLATE="$(pveam available --section system | awk '{ print $2 }' | grep -E '^debian-13-standard_.*_amd64\.tar\.zst$' | sort -V | tail -n1)"
    [[ -n "$TEMPLATE" ]] || die "no Debian 13 template found"

    if ! pveam list "$TEMPLATE_STORAGE" | grep -q "$TEMPLATE"; then
        pveam download "$TEMPLATE_STORAGE" "$TEMPLATE"
    fi
}

wait_for_network() {
    local ctid="$1"

    log "Waiting for network in container $ctid"
    for _ in $(seq 1 30); do
        pct exec "$ctid" -- getent hosts deb.debian.org >/dev/null 2>&1 && return
        sleep 2
    done
    die "container $ctid has no network"
}

container_ip() {
    pct exec "$1" -- hostname -I | awk '{ print $1 }'
}

# require_docker_lxc stops when the host can't run Docker in an unprivileged LXC.
require_docker_lxc() {
    # Older lxc-pve AppArmor profiles break Docker in unprivileged LXCs (Proxmox bug 7006).
    local lxc_version
    lxc_version="$(dpkg-query -W -f='${Version}' lxc-pve)"
    dpkg --compare-versions "$lxc_version" ge 6.0.5-2 ||
        die "lxc-pve $lxc_version is too old for Docker in LXC, update the host first (apt full-upgrade)"
}

# install_docker CTID installs Docker and its compose plugin in a container.
install_docker() {
    log "Installing Docker"
    # shellcheck disable=SC2016 # expands inside the container
    pct exec "$1" -- bash -euc '
        export DEBIAN_FRONTEND=noninteractive
        apt-get update -q
        apt-get install -y -q ca-certificates curl
        install -m 0755 -d /etc/apt/keyrings
        curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc
        chmod a+r /etc/apt/keyrings/docker.asc
        cat >/etc/apt/sources.list.d/docker.sources <<EOF
Types: deb
URIs: https://download.docker.com/linux/debian
Suites: $(. /etc/os-release && echo "$VERSION_CODENAME")
Components: stable
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/docker.asc
EOF
        apt-get update -q
        apt-get install -y -q docker-ce docker-ce-cli containerd.io docker-compose-plugin
    '
}

# app_description is the description of a new or updated app container. The
# manager reads the version line.
app_description() {
    printf '%s\niglo-version: %s\n' "$1" "$IGLO_VERSION"
}

# require_app_container CTID TAG stops unless the container is the app that
# Iglo-OS installed, with the tags iglo and TAG.
require_app_container() {
    local tags
    [[ "$1" =~ ^[1-9][0-9]{2,8}$ ]] || die "invalid container ID: $1"
    tags="$(pct config "$1" 2>/dev/null | sed -n 's/^tags: //p')"
    if [[ ";$tags;" != *";iglo;"* || ";$tags;" != *";$2;"* ]]; then
        die "container $1 is not the $2 app of Iglo-OS (it needs the tags iglo and $2)"
    fi
}

require_running() {
    [[ "$(pct status "$1")" == "status: running" ]] || die "container $1 is not running, start it first"
}

# upgrade_packages upgrades the Debian packages in a container.
upgrade_packages() {
    log "Updating the packages in container $1"
    pct exec "$1" -- bash -euc '
        export DEBIAN_FRONTEND=noninteractive
        apt-get update -q
        apt-get -y -q -o Dpkg::Options::=--force-confold dist-upgrade
        apt-get -y -q autoremove
    '
}

# remove_app CTID TAG removes the container of an app with its disks and
# snapshots. Its backups stay, so you can restore it on the Backups page.
remove_app() {
    require_app_container "$1" "$2"

    if [[ "$(pct status "$1")" == "status: running" ]]; then
        log "Shutting down container $1"
        pct shutdown "$1" --timeout 60 || pct stop "$1"
    fi
    log "Removing container $1 with its disks and snapshots. Its backups stay"
    pct destroy "$1" --purge 1
    log "Done"
}

# Unprivileged LXCs shift ids by 100000, and the apps of the media stack run as uid 1000.
HOST_CONTAINER_UID=101000

# media_folders sets MEDIA_FOLDERS to the folders of the Videos share: movies
# and series, and the anime folders when they are set. Without them, anime
# stays in the series and movies folders, like stacks from before them.
media_folders() {
    MEDIA_FOLDERS=("${MOVIES_FOLDER:-}" "${SERIES_FOLDER:-}")
    [[ -z "${ANIME_FOLDER:-}" ]] || MEDIA_FOLDERS+=("$ANIME_FOLDER")
    [[ -z "${ANIME_MOVIES_FOLDER:-}" ]] || MEDIA_FOLDERS+=("$ANIME_MOVIES_FOLDER")
}

# check_folders allows different folder names in the share, and no paths.
check_folders() {
    local folder pattern='^[A-Za-z0-9]([A-Za-z0-9 ._-]{0,62}[A-Za-z0-9_-])?$'
    local -A seen=()
    media_folders
    for folder in "${MEDIA_FOLDERS[@]}"; do
        if [[ ! "$folder" =~ $pattern || "$folder" == *..* ]]; then
            die "'$folder' is not a folder name, use something like movies, series or anime"
        fi
        [[ -z "${seen[${folder,,}]:-}" ]] || die "the media folders need different names, '$folder' is used twice"
        seen[${folder,,}]=1
    done
}
