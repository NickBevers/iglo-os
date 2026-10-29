# Iglo-OS

A web app to manage a Proxmox VE server from `https://iglo.local`: monitoring, updates, backups, logs, consoles, Tailscale and an app catalog.

Iglo-OS started from [DriesH/homelab](https://github.com/DriesH/homelab) v0.10.1.

## Install

You need Proxmox VE 9 or newer on an amd64 host.

Iglo-OS installs from your own machine, over SSH as root on the Proxmox host.
There are no downloads: you build what you install. One time, make a signing
key (see [deploy to your server](docs/development.md#deploy-to-your-server)).
Then tag a version and deploy it:

```sh
git tag -a v0.1.0 -m v0.1.0
make deploy HOST=root@192.168.1.10
```

On a host without Iglo-OS, that runs the first install.

1. Create your admin account when the installer asks. Add the secret it shows to an authenticator app.
2. Download `http://iglo.local/ca.crt` and install it as a trusted root certificate on each device.
3. Open `https://iglo.local` and go to **Setup**. It installs the whole stack in one go, or the apps you choose. See [docs/setup.md](docs/setup.md).

Options: `./install.sh --storage local-lvm --bridge vmbr0 --hostname iglo --ctid 120 --zram`. With `--zram`, the host gets compressed swap in RAM (zstd, up to a quarter of the RAM) as a safety net when everything peaks at once. On a host with ZFS and no limit on its read cache, the installer prints how to set one.

For the least memory pressure, install Proxmox on ext4 with LVM-thin, not ZFS. With one NVMe, ZFS gains little, and its read cache can take a large share of the RAM.

## Storage, processing and gaming

These capabilities describe this source branch. Deploy a version tagged on
this revision to get them.

| Area | Implemented | Setup and limits |
| --- | --- | --- |
| [Scoped SMB storage](docs/scoped-storage.md) | Storage page, folder roles, host-only credentials and consumer-aware disconnect. Jellyfin and the media stack use the Videos share. | Configure NAS permissions first. SMB only: NFS and host folders are not supported. |
| [Photos / Immich](docs/photos.md) | Pinned installer, SSD database/derivatives, NAS originals, matched backups and recovery verification | Create the Immich admin/API key after installation. Rehearse a full library restore before relying on backups. |
| [Music and Inbox](docs/workers.md) | Native Music and FFmpeg workers, queue pages and admission leases | New Inbox queues start paused. Active jobs finish after admission is paused. |
| [Background work](docs/background-work.md) | Immich queue controls and independent worker controls for gaming holds, windows and host RAM headroom | Admission checks do not reserve RAM/GPU capacity. A Moonlight session holds work by itself. |
| [Gaming](docs/gaming.md) | Experimental Wolf/ES-DE app from the Apps page, Gaming page with Play and Stop, library counts per system, read-only ROM access, persistent emulator profiles, session detection and one session at a time | Privileged LXC after explicit confirmation; hardware streaming, controllers and saves still need acceptance tests. |

Scoped Photos/Music/Inbox updates do not automatically roll back guest disks:
their NAS files cannot be rewound with the local database or queue. Inspect failed
updates and use the documented application recovery path.

The local preview simulates host installations and labels sample data. Go tests,
race checks, frontend/build checks and isolated recovery tests do not establish
performance or recovery on a real Proxmox/NAS installation.

## Update

Tag a newer version and run `make deploy HOST=…` again. The host agent only installs a bundle signed with your key, and only a newer version. If the new version does not work, the old one comes back. The Updates page shows the running version and how the last upgrade went. See [updates](docs/updates.md#updating-iglo-os).

## Uninstall

```sh
iglo-uninstall
```

It asks before it deletes the manager container, the update snapshots or the backup job. See [docs/uninstall.md](docs/uninstall.md).

## Run it locally

You need Go and Node.js 22.

```sh
make dev        # fake Proxmox and host agent, API and web app
make dev-code   # the login code, in a second terminal
```

Open the URL printed by Vite (normally http://localhost:5173; it selects another
port if occupied) and log in with `admin` / `iglo-dev-password`. Use
`make dev-code` for the current login code. See [docs/development.md](docs/development.md)
for tests, a real Proxmox and deploys.

## Docs

- [Guided setup](docs/setup.md)
- [How it works](docs/architecture.md)
- [How Iglo-OS stays light: CPU, memory, GPU and live updates](docs/performance.md)
- [Updates and notifications](docs/updates.md)
- [Health and alerts](docs/health.md)
- [Backups and restoring the manager](docs/backups.md)
- [Background work and resource protection](docs/background-work.md)
- [Scoped SMB storage bindings](docs/scoped-storage.md)
- [Photos: Immich installation, backups and recovery](docs/photos.md)
- [Music and FFmpeg workers](docs/workers.md)
- [Gaming: experimental Wolf / ES-DE app](docs/gaming.md)
- [Logs and console](docs/logs-and-console.md)
- [Jellyfin: install and the Jellyfin page](docs/jellyfin.md)
- [Tailscale and self-hosted Headscale](docs/tailscale.md)
- [Install your own Headscale controller](docs/headscale.md)
- [App names (seerr.iglo.local)](docs/app-names.md)
- [Media stack](docs/media-stack.md)
- [Movies and series on the Videos share](docs/media-folder.md)
- [Settings file (iglo.yaml)](docs/settings-file.md)
- [Uninstall](docs/uninstall.md)
- [Development](docs/development.md)

## License

MIT, see [LICENSE](LICENSE).
