# How it works

- **Manager**: a Go binary in its own LXC container. It serves the React app and talks to the Proxmox API with a limited API token.
- **Host agent**: a small Go service on the Proxmox host. It does what the API can't do, like `apt` upgrades, backup jobs, consoles and app installs. It only listens on a Unix socket that is shared with the manager container.
- **HTTPS**: the manager runs its own certificate authority. That CA can only sign certificates for `iglo.local`.
- **Login**: one admin account with a password and an authenticator app code (TOTP).
- **Data**: JSON files in `/var/lib/iglo` inside the manager container. There is no database.
- **Memory**: the manager runs with `GOMEMLIMIT=360MiB` in its 512 MiB container. The host agent runs with `GOMEMLIMIT=128MiB`, `MemoryHigh=512M` and `MemoryMax=1G`. Installs run in systemd units of their own, so these limits don't cover them. `install.sh --zram` adds compressed swap in RAM on the host. Photos, Music, Inbox and Gaming have no swap of their own, so they don't use it; the manager, Jellyfin and the media stack can.
- **Live updates**: an open page keeps one Server-Sent Events stream to the manager, `GET /api/events`. An event only names what changed, like `apps` or `gaming`, and the page then asks for it the normal way. The manager hears of changes from its own services, from an event stream of the host agent (installs that move on, the sessions of Wolf, the queue of the Music worker) and from the WebSocket of Jellyfin (playback that starts or stops). Every page still polls, slower while its stream is up, so a dropped stream only makes it slower. [performance.md](performance.md) explains these choices and what they gained.

## Permissions

The API token of the manager has the role `IgloManager` with these privileges:

`Sys.Audit, VM.Audit, VM.PowerMgmt, VM.Snapshot, VM.Snapshot.Rollback, VM.Backup, Datastore.Audit, Datastore.AllocateSpace`

Everything that needs more, like the backup job or a console, goes through the host agent. The agent only runs fixed commands with checked input.
