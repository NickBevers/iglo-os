# Working on Iglo-OS

Rules for any agent in this repository. They come from building it: each one
fixed a real bug, a slow path or a leak. The skills in `.claude/skills/` hold
the step-by-step workflows, and `docs/performance.md` explains the performance
and memory choices for people.

## The shape

- **Manager** (`cmd/iglo`, `internal/*`): Go, in a 1-core LXC with 512 MiB.
  Serves the web app and the API, talks to Proxmox with a limited token.
- **Host agent** (`cmd/iglo-agent`, `internal/agent`): Go, root on the host,
  only on a Unix socket. Does what the API can't: `pct`, installs, mounts.
- **Web** (`web/`): React, Vite, TanStack Router and Query. Embedded in the
  manager binary, so the Go tests need `make web` first.
- **Installers** (`deploy/`): Bash, run by the agent with checked answers.
- **Dev mock** (`dev/mock`): a fake Proxmox, agent, Jellyfin and Immich.
- **Demo** (`web/src/demo/fixtures.json`): the web app on sample data.

## Checks

- Run `make web`, then `make test`, `go test -race ./internal/... ./cmd/...`,
  `gofmt -l cmd internal dev web/embed.go`, Prettier in `web/`, `bash -n` on
  every changed script, and `make shellcheck` (ShellCheck 0.10.0 in Docker,
  warnings and up). CI runs the same on every PR. A script that another file
  reads a variable of gets `# shellcheck disable=SC2034` with the reason.
- Run `gofmt` after editing Go with a script. Scripted edits break alignment
  and import groups (standard, external, then `iglo/...`).
- A new route file needs `npx vite build` once, so `routeTree.gen.ts` is
  regenerated before `tsc -b`. Commit the regenerated file.
- Check compose files with `docker compose -f FILE --env-file VARS config`.
  Name the vars file anything but `.env`: a deny rule blocks reading and
  writing `.env` files, also in the scratchpad.
- See a feature work end to end on the dev mock before calling it done. The
  `iglo-dev-run` skill has the commands. Most real bugs here showed up there,
  not in unit tests.
- A test that reads a stream must not race its first event. Take the first
  snapshot before the stream opens, then write.

## Everything the agent serves, the mock serves

- Each new agent route gets a fake in `dev/mock` in the same change, or dev
  mode logs errors and the feature can't be tried.
- Each new API answer that a page reads gets a fixture in the demo.
- The mock checks for changes once a second where the real system pushes.
  Timings on the mock are an upper bound, not a measurement.

## Upstream software

- Check an API, an event name or a file format against the source of the
  version we pin, never from memory. The `iglo-upstream-check` skill shows how:
  for a pinned image digest, read its `org.opencontainers.image.revision`
  label, then the source at that commit.
- Write the version and the date you checked in the code comment or the docs.
- Pin every image of a stack to a digest (`image: repo:tag@sha256:… # version`),
  with `go run ./dev/pinimages FILE`. A test fails on an image without one.
  Immich is pinned by its release in `.env` instead.
- Things that were wrong from memory: Wolf's `stable` branch is old while the
  `stable` image is new; Wolf lists a session's ID as `client_id`; Prowlarr's
  Nyaa and Anime Tosho are Cardigann definitions, picked by `definitionName`;
  Immich keys take fine-grained permissions like `queue.read`.

## Secrets

- Passwords, keys and tokens never go to the browser, to logs or to a file in
  git. Answers files on the host are 0600 and the installer deletes them.
- Relays pass on event names, never event data. Wolf's session events and its
  session list carry the AES stream keys; strip them in the agent.
- The setup keeps its plan with secrets in memory only, and drops it when the
  run ends. A restart means filling the pages again; done steps are skipped.
- A secret that an app needs at runtime, like a VPN login, is sealed on the
  host with `deploy/secrets.sh` (`systemd-creds`, TPM2 when there is one) and
  reaches the container unsealed in RAM, never on its disk: not in `.env`, not
  in a backup, not in the retry file. Pass it on stdin, never as an argument
  or a here-string, which can be a file.

## Performance

- Every `pct` call starts a Perl program on the host. Don't call it in a loop
  without a cache: the agent keeps `pct config` for 30 seconds
  (`containerConfig`). Prefer one long-lived `pct exec` stream over polling.
- `proxmox.Client.Resources` is shared for 2 seconds and node versions for 10
  minutes. Use the client, don't add a second cache in front of it.
- Before adding a poll, count its cost per minute per open tab, and on the
  host. Poll fast only while something runs. With the live stream up, polls
  are a fallback of about a minute.
- Keep slow-changing data (container notes, addresses, storages, Jellyfin's
  library) in a `ttlCache` or similar, and forget it when an operation that
  changes it finishes.
- Run independent calls of one request in parallel (`sync.WaitGroup.Go`).
- Load large libraries lazily (`React.lazy`), like the charts. Check the size
  of the `_app` chunk, which every page loads.
- Write state files only when the state changed.

## Live updates

- `GET /api/events` names a topic, never data. A page refreshes that topic
  through its normal query. New topic: a constant in `internal/events`, a
  publish where it changes, a key in `web/src/lib/live.ts`, and the mock.
- Every stream keeps a fallback poll, because streams drop.
- Wake a watcher right after the user acts (like Play), instead of waiting
  for its idle interval.

## Memory and hardware

- The target is an HP Elite Mini G9: i7-12700T with 20 threads, 32 GB, a
  512 GB NVMe and a UHD 770. 32 GB is the hard limit: don't propose more RAM.
- Container memory and cores are limits, not reservations. Raising cores costs
  nothing idle; check memory totals against 32 GB in the setup review.
- Background work decides on the host's `MemAvailable` and memory pressure
  (PSI) from the agent, not on what Proxmox counts as used.
- Use the GPU when the host has one, and keep working without it: optional
  compose files through `COMPOSE_FILE` in `.env`, and codecs read from
  `vainfo`, not assumed.
- Minecraft is dropped. Its code stays hidden; don't bring it back.

## Installers

- Apps reach the NAS only through scoped shares (`deploy/storage.sh`,
  `bind_share`). There is no other media mount.
- An optional service is a Compose profile (`COMPOSE_PROFILES`), and a stack
  from before the option keeps its old behaviour after an update.
- Check every answer in the agent before it reaches a script, and again in
  the script.
- A step that can fail on the network (an indexer, a tracker) only warns, so
  it can't fail the whole install.

## Writing and Git

- Follow the writing style in the user's global instructions: no dashes as
  punctuation, short sentences, plain words. That covers code comments, docs,
  commits and PR descriptions.
- Commits: `type(scope): what changed`, with a body that says why. No Claude
  attribution.
- One branch and one PR per topic. Say in the PR which PR it builds on, what
  was tested, and what wasn't. Nick merges; open the PR and stop there.
- Personal setup notes belong in `~/Documents/personal/home-setup`, never here.
