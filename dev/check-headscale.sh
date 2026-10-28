#!/usr/bin/env bash
# Real pinned binaries and proxy checks in one isolated 512 MiB container.
# This does not run Proxmox or prove performance on the Mini PC.
set -euo pipefail
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# Debian 13 official multiarch image checked 28 October 2026.
IMAGE=debian@sha256:89c1ea91e897f0fe0a3731595330bc45b8bdef4047464a07116f891a94054b31
docker run --rm --interactive --platform linux/amd64 --memory 512m --memory-swap 512m --cpus 1 \
    --cap-add NET_ADMIN -v "$REPO/deploy/stacks/headscale:/stack:ro" "$IMAGE" bash -s <<'CHECK'
set -Eeuo pipefail
trap 'echo "Smoke test failed at line $LINENO" >&2' ERR
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq ca-certificates curl tar sqlite3 nftables python3 >/dev/null
work=$(mktemp -d)
value() { sed -n "s/^$1=//p" /stack/guest.sh; }
hs_version=$(value HEADSCALE_VERSION)
caddy_version=$(value CADDY_VERSION)
curl -fsSL --retry 3 "https://github.com/juanfont/headscale/releases/download/v$hs_version/headscale_${hs_version}_linux_amd64" -o "$work/headscale"
curl -fsSL --retry 3 "https://github.com/caddyserver/caddy/releases/download/v$caddy_version/caddy_${caddy_version}_linux_amd64.tar.gz" -o "$work/caddy.tar.gz"
printf '%s  %s\n' "$(value HEADSCALE_SHA256)" "$work/headscale" | sha256sum --check --status
printf '%s  %s\n' "$(value CADDY_SHA256)" "$work/caddy.tar.gz" | sha256sum --check --status
tar -xzf "$work/caddy.tar.gz" -C "$work" caddy
install -m 0755 "$work/headscale" "$work/caddy" /usr/local/bin/
useradd --system --user-group --home-dir /var/lib/headscale headscale
useradd --system --user-group --home-dir /var/lib/caddy caddy
install -d -m 0750 -g headscale /etc/headscale
install -d -m 0750 -g caddy /etc/caddy
install -d -m 0700 -o headscale -g headscale /var/lib/headscale /run/headscale
install -d -m 0700 -o caddy -g caddy /var/lib/caddy
sed 's/@HOST@/vpn.example.com/g' /stack/config.yaml.in > /etc/headscale/config.yaml
printf '%s\n' '{"acls":[{"action":"accept","src":["*"],"dst":["*:*"]}]}' > /etc/headscale/policy.hujson
chmod 0640 /etc/headscale/config.yaml /etc/headscale/policy.hujson
chown root:headscale /etc/headscale/*
runuser -u headscale -- env GOMEMLIMIT=256MiB GOMAXPROCS=1 headscale --config /etc/headscale/config.yaml configtest >/dev/null
sed 's/@HOST@/vpn.example.com/g' /stack/Caddyfile.in > "$work/Caddyfile"
caddy validate --config "$work/Caddyfile" --adapter caddyfile >/dev/null
# Production uses Let's Encrypt. The isolated test uses a throwaway local CA
# and ports on loopback, so it never requests a public certificate.
python3 - "$work/Caddyfile" <<'PY'
import pathlib,re,sys
p=pathlib.Path(sys.argv[1]);s=p.read_text()
s,n=re.subn(r'tls\s*\{\s*issuer acme\s*\{\s*dir https://acme-v02.api.letsencrypt.org/directory\s*\}\s*\}', 'tls internal',s)
assert n==1
s=s.replace('http://vpn.example.com {','http://vpn.example.com:8081 {').replace('https://vpn.example.com {','https://vpn.example.com:8443 {')
p.write_text(s)
PY
install -m 0644 "$work/Caddyfile" /etc/caddy/test-Caddyfile
runuser -u headscale -- env GOMEMLIMIT=256MiB GOMAXPROCS=1 headscale --config /etc/headscale/config.yaml serve > "$work/headscale.log" 2>&1 &
runuser -u caddy -- env GOMEMLIMIT=96MiB GOMAXPROCS=1 XDG_DATA_HOME=/var/lib/caddy/data XDG_CONFIG_HOME=/var/lib/caddy/config caddy run --config /etc/caddy/test-Caddyfile --adapter caddyfile > "$work/caddy.log" 2>&1 &
trap 'code=$?; trap - EXIT; while read -r pid; do kill "$pid" 2>/dev/null || true; done < <(jobs -p); exit "$code"' EXIT
ready=false
for _ in $(seq 1 60); do
    if curl --noproxy '*' -k --silent --fail --resolve vpn.example.com:8443:127.0.0.1 https://vpn.example.com:8443/health >/dev/null; then ready=true;break;fi
    sleep 1
done
if [[ "$ready" != true ]]; then cat "$work/headscale.log" "$work/caddy.log" >&2;exit 1;fi
for path in /api/v1/users /api /v1/users /swagger /metrics /debug/pprof/ /verify /%61pi/v1/users; do
    code=$(curl --noproxy '*' -k --silent --output /dev/null --write-out '%{http_code}' --resolve vpn.example.com:8443:127.0.0.1 "https://vpn.example.com:8443$path")
    [[ "$code" == 404 ]] || { echo "Private route $path returned $code" >&2;exit 1; }
done
[[ "$(curl --noproxy '*' -k --silent --output /dev/null --write-out '%{http_code}' --resolve vpn.example.com:8443:127.0.0.1 https://vpn.example.com:8443/derp/probe)" == 200 ]]
headscale --config /etc/headscale/config.yaml users create iglo >/dev/null
headscale --config /etc/headscale/config.yaml users list --output json > "$work/users.json"
python3 -c 'import json,sys;assert any(u.get("name")=="iglo" for u in json.load(open(sys.argv[1])))' "$work/users.json"
checked=$(runuser -u headscale -- mktemp /var/lib/headscale/.iglo-backup-XXXXXX)
runuser -u headscale -- sqlite3 /var/lib/headscale/db.sqlite ".backup '$checked'"
[[ "$(runuser -u headscale -- sqlite3 "$checked" 'PRAGMA integrity_check')" == ok ]]
[[ "$(stat -c '%U' "$checked")" == headscale ]]
nft --check -f /stack/nftables.conf
nft -f /stack/nftables.conf
curl --noproxy '*' -k --silent --fail --resolve vpn.example.com:8443:127.0.0.1 https://vpn.example.com:8443/health >/dev/null
printf '%s\n' 'Passed: 512 MiB / one CPU startup, pinned config validation, private proxy routes, users, SQLite backup and guest firewall.'
CHECK
