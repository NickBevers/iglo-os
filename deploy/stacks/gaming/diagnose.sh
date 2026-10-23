#!/usr/bin/env bash
set -euo pipefail
cd /opt/gaming
echo '== Guest memory and mounted Games share =='
free -h
findmnt --mountpoint /data/games --output TARGET,SOURCE,FSTYPE,OPTIONS
echo '== Render and virtual input devices =='
ls -l /dev/dri /dev/uinput /dev/uhid
echo '== VA-API capabilities (not proof of actual stream encoding) =='
vainfo --display drm --device /dev/dri/renderD128
echo '== Wolf containers =='
docker compose ps
echo '== Recent Wolf logs: check the encoder selected during a live session =='
docker compose logs --tail 120
echo '== Active sessions from private Unix API =='
curl --fail --silent --unix-socket /var/run/wolf/wolf.sock http://localhost/api/v1/sessions
