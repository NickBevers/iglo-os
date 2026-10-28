#!/usr/bin/env bash
set -euo pipefail
umask 077
MODE="${1:-}"
[[ "$MODE" == install || "$MODE" == update ]] || exit 1
STACK=/opt/headscale
HEADSCALE_VERSION=0.29.6
CADDY_VERSION=2.11.7
# Official release SHA256 digests checked 28 October 2026.
HEADSCALE_SHA256=7477c374949eea6fb7cf3cbff14dbef49e4d118d9a33557d6764a77ece617e53
CADDY_SHA256=727b91701a392de6ebc5027509f548bf39979e5216340d0faed8fa5e69c84f8b
HEADSCALE_HOSTNAME=""
WORK="$(mktemp -d)"
BACKUP="" STOPPED=false BACKUP_COMPLETE=false
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT
rollback() {
    trap - ERR
    set +e
    if [[ "$MODE" == update && "$STOPPED" == true ]]; then
        if ! systemctl stop caddy headscale; then
            echo "error: Controller update failed and services could not be stopped safely for recovery." >&2
            exit 1
        fi
        RECOVERY_OK=true
        restore_step() { "$@" || RECOVERY_OK=false; }
        if [[ "$BACKUP_COMPLETE" == true ]]; then
            restore_step cp -a "$BACKUP/bin/"* /usr/local/bin/
            restore_step cp -a "$BACKUP/config/headscale/." /etc/headscale/
            restore_step cp -a "$BACKUP/config/caddy/." /etc/caddy/
            restore_step cp -a "$BACKUP/config/units/"* /etc/systemd/system/
            restore_step cp -a "$BACKUP/config/nftables.conf" /etc/nftables.conf
            restore_step cp -a "$BACKUP/config/version" /etc/iglo-headscale/version
            restore_step rm -rf /var/lib/headscale /var/lib/caddy
            restore_step cp -a "$BACKUP/state/headscale" /var/lib/headscale
            restore_step cp -a "$BACKUP/state/caddy" /var/lib/caddy
            restore_step systemctl daemon-reload
            restore_step nft -f /etc/nftables.conf
        fi
        restore_step systemctl start headscale caddy
        if [[ "$RECOVERY_OK" == true ]] && curl --fail --silent --retry 5 --retry-connrefused --retry-delay 1 --max-time 3 http://127.0.0.1:8080/health >/dev/null && systemctl is-active --quiet caddy; then
            echo "error: Controller update failed. The previous controller was restored and local health passed." >&2
            exit 4
        fi
        echo "error: Controller update failed. Local recovery could not be verified; inspect the saved recovery point." >&2
        exit 1
    elif [[ "$MODE" == update ]]; then
        echo "error: Controller update stopped before replacing controller binaries or application state." >&2
        exit 3
    fi
    echo "error: Controller installation failed." >&2
    exit 1
}
trap rollback ERR

# Refuse unsupported versions before package work or opening application state.
if [[ "$MODE" == update ]]; then
    # Only reviewed source versions may open the database with this binary.
    # Same-version refreshes are supported; future upgrades need a reviewed allowlist.
    [[ "$(cat /etc/iglo-headscale/version)" == "$HEADSCALE_VERSION" ]] || {
        echo "error: This source Headscale version has no reviewed upgrade path. Nothing was replaced." >&2
        exit 3
    }
fi
export DEBIAN_FRONTEND=noninteractive
apt-get update -q
apt-get install -y -q ca-certificates curl tar python3 sqlite3 nftables
python3 "$STACK/validate.py" answers /etc/iglo-headscale.conf
while IFS='=' read -r key value; do
    if [[ "$key" == HEADSCALE_HOSTNAME ]]; then HEADSCALE_HOSTNAME="$value"; fi
done < /etc/iglo-headscale.conf
[[ -n "$HEADSCALE_HOSTNAME" ]]
if [[ "$MODE" == update ]]; then
    for path in /var/lib/headscale /var/lib/caddy; do
        [[ -d "$path" && ! -L "$path" ]]
        if mountpoint -q "$path"; then echo "error: Controller state must not be a separate mount" >&2;exit 3;fi
    done
fi
# Fetch and verify while the old controller still runs, before any downtime.
curl --fail --silent --show-error --location --retry 3 --connect-timeout 15 --max-time 300 --max-filesize 104857600 \
    "https://github.com/juanfont/headscale/releases/download/v$HEADSCALE_VERSION/headscale_${HEADSCALE_VERSION}_linux_amd64" -o "$WORK/headscale"
printf '%s  %s\n' "$HEADSCALE_SHA256" "$WORK/headscale" | sha256sum --check --status
curl --fail --silent --show-error --location --retry 3 --connect-timeout 15 --max-time 300 --max-filesize 104857600 \
    "https://github.com/caddyserver/caddy/releases/download/v$CADDY_VERSION/caddy_${CADDY_VERSION}_linux_amd64.tar.gz" -o "$WORK/caddy.tar.gz"
printf '%s  %s\n' "$CADDY_SHA256" "$WORK/caddy.tar.gz" | sha256sum --check --status
tar -xzf "$WORK/caddy.tar.gz" -C "$WORK" --no-same-owner caddy
[[ -f "$WORK/caddy" && ! -L "$WORK/caddy" ]]

if [[ "$MODE" == update ]]; then
    size=$(du -sb /var/lib/headscale /var/lib/caddy | awk '{sum+=$1} END {print sum}')
    available=$(df -B1 --output=avail /var/lib/headscale | tail -n 1)
    ((available > size * 2 + 314572800))
    install -d -m 0700 /var/backups/iglo-headscale
    BACKUP=$(mktemp -d "/var/backups/iglo-headscale/$(date -u +%Y%m%dT%H%M%SZ)-XXXXXX")
    STOPPED=true
    systemctl stop caddy headscale
    install -d -m 0700 "$BACKUP/bin" "$BACKUP/config/units" "$BACKUP/state"
    cp -a /usr/local/bin/headscale /usr/local/bin/caddy "$BACKUP/bin/"
    cp -a /etc/headscale /etc/caddy "$BACKUP/config/"
    cp -a /etc/systemd/system/headscale.service /etc/systemd/system/caddy.service "$BACKUP/config/units/"
    cp -a /etc/nftables.conf /etc/iglo-headscale/version "$BACKUP/config/"
    cp -a /var/lib/headscale /var/lib/caddy "$BACKUP/state/"
    # SQLite's backup API includes committed WAL pages. Remove copied WAL/SHM
    # sidecars so they can never override the consistent database copy.
    checked=$(runuser -u headscale -- mktemp /var/lib/headscale/.iglo-backup-XXXXXX)
    runuser -u headscale -- sqlite3 /var/lib/headscale/db.sqlite ".backup '$checked'"
    [[ "$(runuser -u headscale -- sqlite3 "$checked" 'PRAGMA integrity_check')" == ok ]]
    cp -a "$checked" "$BACKUP/state/headscale/db.sqlite"
    rm -f "$checked"
    rm -f "$BACKUP/state/headscale/db.sqlite-wal" "$BACKUP/state/headscale/db.sqlite-shm"
    chown --reference=/var/lib/headscale/db.sqlite "$BACKUP/state/headscale/db.sqlite"
    chmod 0600 "$BACKUP/state/headscale/db.sqlite"
    BACKUP_COMPLETE=true
else
    id headscale >/dev/null 2>&1 || useradd --system --user-group --home-dir /var/lib/headscale --shell /usr/sbin/nologin headscale
    id caddy >/dev/null 2>&1 || useradd --system --user-group --home-dir /var/lib/caddy --shell /usr/sbin/nologin caddy
    install -d -m 0700 -o headscale -g headscale /var/lib/headscale
    install -d -m 0700 -o caddy -g caddy /var/lib/caddy
fi
install -d -m 0750 -g headscale /etc/headscale
install -d -m 0750 -g caddy /etc/caddy
install -d -m 0700 /etc/iglo-headscale
sed "s/@HOST@/$HEADSCALE_HOSTNAME/g" "$STACK/config.yaml.in" > /etc/headscale/config.yaml
sed "s/@HOST@/$HEADSCALE_HOSTNAME/g" "$STACK/Caddyfile.in" > /etc/caddy/Caddyfile
chown root:headscale /etc/headscale/config.yaml
chown root:caddy /etc/caddy/Caddyfile
chmod 0640 /etc/headscale/config.yaml /etc/caddy/Caddyfile
if [[ ! -f /etc/headscale/policy.hujson ]]; then
    # One private family network. Keep administrator edits on updates.
    printf '%s\n' '{"acls":[{"action":"accept","src":["*"],"dst":["*:*"]}]}' > /etc/headscale/policy.hujson
    chown root:headscale /etc/headscale/policy.hujson
    chmod 0640 /etc/headscale/policy.hujson
fi
install -m 0755 "$WORK/headscale" /usr/local/bin/headscale
install -m 0755 "$WORK/caddy" /usr/local/bin/caddy
install -m 0644 "$STACK/headscale.service" "$STACK/caddy.service" /etc/systemd/system/
install -m 0600 "$STACK/nftables.conf" /etc/nftables.conf
install -d -m 0755 /etc/systemd/journald.conf.d
printf '[Journal]\nSystemMaxUse=64M\nRuntimeMaxUse=16M\n' > /etc/systemd/journald.conf.d/iglo-controller.conf
nft --check -f /etc/nftables.conf
/usr/local/bin/caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null
# configtest opens local state, so it runs only after a stopped consistent backup.
runuser -u headscale -- /usr/local/bin/headscale --config /etc/headscale/config.yaml configtest >/dev/null
systemctl daemon-reload
systemctl restart systemd-journald
systemctl enable --now nftables
nft -f /etc/nftables.conf
systemctl enable --now headscale caddy
healthy=false
for _ in $(seq 1 30); do
    if curl --fail --silent --max-time 2 http://127.0.0.1:8080/health >/dev/null && systemctl is-active --quiet caddy; then
        healthy=true
        break
    fi
    sleep 1
done
[[ "$healthy" == true ]]
# User creation emits no authentication key. Existing accounts stay untouched.
headscale --config /etc/headscale/config.yaml users list --output json > "$WORK/users.json"
if ! python3 -c 'import json,sys;sys.exit(0 if any(u.get("name")=="iglo" for u in json.load(open(sys.argv[1]))) else 1)' "$WORK/users.json"; then
    headscale --config /etc/headscale/config.yaml users create iglo >/dev/null
fi
printf '%s\n' "$HEADSCALE_VERSION" > /etc/iglo-headscale/version
chmod 0600 /etc/iglo-headscale/version
trap - ERR
# Retain three private local recovery points only after successful health checks.
if [[ "$MODE" == update ]]; then
    find /var/backups/iglo-headscale -mindepth 1 -maxdepth 1 -type d -name '20*' | sort -r | tail -n +4 | while IFS= read -r old; do rm -rf -- "$old"; done || echo "Warning: could not prune all old local recovery points." >&2
fi
apt-get clean || echo "Warning: could not clear the package cache." >&2
echo "Local checks passed. Controller: https://$HEADSCALE_HOSTNAME"
if find /var/lib/caddy -type f -name "$HEADSCALE_HOSTNAME.crt" -print -quit | grep -q .; then
    echo "Certificate obtained. Internet reachability is not verified by Iglo."
else
    echo "Certificate issuance is pending. Check public DNS and TCP 80/443 forwarding. Internet reachability is not verified by Iglo."
fi
echo "Approve devices from the guest CLI. The manager's current VPN was not changed."
