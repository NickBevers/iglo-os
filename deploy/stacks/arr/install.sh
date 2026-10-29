#!/usr/bin/env bash
# Installs the media stack (Prowlarr, Radarr, Sonarr, Bazarr, qBittorrent,
# Seerr, Recyclarr, FlareSolverr) in a new LXC on this Proxmox host.
# qBittorrent, Prowlarr and FlareSolverr go through ProtonVPN or PIA via Gluetun.
#
#   ./install.sh [--storage local-lvm] [--bridge vmbr0] [--hostname media] [--ctid 130]
#                [--downloads-size 200] [--jellyfin-ctid 110] [--answers file]
#   ./install.sh --update --ctid 130
#   ./install.sh --remove --ctid 130
#   ./install.sh --vpn --ctid 130 --answers file    (VPN_PROVIDER, VPN_LOCATIONS, and the secrets below or empty to keep them)
#   ./install.sh --flaresolverr on --ctid 130        (or off: starts or stops FlareSolverr, and adds or removes its proxy in Prowlarr)
# The VPN answers are VPN_PROVIDER (protonvpn or pia), VPN_LOCATIONS (countries
# or regions, separated by commas), WIREGUARD_PRIVATE_KEY for ProtonVPN, and
# PIA_USERNAME, PIA_PASSWORD and the optional PIA_DEDICATED_TOKEN for PIA.
# With --answers, it asks nothing and reads the answers from a KEY=value file.
# The host agent uses this for the Apps page, and also --update and --remove.
set -euo pipefail

STACK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=deploy/lib.sh
source "$STACK_DIR/../../lib.sh"
# shellcheck source=deploy/storage.sh
source "$STACK_DIR/../../storage.sh"
# shellcheck source=deploy/secrets.sh
source "$STACK_DIR/../../secrets.sh"

STORAGE="local-lvm"
BRIDGE="vmbr0"
CT_HOSTNAME="media"
CT_ID=""
DOWNLOADS_SIZE=200
JELLYFIN_CTID=""
ANSWERS=""
MODE="install"
RESTART_JELLYFIN="y"
VIDEOS_BINDING=""
FLARESOLVERR="n"
FLARESOLVERR_SWITCH=""
ANIME_INDEXERS="y"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --storage) STORAGE="$2"; shift 2 ;;
        --bridge) BRIDGE="$2"; shift 2 ;;
        --hostname) CT_HOSTNAME="$2"; shift 2 ;;
        --ctid) CT_ID="$2"; shift 2 ;;
        --downloads-size) DOWNLOADS_SIZE="$2"; shift 2 ;;
        --jellyfin-ctid) JELLYFIN_CTID="$2"; shift 2 ;;
        --answers) ANSWERS="$2"; shift 2 ;;
        --update) MODE="update"; shift ;;
        --remove) MODE="remove"; shift ;;
        --vpn) MODE="vpn"; shift ;;
        --flaresolverr) MODE="flaresolverr"; FLARESOLVERR_SWITCH="${2:-}"; shift 2 ;;
        *) die "unknown option: $1" ;;
    esac
done

preflight() {
    require_proxmox
    require_docker_lxc

    for file in compose.yaml recyclarr.yml iglo-arr; do
        [[ -f "$STACK_DIR/$file" ]] || die "missing $file next to install.sh"
    done

    if container_exists "$CT_HOSTNAME"; then
        die "a container named '$CT_HOSTNAME' already exists"
    fi

    if [[ -z "$JELLYFIN_CTID" ]]; then
        JELLYFIN_CTID="$(pct list | awk 'NR > 1 && $NF == "jellyfin" { print $1 }')"
    fi
}

# load_answers reads KEY=value lines. Values are only stored, never run.
load_answers() {
    local line key value
    JELLYFIN_API_KEY="" JELLYFIN_ADMIN_USERNAME="" JELLYFIN_ADMIN_PASSWORD=""
    OPENSUBTITLES_USERNAME="" OPENSUBTITLES_PASSWORD=""
    ANIME_FOLDER="" ANIME_MOVIES_FOLDER="" VPN_PROVIDER="" VPN_LOCATIONS=""
    WIREGUARD_PRIVATE_KEY="" PIA_USERNAME="" PIA_PASSWORD="" PIA_DEDICATED_TOKEN=""
    while IFS= read -r line || [[ -n "$line" ]]; do
        [[ -z "$line" ]] && continue
        key="${line%%=*}" value="${line#*=}"
        case "$key" in
            VIDEOS_BINDING | MOVIES_FOLDER | SERIES_FOLDER | ANIME_FOLDER | ANIME_MOVIES_FOLDER | \
                VPN_PROVIDER | VPN_LOCATIONS | WIREGUARD_PRIVATE_KEY | PIA_USERNAME | PIA_PASSWORD | \
                PIA_DEDICATED_TOKEN | \
                SUBTITLE_LANGUAGES | ARR_USERNAME | ARR_PASSWORD | JELLYFIN_API_KEY | JELLYFIN_ADMIN_USERNAME | \
                JELLYFIN_ADMIN_PASSWORD | OPENSUBTITLES_USERNAME | OPENSUBTITLES_PASSWORD | RESTART_JELLYFIN | \
                STORAGE | DOWNLOADS_SIZE | FLARESOLVERR | ANIME_INDEXERS)
                printf -v "$key" '%s' "$value"
                ;;
            *) die "unknown answer: $key" ;;
        esac
    done <"$ANSWERS"
    # It holds the VPN key and the passwords.
    rm -f "$ANSWERS"

    ((${#ARR_PASSWORD} >= 12)) || die "password must be at least 12 characters"
    [[ "$FLARESOLVERR" == y || "$FLARESOLVERR" == n ]] || die "FLARESOLVERR must be y or n"
    [[ "$ANIME_INDEXERS" == y || "$ANIME_INDEXERS" == n ]] || die "ANIME_INDEXERS must be y or n"
    check_folders
    check_vpn
    JELLYFIN_URL=""
    if [[ -n "$JELLYFIN_CTID" ]]; then
        JELLYFIN_URL="http://$(container_ip "$JELLYFIN_CTID"):8096"
    else
        JELLYFIN_API_KEY="" JELLYFIN_ADMIN_USERNAME="" JELLYFIN_ADMIN_PASSWORD=""
    fi
}

ask_settings() {
    if [[ -n "$ANSWERS" ]]; then
        load_answers
        return
    fi

    log "A few questions first"

    echo "Connect the Videos share on the Storage page of Iglo-OS first, with write access."
    ask VIDEOS_BINDING "ID of the Videos share"
    echo "Movies, series, anime and anime movies each get a folder in it, and a library in Jellyfin."
    ask MOVIES_FOLDER "Folder for movies" "movies"
    ask SERIES_FOLDER "Folder for series" "series"
    ask ANIME_FOLDER "Folder for anime series" "anime"
    ask ANIME_MOVIES_FOLDER "Folder for anime movies" "anime-movies"
    check_folders

    WIREGUARD_PRIVATE_KEY="" PIA_USERNAME="" PIA_PASSWORD="" PIA_DEDICATED_TOKEN=""
    ask VPN_PROVIDER "VPN provider (protonvpn or pia)" "protonvpn"
    if [[ "$VPN_PROVIDER" == pia ]]; then
        ask PIA_USERNAME "PIA username (like p1234567)"
        ask PIA_PASSWORD "PIA password" "" secret
        echo "Optional: a PIA dedicated IP token (DIP...). Then answer 'Dedicated IP' as the region."
        ask PIA_DEDICATED_TOKEN "PIA dedicated IP token (empty for none)" "" secret
        ask VPN_LOCATIONS "PIA regions (comma separated)" "Netherlands"
    else
        echo "Get a WireGuard key at https://account.proton.me/u/0/vpn/WireGuard"
        echo "(turn on 'NAT-PMP (Port Forwarding)' when you create it)."
        ask WIREGUARD_PRIVATE_KEY "ProtonVPN WireGuard private key" "" secret
        ask VPN_LOCATIONS "ProtonVPN countries (comma separated)" "Netherlands"
    fi
    check_vpn

    echo "FlareSolverr runs a browser for indexers behind Cloudflare. It can take up to 1 GB."
    echo "You can turn it on or off later on the Apps page."
    ask FLARESOLVERR "Run FlareSolverr? (y/n)" "n"
    ask ANIME_INDEXERS "Add the anime trackers Nyaa.si and Anime Tosho to Prowlarr? (y/n)" "y"
    ask SUBTITLE_LANGUAGES "Subtitle languages (2-letter codes, comma separated)" "en"
    echo "Optional: an OpenSubtitles.com account finds many more subtitles (free at opensubtitles.com)."
    ask OPENSUBTITLES_USERNAME "OpenSubtitles.com username (empty to skip)"
    OPENSUBTITLES_PASSWORD=""
    if [[ -n "$OPENSUBTITLES_USERNAME" ]]; then
        ask OPENSUBTITLES_PASSWORD "OpenSubtitles.com password" "" secret
    fi

    echo "One login for Radarr, Sonarr, Prowlarr, Bazarr and qBittorrent:"
    ask ARR_USERNAME "Username" "iglo"
    ask ARR_PASSWORD "Password (min 12 characters)" "" secret
    ((${#ARR_PASSWORD} >= 12)) || die "password must be at least 12 characters"

    JELLYFIN_URL=""
    JELLYFIN_API_KEY="" JELLYFIN_ADMIN_USERNAME="" JELLYFIN_ADMIN_PASSWORD=""
    if [[ -n "$JELLYFIN_CTID" ]]; then
        JELLYFIN_URL="http://$(container_ip "$JELLYFIN_CTID"):8096"
        echo "Found Jellyfin in container $JELLYFIN_CTID ($JELLYFIN_URL)."
        echo "Create an API key in Jellyfin: Dashboard > API Keys > +. Leave empty to skip."
        ask JELLYFIN_API_KEY "Jellyfin API key" "" secret
        echo "With a Jellyfin admin, the installer also sets up Seerr. You then log in to Seerr with that account."
        ask JELLYFIN_ADMIN_USERNAME "Jellyfin admin username (empty to skip)"
        if [[ -n "$JELLYFIN_ADMIN_USERNAME" ]]; then
            ask JELLYFIN_ADMIN_PASSWORD "Jellyfin admin password" "" secret
        fi
    fi
}

# prepare_videos checks the Videos share and makes the media folders. The
# apps of the stack write there, so the share needs write access.
prepare_videos() {
    require_binding "$VIDEOS_BINDING" videos rw
    media_folders
    make_share_folders "${MEDIA_FOLDERS[@]}"
    check_writable
}

# check_writable makes sure that the containers can write in the media
# folders. The NAS can still refuse the SMB user.
check_writable() {
    local folder dir
    for folder in "${MEDIA_FOLDERS[@]}"; do
        dir="$BINDING_PATH/$folder"
        if ! setpriv --reuid="$HOST_CONTAINER_UID" --regid="$HOST_CONTAINER_UID" --clear-groups touch "$dir/.iglo-write-test" 2>/dev/null; then
            die "containers can't write to $folder on the Videos share. On the NAS, give the SMB user of that share write access"
        fi
        rm -f "$dir/.iglo-write-test"
    done
}

create_container() {
    CT_ID="${CT_ID:-$(pvesh get /cluster/nextid)}"
    log "Creating container $CT_ID ($CT_HOSTNAME)"

    pct create "$CT_ID" "$TEMPLATE_STORAGE:vztmpl/$TEMPLATE" \
        --hostname "$CT_HOSTNAME" \
        --unprivileged 1 \
        --features nesting=1,keyctl=1 \
        --cores 4 --memory 4096 --swap 1024 \
        --rootfs "$STORAGE:16" \
        --net0 "name=eth0,bridge=$BRIDGE,ip=dhcp" \
        --onboot 1 \
        --dev0 /dev/net/tun \
        --mp1 "$STORAGE:$DOWNLOADS_SIZE,mp=/data/downloads,backup=0" \
        --tags "iglo;media" \
        --description "$(app_description "Iglo-OS media stack")"

    CREATED_CT="$CT_ID"
    bind_share "$CT_ID" 0 "$VIDEOS_BINDING" videos rw /data/media
    secrets_attach "$CT_ID" 2
    pct start "$CT_ID"
    wait_for_network "$CT_ID"
}

# on_exit removes the container and the mount that a failed install from the
# Apps page made, so the next try can start clean.
on_exit() {
    local code=$?
    if ((code == 0)) || [[ -z "$ANSWERS" ]]; then
        return
    fi
    if [[ -n "${CREATED_CT:-}" ]]; then
        log "The install failed, removing container $CREATED_CT"
        pct stop "$CREATED_CT" >/dev/null 2>&1 || true
        pct destroy "$CREATED_CT" --purge 1 || true
        remove_binding_dependencies "$CREATED_CT"
        secrets_remove "$CREATED_CT" || true
    fi
}

push_stack() {
    log "Copying the stack into the container"
    pct exec "$CT_ID" -- bash -euc '
        install -d -m 0755 /opt/arr /opt/arr/config
        # Seerr and Recyclarr run as uid 1000 and do not fix their own folders.
        install -d -m 0755 -o 1000 -g 1000 /opt/arr/config/seerr /opt/arr/config/recyclarr
    '
    push_stack_files

    local env_file
    env_file="$(mktemp)"
    trap 'rm -f "$env_file"; trap - RETURN' RETURN

    cat >"$env_file" <<EOF
TZ=$(timedatectl show -p Timezone --value)
DATA_DIR=/data
ARR_USERNAME=$ARR_USERNAME
RADARR_API_KEY=$(openssl rand -hex 16)
SONARR_API_KEY=$(openssl rand -hex 16)
PROWLARR_API_KEY=$(openssl rand -hex 16)
SUBTITLE_LANGUAGES=$SUBTITLE_LANGUAGES
MOVIES_FOLDER=$MOVIES_FOLDER
SERIES_FOLDER=$SERIES_FOLDER
ANIME_FOLDER=$ANIME_FOLDER
ANIME_MOVIES_FOLDER=$ANIME_MOVIES_FOLDER
FLARESOLVERR=$FLARESOLVERR
ANIME_INDEXERS=$ANIME_INDEXERS
COMPOSE_PROFILES=$([[ "$FLARESOLVERR" == y ]] && echo flaresolverr)
JELLYFIN_URL=$JELLYFIN_URL
JELLYFIN_API_KEY=$JELLYFIN_API_KEY
JELLYFIN_ADMIN_USERNAME=$JELLYFIN_ADMIN_USERNAME
OPENSUBTITLES_USERNAME=$OPENSUBTITLES_USERNAME
EOF
    pct push "$CT_ID" "$env_file" /opt/arr/.env --perms 0600
    apply_vpn
}

# check_vpn checks the VPN answers before anything is made. iglo-arr checks
# them again against the list of locations.
check_vpn() {
    [[ "$VPN_PROVIDER" == protonvpn || "$VPN_PROVIDER" == pia ]] || die "VPN_PROVIDER must be protonvpn or pia"
    # The length apart: POSIX allows regex repeat counts up to 255 only.
    local pattern="^[A-Za-z0-9 '.,()-]+\$"
    [[ "$VPN_LOCATIONS" =~ $pattern && ${#VPN_LOCATIONS} -le 600 ]] || die "give the VPN locations, separated by commas"
    VPN_LOCATIONS="$(sed -E 's/ *, */,/g; s/^ +//; s/ +$//' <<<"$VPN_LOCATIONS")"
}

# vpn_secret_names are the VPN secrets that the host keeps sealed, by answer.
VPN_SECRETS=(WIREGUARD_PRIVATE_KEY PIA_USERNAME PIA_PASSWORD PIA_DEDICATED_TOKEN)

# apply_vpn has iglo-arr check the VPN answers and write them in .env without
# the secrets. The secrets go on stdin, and come back as the files that
# Gluetun reads, which the host seals (deploy/secrets.sh). An empty secret
# keeps the sealed one. Nothing goes through a file or the process list.
apply_vpn() {
    log "Saving the VPN settings"
    local name output line value
    for name in "${VPN_SECRETS[@]}"; do
        [[ -n "${!name}" ]] || printf -v "$name" '%s' "$(secret_get "$CT_ID" keep "${name,,}")"
    done

    output="$(printf 'VPN_PROVIDER=%s\nVPN_LOCATIONS=%s\nWIREGUARD_PRIVATE_KEY=%s\nPIA_USERNAME=%s\nPIA_PASSWORD=%s\nPIA_DEDICATED_TOKEN=%s\n' \
        "$VPN_PROVIDER" "$VPN_LOCATIONS" "$WIREGUARD_PRIVATE_KEY" "$PIA_USERNAME" "$PIA_PASSWORD" "$PIA_DEDICATED_TOKEN" |
        pct exec "$CT_ID" -- /usr/local/bin/iglo-arr vpn)" || return 1

    # Process substitution is a pipe; a here-string can be a file on disk.
    while IFS= read -r line; do
        name="${line%%=*}" value="${line#*=}"
        case "$name" in
            wireguard_private_key | openvpn_user | openvpn_password | vpn_port_forwarding_username | vpn_port_forwarding_password)
                printf '%s' "$value" | secret_put "$CT_ID" run "$name"
                # A key that iglo-arr took from an old .env: keep it too.
                if [[ "$name" == wireguard_private_key && -z "$WIREGUARD_PRIVATE_KEY" ]]; then
                    WIREGUARD_PRIVATE_KEY="$value"
                fi
                ;;
            *) return 1 ;;
        esac
    done < <(printf '%s\n' "$output")
    # The answers too, so the next change can keep them.
    for name in "${VPN_SECRETS[@]}"; do
        printf '%s' "${!name}" | secret_put "$CT_ID" keep "${name,,}"
    done
    secrets_unlock "$CT_ID"
}

# attach_secrets gives a stack from before the sealed secrets its secrets
# folder. The container restarts to see it.
attach_secrets() {
    secrets_attached "$CT_ID" && return 0
    log "Giving the container its folder for sealed secrets, it restarts"
    secrets_attach "$CT_ID" "$(free_mount_index "$CT_ID")"
    pct reboot "$CT_ID"
    wait_for_network "$CT_ID"
}

# migrate_vpn_secrets takes the VPN key of a stack from before the sealed
# secrets out of .env, and seals it on the host.
migrate_vpn_secrets() {
    pct exec "$CT_ID" -- grep -q '^WIREGUARD_PRIVATE_KEY=' /opt/arr/.env || return 0
    log "Moving the VPN key out of the container, sealed on the host"
    VPN_PROVIDER=protonvpn WIREGUARD_PRIVATE_KEY="" PIA_USERNAME="" PIA_PASSWORD="" PIA_DEDICATED_TOKEN=""
    VPN_LOCATIONS="$(pct exec "$CT_ID" -- sed -n 's/^VPN_COUNTRIES=//p' /opt/arr/.env)"
    # Old stacks took countries as free text. When iglo-arr refuses them, the
    # key stays in .env until the next VPN change from the Apps page.
    apply_vpn || log "Could not move the VPN key: choose the countries again under VPN on the Apps page"
}

# push_stack_files copies the files of this release into the container.
push_stack_files() {
    pct push "$CT_ID" "$STACK_DIR/compose.yaml" /opt/arr/compose.yaml --perms 0644
    pct push "$CT_ID" "$STACK_DIR/recyclarr.yml" /opt/arr/config/recyclarr/recyclarr.yml --perms 0644 --user 1000 --group 1000
    pct push "$CT_ID" "$STACK_DIR/iglo-arr" /usr/local/bin/iglo-arr --perms 0755
}

start_stack() {
    log "Starting the stack (the first image download takes a few minutes)"
    # qBittorrent and Prowlarr wait for a healthy VPN, so a VPN problem can already fail here.
    if ! pct exec "$CT_ID" -- docker compose --project-directory /opt/arr up -d --remove-orphans --quiet-pull; then
        vpn_failed
    fi
    wait_for_vpn
}

wait_for_vpn() {
    vpn_healthy || vpn_failed
}

vpn_healthy() {
    log "Waiting for the VPN"
    for _ in $(seq 1 60); do
        if [[ "$(pct exec "$CT_ID" -- docker inspect -f '{{.State.Health.Status}}' gluetun)" == "healthy" ]]; then
            return 0
        fi
        sleep 3
    done

    return 1
}

show_vpn_log() {
    echo "Last lines of the VPN log (gluetun):" >&2
    pct exec "$CT_ID" -- docker logs --tail 50 gluetun >&2 || true
}

# vpn_failed shows the end of the VPN log, because the container may be removed after this.
vpn_failed() {
    show_vpn_log
    die "the VPN did not connect. Check the VPN login and the locations, and read the VPN log above"
}

share_media_with_jellyfin() {
    [[ -n "$JELLYFIN_CTID" ]] || return 0

    # Jellyfin from Iglo-OS already reads the same share at /data/media.
    local current
    current="$(pct config "$JELLYFIN_CTID" | sed -nE 's#^mp[0-9]+: ([^,]+),(.*,)?mp=/data/media(,.*)?$#\1#p')"
    if [[ "$current" == "$BINDING_PATH" ]]; then
        return 0
    fi
    [[ -z "$current" ]] || die "Jellyfin (container $JELLYFIN_CTID) already has other media at /data/media ($current). Use its Videos share"

    log "Sharing the Videos share with Jellyfin (container $JELLYFIN_CTID) as /data/media"
    bind_share "$JELLYFIN_CTID" "$(free_mount_index "$JELLYFIN_CTID")" "$VIDEOS_BINDING" videos ro /data/media

    if [[ -z "$ANSWERS" ]]; then
        ask RESTART_JELLYFIN "Jellyfin must restart to see the folder. Restart it now? (y/n)" "y"
    fi
    if [[ "$RESTART_JELLYFIN" == "y" ]]; then
        pct reboot "$JELLYFIN_CTID"
        for _ in $(seq 1 40); do
            curl -sf "$JELLYFIN_URL/System/Info/Public" >/dev/null && return 0
            sleep 3
        done
        die "Jellyfin did not come back after the restart"
    fi

    # Libraries need the folder, so skip them until Jellyfin has restarted.
    JELLYFIN_API_KEY=""
}

configure_stack() {
    log "Connecting the apps"
    # The full path, because pct sets PATH to /sbin:/bin:/usr/sbin:/usr/bin inside the container.
    # The passwords go on stdin, so they never end up in a file in the container.
    printf '%s\n%s\n%s\n' "$ARR_PASSWORD" "$JELLYFIN_ADMIN_PASSWORD" "$OPENSUBTITLES_PASSWORD" |
        pct exec "$CT_ID" -- /usr/local/bin/iglo-arr configure
}

# update upgrades the packages, copies the stack of this release, and starts
# the new images. The settings in the apps stay: they need no passwords.
update() {
    require_proxmox
    for file in compose.yaml recyclarr.yml iglo-arr; do
        [[ -f "$STACK_DIR/$file" ]] || die "missing $file next to install.sh"
    done
    require_app_container "$CT_ID" media
    require_running "$CT_ID"

    upgrade_packages "$CT_ID"

    log "Copying the stack of Iglo-OS $IGLO_VERSION into the container"
    push_stack_files
    attach_secrets
    migrate_vpn_secrets
    # A stack from before FlareSolverr was optional keeps running it.
    pct exec "$CT_ID" -- bash -euc '
        grep -q "^FLARESOLVERR=" /opt/arr/.env || printf "FLARESOLVERR=y\nCOMPOSE_PROFILES=flaresolverr\n" >> /opt/arr/.env
    '

    log "Downloading the new images"
    pct exec "$CT_ID" -- docker compose --project-directory /opt/arr pull --quiet
    start_stack
    pct exec "$CT_ID" -- docker image prune -f >/dev/null

    pct set "$CT_ID" --description "$(app_description "Iglo-OS media stack")"
    log "Done"
}

# restart_vpn recreates Gluetun and the apps that share its network
# (see compose.yaml), so they all use the new VPN connection.
restart_vpn() {
    log "Restarting the VPN and the apps behind it"
    # FlareSolverr only when its profile is on: naming it would start it anyway.
    pct exec "$CT_ID" -- bash -euc '
        cd /opt/arr
        services="gluetun qbittorrent prowlarr $(docker compose config --services | grep -x flaresolverr || true)"
        docker compose up -d --force-recreate $services
    ' && vpn_healthy
}

# change_vpn writes the new VPN settings. When they can't be saved or the VPN
# does not connect with them, it puts the old .env, dedicated IP file and
# sealed secrets back.
change_vpn() {
    require_proxmox
    require_app_container "$CT_ID" media
    require_running "$CT_ID"

    local line key value
    VPN_PROVIDER="" VPN_LOCATIONS="" WIREGUARD_PRIVATE_KEY="" PIA_USERNAME="" PIA_PASSWORD="" PIA_DEDICATED_TOKEN=""
    while IFS= read -r line || [[ -n "$line" ]]; do
        [[ -z "$line" ]] && continue
        key="${line%%=*}" value="${line#*=}"
        case "$key" in
            VPN_PROVIDER | VPN_LOCATIONS | WIREGUARD_PRIVATE_KEY | PIA_USERNAME | PIA_PASSWORD | PIA_DEDICATED_TOKEN)
                printf -v "$key" '%s' "$value"
                ;;
            *) die "unknown answer: $key" ;;
        esac
    done <"$ANSWERS"
    # It holds the VPN secrets.
    rm -f "$ANSWERS"
    check_vpn

    # This release's iglo-arr and compose.yaml know the providers and the
    # sealed secrets; a stack from before has neither.
    push_stack_files
    attach_secrets

    secrets_save "$CT_ID"
    pct exec "$CT_ID" -- bash -c 'cp -p /opt/arr/.env /opt/arr/.env.before-vpn
        f=/opt/arr/config/gluetun/pia-dedicated.ovpn
        if [[ -f "$f" ]]; then cp -p "$f" "$f.before-vpn"; else rm -f "$f.before-vpn"; fi'
    if ! apply_vpn; then
        put_back_vpn
        die "the VPN settings were not saved, read the error above"
    fi

    if restart_vpn; then
        secrets_forget_saved "$CT_ID"
        pct exec "$CT_ID" -- rm -f /opt/arr/.env.before-vpn /opt/arr/config/gluetun/pia-dedicated.ovpn.before-vpn
        log "Done"
        return 0
    fi

    show_vpn_log
    log "The VPN did not connect, going back to the old settings"
    put_back_vpn
    restart_vpn || log "The VPN does not connect with the old settings either"
    die "the VPN did not connect with the new settings, so the old settings are back. Check the VPN login and the locations, and read the VPN log above"
}

# put_back_vpn restores what change_vpn saved.
put_back_vpn() {
    secrets_restore "$CT_ID"
    pct exec "$CT_ID" -- bash -c 'mv -f /opt/arr/.env.before-vpn /opt/arr/.env
        f=/opt/arr/config/gluetun/pia-dedicated.ovpn
        if [[ -f "$f.before-vpn" ]]; then mv -f "$f.before-vpn" "$f"; else rm -f "$f"; fi'
}

# change_flaresolverr turns FlareSolverr on or off in a stack that runs,
# without passwords: iglo-arr uses the Prowlarr API key in .env. On starts
# the service, then adds its proxy to Prowlarr. Off removes the proxy, then
# stops the service. Indexers keep the flaresolverr tag either way.
change_flaresolverr() {
    [[ "$FLARESOLVERR_SWITCH" == on || "$FLARESOLVERR_SWITCH" == off ]] || die "--flaresolverr takes on or off"
    require_proxmox
    for file in compose.yaml recyclarr.yml iglo-arr; do
        [[ -f "$STACK_DIR/$file" ]] || die "missing $file next to install.sh"
    done
    require_app_container "$CT_ID" media
    require_running "$CT_ID"

    # This release's iglo-arr knows the flaresolverr command, and its
    # compose.yaml has the profile. The other services keep running as they are.
    push_stack_files
    local before
    before="$(pct exec "$CT_ID" -- sed -n 's/^FLARESOLVERR=//p' /opt/arr/.env)"

    if [[ "$FLARESOLVERR_SWITCH" == on ]]; then
        log "Starting FlareSolverr"
        # Naming the service starts it without its profile. --no-deps
        # leaves Gluetun and the apps behind it alone.
        pct exec "$CT_ID" -- docker compose --project-directory /opt/arr up -d --no-deps --quiet-pull flaresolverr
        if ! pct exec "$CT_ID" -- /usr/local/bin/iglo-arr flaresolverr on; then
            # A stack from before the switch has no answer, and runs it.
            if [[ "$before" == n ]]; then
                pct exec "$CT_ID" -- docker compose --project-directory /opt/arr rm -s -f flaresolverr || true
            fi
            die "Prowlarr did not take FlareSolverr, read the error above"
        fi
    else
        pct exec "$CT_ID" -- /usr/local/bin/iglo-arr flaresolverr off ||
            die "could not remove FlareSolverr from Prowlarr, so it keeps running. Read the error above"
        log "Stopping FlareSolverr"
        pct exec "$CT_ID" -- docker compose --project-directory /opt/arr rm -s -f flaresolverr
    fi
    log "Done"
}

main() {
    case "$MODE" in
        vpn) change_vpn; return ;;
        flaresolverr) change_flaresolverr; return ;;
        update) update; return ;;
        remove)
            require_proxmox
            remove_app "$CT_ID" media
            remove_binding_dependencies "$CT_ID"
            secrets_remove "$CT_ID"
            return
            ;;
    esac

    trap on_exit EXIT
    preflight
    ask_settings
    prepare_videos
    download_template
    create_container
    install_docker "$CT_ID"
    share_media_with_jellyfin
    push_stack
    start_stack
    configure_stack

    local ip cloudflare="Indexers behind Cloudflare: give them the 'flaresolverr' tag in Prowlarr."
    ip="$(container_ip "$CT_ID")"
    if [[ "$FLARESOLVERR" != y ]]; then
        cloudflare="Indexers behind Cloudflare need FlareSolverr. Turn it on from the Apps page."
    fi

    log "Done"
    # For the host agent, which shows the links on the Apps page.
    echo "IGLO app media $CT_ID $ip"
    cat <<EOF

  Requests (Seerr):  http://$ip:5055   <- finish its setup now, until then anyone on your network can
  Radarr (movies):   http://$ip:7878
  Sonarr (TV):       http://$ip:8989
  Prowlarr:          http://$ip:9696   <- add your indexers here, they sync to Radarr and Sonarr
  Bazarr:            http://$ip:6767
  qBittorrent:       http://$ip:8080

  Log in with '$ARR_USERNAME' and your password.
  $cloudflare

EOF
}

main
