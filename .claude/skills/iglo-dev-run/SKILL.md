---
name: iglo-dev-run
description: Run the Iglo-OS manager against the dev mock in a scratch folder and drive its API with curl, to see a feature work end to end. Use before calling an Iglo-OS change done, to reproduce a bug, or to time a flow like Play, an install or the setup.
---

# Run Iglo-OS end to end on the dev mock

`make dev` is for a person in a browser. For an agent, run the mock and the
manager in a scratch folder with a copy of the dev state, so `.dev/` of the
user stays as it is, and drive the API with curl.

## 1. Build into a scratch folder

```sh
S=<scratchpad>/devrun
rm -rf "$S/.dev" && mkdir -p "$S/.dev"
# The admin account only, for a fresh server. Copy all of .dev/ to keep the installed apps.
rsync -a --include 'data/***' --exclude '*' .dev/ "$S/.dev/"
(cd web && npm run build)          # the manager embeds the web app
go build -o "$S/mock" ./dev/mock
go build -o "$S/iglo" ./cmd/iglo
```

Switches in `$S/.dev/`: `no-jellyfin` hides the Jellyfin container of the
mock, `gaming-not-ready` makes Gaming not ready, `gaming-second-session`
lets a second device join a running game.

## 2. Start both, in the background

Write `$S/run.sh` and run it with the background option of the shell tool:

```sh
#!/usr/bin/env bash
cd "$S"
./mock > mock.log 2>&1 &
for _ in $(seq 50); do [[ -S .dev/agent.sock ]] && break; sleep 0.1; done
export PATH="<repo>/dev/bin:$PATH"
export IGLO_DEV=1 IGLO_HTTP_ADDR=127.0.0.1:8080 IGLO_DATA_DIR=.dev/data
export IGLO_AGENT_SOCKET=.dev/agent.sock IGLO_SELF_VMID=100
export PROXMOX_URL=http://127.0.0.1:18006 PROXMOX_TOKEN_ID='iglo@pve!dev' PROXMOX_TOKEN_SECRET=dev
exec ./iglo serve > iglo.log 2>&1
```

Then check `grep -c 'address already in use' $S/*.log` is 0. If it isn't, an
old run still holds the ports, and you are testing old binaries.

## 3. Log in and call the API

```sh
CODE=$("$S/mock" code | tr -dc '0-9' | tail -c 6)
H=(-H 'X-Iglo-Request: 1' -H 'Content-Type: application/json')
curl -s -c "$S/jar" "${H[@]}" -d "{\"username\":\"admin\",\"password\":\"iglo-dev-password\",\"code\":\"$CODE\"}" \
  http://127.0.0.1:8080/api/auth/login
curl -s -b "$S/jar" http://127.0.0.1:8080/api/apps
```

Every request that changes something needs the `X-Iglo-Request: 1` header.
To watch live updates, keep `curl -N -b "$S/jar" http://127.0.0.1:8080/api/events`
open in the background and write each line with a timestamp to a file.

Parse JSON with `python3 -I`, from a file or stdin. For a longer script, use a
heredoc (`python3 -I - FILE <<'PYEOF'`), not a quoted one-liner: escaped
quotes inside f-strings break.

## 4. Wait without sleeping in the foreground

Loop on the condition, like
`until [ "$(curl ... | python3 -I -c '...state...')" != running ]; do sleep 3; done`,
or run the loop in the background and wait for its notice.

## 5. Stop by port, not by name

```sh
kill $(lsof -nP -t -iTCP:8080 -iTCP:18006 -sTCP:LISTEN)
```

`pkill -f` with a pattern missed the processes once, and the next run tested
stale binaries. The background shell exits with 144 after this kill; that is
expected.

## What the mock can't tell you

The mock looks for changes once a second where the real agent, Wolf or the
Music worker push them, and its Proxmox answers at once. Say so when you
report timings. The installers only play a script on the mock; the shell side
needs a real Proxmox host.
