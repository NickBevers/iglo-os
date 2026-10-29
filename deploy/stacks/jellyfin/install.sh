#!/usr/bin/env bash
# Installs Jellyfin in a new LXC on this Proxmox host, with the official
# Jellyfin repository, the Videos share from Storage and the GPU of the host.
# The Music share from Storage is optional.
#
#   ./install.sh [--storage local-lvm] [--bridge vmbr0] [--ctid 140] [--answers file]
#   ./install.sh --update --ctid 140
#   ./install.sh --remove --ctid 140
#
# With --answers, it asks nothing and reads the answers from a KEY=value file.
# The host agent uses this for the Apps page, and also --update and --remove.
# An update takes --answers too, with only the API key of Jellyfin, to add the
# libraries that are missing.
set -euo pipefail

STACK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=deploy/lib.sh
source "$STACK_DIR/../../lib.sh"
# shellcheck source=deploy/storage.sh
source "$STACK_DIR/../../storage.sh"

STORAGE="local-lvm"
BRIDGE="vmbr0"
CT_HOSTNAME="jellyfin"
CT_ID=""
ANSWERS=""
MODE="install"
VIDEOS_BINDING=""
MUSIC_BINDING=""
JELLYFIN_API_KEY=""
# HOME_VIDEOS_FOLDER is where Inbox publishes on the Videos share (cmd/iglo-inbox).
HOME_VIDEOS_FOLDER="home-videos"
# The Music app publishes songs and music videos in these folders of the
# Music share (internal/music).
MUSIC_AUDIO_FOLDER="audio"
MUSIC_VIDEOS_FOLDER="videos"
GPU=""
GPU_DEVICE="/dev/dri/renderD128"
# VAINFO_FILE keeps what the GPU decodes, for the codecs that Jellyfin turns on.
VAINFO_FILE=""
# HUC is y when the HuC firmware of the GPU runs, for the Intel low-power encoders.
HUC=""

while [[ $# -gt 0 ]]; do
    case "$1" in
        --storage) STORAGE="$2"; shift 2 ;;
        --bridge) BRIDGE="$2"; shift 2 ;;
        --ctid) CT_ID="$2"; shift 2 ;;
        --answers) ANSWERS="$2"; shift 2 ;;
        --update) MODE="update"; shift ;;
        --remove) MODE="remove"; shift ;;
        *) die "unknown option: $1" ;;
    esac
done

preflight() {
    require_proxmox
    [[ -x "$STACK_DIR/iglo-jellyfin" ]] || die "missing iglo-jellyfin next to install.sh"
    if container_exists "$CT_HOSTNAME"; then
        die "a container named '$CT_HOSTNAME' already exists"
    fi

    if [[ -e "$GPU_DEVICE" ]]; then
        GPU=1
    fi
}

# load_answers reads KEY=value lines. Values are only stored, never run.
load_answers() {
    local line key value
    while IFS= read -r line || [[ -n "$line" ]]; do
        [[ -z "$line" ]] && continue
        key="${line%%=*}" value="${line#*=}"
        case "$key" in
            VIDEOS_BINDING | MOVIES_FOLDER | SERIES_FOLDER | ANIME_FOLDER | ANIME_MOVIES_FOLDER | \
                MUSIC_BINDING | JELLYFIN_ADMIN_USERNAME | \
                JELLYFIN_ADMIN_PASSWORD | STORAGE)
                printf -v "$key" '%s' "$value"
                ;;
            *) die "unknown answer: $key" ;;
        esac
    done <"$ANSWERS"
    # It holds the admin password.
    rm -f "$ANSWERS"
}

# load_update_answers reads the answers of an update: only the API key that
# Iglo-OS has of this Jellyfin. The file goes at once, as it holds the key.
load_update_answers() {
    local line key value
    while IFS= read -r line || [[ -n "$line" ]]; do
        [[ -z "$line" ]] && continue
        key="${line%%=*}" value="${line#*=}"
        case "$key" in
            JELLYFIN_API_KEY) JELLYFIN_API_KEY="$value" ;;
            *) rm -f "$ANSWERS"; die "unknown answer: $key" ;;
        esac
    done <"$ANSWERS"
    rm -f "$ANSWERS"
    [[ "$JELLYFIN_API_KEY" =~ ^[a-f0-9]{32}$ ]] || die "the Jellyfin API key must be 32 characters (0-9 and a-f)"
}

ask_settings() {
    ANIME_FOLDER="" ANIME_MOVIES_FOLDER=""
    if [[ -n "$ANSWERS" ]]; then
        load_answers
    else
        log "A few questions first"
        echo "Connect the Videos share on the Storage page of Iglo-OS first."
        ask VIDEOS_BINDING "ID of the Videos share"
        ask MOVIES_FOLDER "Folder for movies in it" "movies"
        ask SERIES_FOLDER "Folder for series in it" "series"
        ask ANIME_FOLDER "Folder for anime series in it" "anime"
        ask ANIME_MOVIES_FOLDER "Folder for anime movies in it" "anime-movies"
        echo "A Music share from the Storage page gives Jellyfin the Music libraries."
        ask MUSIC_BINDING "ID of the Music share, empty for none" ""
        echo "The admin account of Jellyfin:"
        ask JELLYFIN_ADMIN_USERNAME "Username" "admin"
        ask JELLYFIN_ADMIN_PASSWORD "Password" "" secret
    fi

    check_folders
    if [[ -n "$MUSIC_BINDING" ]]; then
        [[ "$MUSIC_BINDING" != "$VIDEOS_BINDING" ]] || die "the Music share must be another share than the Videos share"
        require_binding "$MUSIC_BINDING" music ro
    fi
    # Last, so make_share_folders works on the Videos share.
    require_binding "$VIDEOS_BINDING" videos ro
    [[ -n "${JELLYFIN_ADMIN_USERNAME:-}" && -n "${JELLYFIN_ADMIN_PASSWORD:-}" ]] || die "the Jellyfin admin needs a username and a password"
}

create_container() {
    CT_ID="${CT_ID:-$(pvesh get /cluster/nextid)}"
    log "Creating container $CT_ID ($CT_HOSTNAME)"

    pct create "$CT_ID" "$TEMPLATE_STORAGE:vztmpl/$TEMPLATE" \
        --hostname "$CT_HOSTNAME" \
        --unprivileged 1 \
        --features nesting=1 \
        --cores 4 --memory 3072 --swap 512 \
        --rootfs "$STORAGE:16" \
        --net0 "name=eth0,bridge=$BRIDGE,ip=dhcp" \
        --onboot 1 \
        --tags "iglo;jellyfin" \
        --description "$(app_description "Jellyfin, installed by Iglo-OS")"

    CREATED_CT="$CT_ID"
    # Jellyfin only reads the media, and the shares mount before the container starts.
    bind_share "$CT_ID" 0 "$VIDEOS_BINDING" videos ro /data/media
    [[ -z "$MUSIC_BINDING" ]] || bind_share "$CT_ID" 1 "$MUSIC_BINDING" music ro /data/music
    pct start "$CT_ID"
    wait_for_network "$CT_ID"
}

# optional_folder says whether folder $1 is on the share of the last
# require_binding. It makes the folder when it is missing and the share has
# write access. Without it, Jellyfin gets no library for the folder.
optional_folder() {
    [[ ! -d "$BINDING_PATH/$1" ]] || return 0
    if [[ "$BINDING_READ_ONLY" == false ]]; then
        log "Making the folder $1 on the share"
        if mkdir -m 0775 "$BINDING_PATH/$1"; then
            return 0
        fi
    fi
    log "The share has no folder $1, so Jellyfin gets no library for it"
    return 1
}

# extra_libraries sets LIBRARY_FLAGS to the libraries of the home videos of
# Inbox on the Videos share, and of the Music app on the Music share, for the
# folders that are there.
extra_libraries() {
    LIBRARY_FLAGS=()
    require_binding "$VIDEOS_BINDING" videos ro
    if optional_folder "$HOME_VIDEOS_FOLDER"; then
        LIBRARY_FLAGS+=(--home-videos "/data/media/$HOME_VIDEOS_FOLDER")
    fi
    [[ -n "$MUSIC_BINDING" ]] || return 0
    require_binding "$MUSIC_BINDING" music ro
    if optional_folder "$MUSIC_AUDIO_FOLDER"; then
        LIBRARY_FLAGS+=(--music "/data/music/$MUSIC_AUDIO_FOLDER")
    fi
    if optional_folder "$MUSIC_VIDEOS_FOLDER"; then
        LIBRARY_FLAGS+=(--music-videos "/data/music/$MUSIC_VIDEOS_FOLDER")
    fi
}

# mounted_binding prints the share of Storage that container $CT_ID has at
# mount point $1, from the config of the container in $2. It prints nothing
# for no mount, or a mount that is not a share of Storage.
mounted_binding() {
    local source
    source="$(sed -nE "s#^mp[0-9]+: ([^,]+),(.*,)?mp=$1(,.*)?\$#\\1#p" <<<"$2")"
    [[ "$source" =~ ^/mnt/iglo/shares/([a-z][a-z0-9]{0,31})$ ]] && echo "${BASH_REMATCH[1]}"
    return 0
}

# connect_music gives an installed Jellyfin the Music share, read-only, when
# it has none and Storage has exactly one that is mounted. It sets
# MUSIC_BINDING to the share that Jellyfin has, and RESTART when Jellyfin must
# restart to see a new one.
connect_music() {
    local config conf id candidates=()
    config="$(pct config "$CT_ID")"
    VIDEOS_BINDING="$(mounted_binding /data/media "$config")"
    MUSIC_BINDING="$(mounted_binding /data/music "$config")"
    [[ -z "$MUSIC_BINDING" ]] || return 0
    if grep -qE '^mp[0-9]+: [^,]+,(.*,)?mp=/data/music(,|$)' <<<"$config"; then
        log "Jellyfin has other files at /data/music, so the update leaves the Music share out"
        return 0
    fi

    for conf in /etc/iglo/storage/*.conf; do
        [[ -f "$conf" ]] || continue
        id="$(basename "$conf" .conf)"
        grep -qx 'ROLE=music' "$conf" || continue
        # A share that is not mounted now is left out, without stopping the update.
        if (require_binding "$id" music ro) >/dev/null 2>&1; then
            candidates+=("$id")
        fi
    done
    case "${#candidates[@]}" in
        0) return 0 ;;
        1) ;;
        *)
            log "Storage has more than one Music share, so the update connects none. Reinstall Jellyfin to choose one"
            return 0
            ;;
    esac

    MUSIC_BINDING="${candidates[0]}"
    log "Giving Jellyfin the Music share $MUSIC_BINDING at /data/music, read-only"
    bind_share "$CT_ID" "$(free_mount_index "$CT_ID")" "$MUSIC_BINDING" music ro /data/music
    RESTART=y
}

# update_libraries adds the libraries of the Music share and the home videos
# that are missing. A failure only warns: the update itself worked.
update_libraries() {
    if [[ -z "$JELLYFIN_API_KEY" ]]; then
        log "Iglo-OS has no API key of this Jellyfin, so the update adds no libraries. Connect it on the Jellyfin page"
        return 0
    fi
    if [[ -z "$VIDEOS_BINDING" ]]; then
        log "Jellyfin has no Videos share of Storage at /data/media, so the update adds no libraries"
        return 0
    fi
    extra_libraries
    ((${#LIBRARY_FLAGS[@]} > 0)) || return 0
    # The key goes on stdin, so it is not in the process list.
    if ! printf '%s\n' "$JELLYFIN_API_KEY" | "$STACK_DIR/iglo-jellyfin" libraries \
        --url "http://$(container_ip "$CT_ID"):8096" "${LIBRARY_FLAGS[@]}"; then
        log "Warning: the libraries were not added. Add them in Jellyfin under Dashboard > Libraries"
    fi
}

# update upgrades Jellyfin and the other packages, and connects the Music
# share and its libraries when they are new. The setup of Jellyfin and its
# libraries stay.
update() {
    require_proxmox
    [[ -z "$ANSWERS" ]] || load_update_answers
    [[ -x "$STACK_DIR/iglo-jellyfin" ]] || die "missing iglo-jellyfin next to install.sh"
    require_app_container "$CT_ID" jellyfin
    require_running "$CT_ID"

    upgrade_packages "$CT_ID"

    RESTART=""
    connect_music
    if [[ -n "$RESTART" ]]; then
        log "Restarting Jellyfin, so it sees the Music share"
        pct reboot "$CT_ID"
        wait_for_network "$CT_ID"
    fi

    log "Waiting for Jellyfin"
    local url ready=""
    url="http://$(container_ip "$CT_ID"):8096/System/Info/Public"
    for _ in $(seq 1 40); do
        if curl -sf "$url" >/dev/null; then
            ready=y
            break
        fi
        sleep 3
    done
    [[ -n "$ready" ]] || die "Jellyfin does not answer after the update"

    update_libraries
    pct set "$CT_ID" --description "$(app_description "Jellyfin, installed by Iglo-OS")"
    log "Done"
}

# on_exit removes the container that a failed install from the Apps page made,
# so the next try can start clean. The files on the share stay.
on_exit() {
    local code=$?
    [[ -z "$VAINFO_FILE" ]] || rm -f "$VAINFO_FILE"
    if ((code == 0)) || [[ -z "$ANSWERS" ]]; then
        return
    fi
    if [[ -n "${CREATED_CT:-}" ]]; then
        log "The install failed, removing container $CREATED_CT"
        pct stop "$CREATED_CT" >/dev/null 2>&1 || true
        pct destroy "$CREATED_CT" --purge 1 || true
        remove_binding_dependencies "$CREATED_CT"
    fi
}

install_jellyfin() {
    log "Installing Jellyfin from repo.jellyfin.org"
    # shellcheck disable=SC2016 # expands inside the container
    pct exec "$CT_ID" -- bash -euc '
        export DEBIAN_FRONTEND=noninteractive
        apt-get update -q
        apt-get install -y -q ca-certificates curl gnupg
        install -d -m 0755 /etc/apt/keyrings
        curl -fsSL https://repo.jellyfin.org/jellyfin_team.gpg.key | gpg --dearmor --yes -o /etc/apt/keyrings/jellyfin.gpg
        cat >/etc/apt/sources.list.d/jellyfin.sources <<EOF
Types: deb
URIs: https://repo.jellyfin.org/debian
Suites: $(. /etc/os-release && echo "$VERSION_CODENAME")
Components: main
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/jellyfin.gpg
EOF
        apt-get update -q
        apt-get install -y -q jellyfin
    '
}

# setup_gpu passes the GPU of the host to the container, and checks that
# Jellyfin can use it. Without a working GPU, Jellyfin uses the CPU.
setup_gpu() {
    [[ -n "$GPU" ]] || return 0

    log "Passing the GPU ($GPU_DEVICE) to the container"
    local gid
    gid="$(pct exec "$CT_ID" -- bash -euc '
        getent group render >/dev/null || groupadd --system render
        usermod -aG render,video jellyfin
        getent group render | cut -d: -f3
    ')"
    pct set "$CT_ID" --dev0 "$GPU_DEVICE,gid=$gid"
    pct reboot "$CT_ID"
    wait_for_network "$CT_ID"

    VAINFO_FILE="$(mktemp)"
    if ! pct exec "$CT_ID" -- runuser -u jellyfin -- /usr/lib/jellyfin-ffmpeg/vainfo --display drm --device "$GPU_DEVICE" >"$VAINFO_FILE" 2>&1; then
        log "Jellyfin can't use the GPU, it transcodes on the CPU"
        GPU=""
    elif huc_running "$GPU_DEVICE"; then
        HUC=y
    fi
}

# huc_running says whether the HuC firmware of GPU $1 runs. The Intel
# low-power encoders need it for their bitrate control. Linux loads it by
# default on Alder Lake and newer (enable_guc=2 or 3, intel_uc.c), and
# writes "\tstatus: RUNNING" to huc_info once it is up (intel_uc_fw.c).
# Checked against Linux v6.14 and v7.0, the Proxmox kernel, on 28 October 2026.
huc_running() {
    local pci dir
    pci="$(basename "$(readlink -f "/sys/class/drm/$(basename "$1")/device")")"
    for dir in /sys/kernel/debug/dri/*; do
        # The name file says "i915 dev=0000:00:02.0 unique=...".
        grep -qs " dev=$pci\b" "$dir/name" || continue
        grep -qsx $'\tstatus: RUNNING' "$dir"/gt*/uc/huc_info && return 0
    done
    return 1
}

configure_jellyfin() {
    log "Setting up Jellyfin"
    local flags=()
    [[ -n "$GPU" ]] && flags+=(--gpu --vainfo "$VAINFO_FILE")
    [[ -n "$GPU" && "$HUC" == y ]] && flags+=(--huc)
    [[ -z "$ANIME_FOLDER" ]] || flags+=(--anime "/data/media/$ANIME_FOLDER")
    [[ -z "$ANIME_MOVIES_FOLDER" ]] || flags+=(--anime-movies "/data/media/$ANIME_MOVIES_FOLDER")
    extra_libraries
    flags+=("${LIBRARY_FLAGS[@]}")

    # The password goes on stdin, so it is not in the process list.
    printf '%s\n' "$JELLYFIN_ADMIN_PASSWORD" | "$STACK_DIR/iglo-jellyfin" setup \
        --url "http://$(container_ip "$CT_ID"):8096" \
        --admin "$JELLYFIN_ADMIN_USERNAME" \
        --movies "/data/media/$MOVIES_FOLDER" \
        --series "/data/media/$SERIES_FOLDER" \
        "${flags[@]}"
}

main() {
    case "$MODE" in
        update) update; return ;;
        remove)
            require_proxmox
            remove_app "$CT_ID" jellyfin
            remove_binding_dependencies "$CT_ID"
            return
            ;;
    esac

    trap on_exit EXIT
    preflight
    ask_settings
    media_folders
    make_share_folders "${MEDIA_FOLDERS[@]}"
    download_template
    create_container
    install_jellyfin
    setup_gpu
    configure_jellyfin

    local ip
    ip="$(container_ip "$CT_ID")"

    log "Done"
    # For the host agent, which shows the link on the Apps page.
    echo "IGLO app jellyfin $CT_ID $ip"
    cat <<EOF

  Jellyfin:  http://$ip:8096   <- log in as '$JELLYFIN_ADMIN_USERNAME'
  Hardware transcoding: $([[ -n "$GPU" ]] && echo "on (VA-API, $GPU_DEVICE$([[ "$HUC" == y ]] && echo ", HuC loaded"))" || echo "off, the CPU transcodes")

EOF
}

main
