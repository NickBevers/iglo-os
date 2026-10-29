# Updates

## Containers and the host

The Updates page checks the Proxmox host and all running containers for updates. It uses apt and Docker Compose images.

- **Containers**: once a week (Sunday 04:00 by default), each container with auto-update on gets a snapshot and then its updates. If the update fails, or the container stops, the manager rolls it back to the snapshot. The last 3 snapshots are kept.
- **Proxmox host**: the schedule only checks it, because the host has no snapshots. Install host updates from the dashboard. The page tells you when a new kernel needs a reboot.
- **Scoped Photos, Music, Inbox and Gaming containers** never get automatic disk rollback, including through this page. Their NAS data cannot be rewound with the local database or queue. Inspect a failed operation and use the application recovery procedure.
- **The manager itself** is never updated by the schedule, and a failed update of it is not rolled back automatically.
- **Apps of Iglo-OS** run images pinned to a digest, so the weekly run finds no new images for them. Their new versions come with a new Iglo-OS version, through **Update** on the Apps page. Legacy stacks use snapshots; scoped apps export their application data and require deliberate recovery.

## Notifications

Iglo-OS tells you when an update ran, a backup failed, a health check failed or a console opened. It sends each message to ntfy, if you set it up on the Updates page. It is optional. Alerts are saved locally before delivery. A failed delivery retries from 30 seconds up to 30 minutes apart, including after a restart. The queue holds up to 128 message parts of 3000 bytes, with no credentials. An empty queue makes no network requests or file writes. Changing the ntfy server or topic, or turning ntfy off, discards pending deliveries rather than sending old alerts to the new place. A full queue or a disk error is logged for attention.

### ntfy

[ntfy](https://ntfy.sh) sends push notifications to your phone without Google or Apple. The server and the Android app are open source (Apache-2.0), and free. The F-Droid version of the app never uses Firebase: it keeps its own connection to the server. Checked on 28 October 2026, with ntfy v2.34.0 and the app 1.25.2.

1. Install ntfy from F-Droid.
2. On the Updates page, keep the server `https://ntfy.sh`, press **New topic**, and save.
3. In the app, subscribe to that topic, and press **Send test** on the Updates page.

On ntfy.sh, anyone who knows a topic can read it and send to it. **New topic** makes a random name of 25 characters, so treat it like a password. The messages are short lines like "✅ jellyfin (101): updated", without passwords or keys.

To keep the messages off ntfy.sh, run your own ntfy server and enter its address. With logins on, make an access token with `ntfy token add <user>` and enter it too. The manager keeps the token. It never goes to the browser or to the settings file, and it only goes to the server it was entered for. Your phone has to reach that server, at home or through Tailscale.

Seerr can send its own messages to ntfy, for requests and new media: set it up in Seerr under **Settings > Notifications**.

## Updating Iglo-OS

New versions come from your own machine: tag a version and run
`make deploy HOST=root@<host>`. It runs every check, signs the bundle with your
key, and hands it to the host agent over SSH. See
[deploy to your server](development.md#deploy-to-your-server).

The manager can't start an upgrade, and it doesn't look for new versions. Only
root on the host can, through the agent's root-only socket. The agent only
installs a bundle that is signed with your key, and only a newer version. It
keeps the previous version. The new version has to start and reach the host
agent and Proxmox (`iglo ready`) within about a minute, or the previous version
comes back. When even the previous version doesn't come back, the upgrade says
so: run `iglo-restore` on the host to restore the manager from its last backup.
To do that from away from home, see
[reach the host when the manager is down](tailscale.md#reach-the-host-when-the-manager-is-down).

The Updates page shows the running version, and how the last upgrade went,
with its log.

## Headscale controller

Headscale updates keep checked local recovery points. A verified local recovery is not overwritten with an older snapshot. Unsupported version changes need a reviewed upgrade path. See [controller updates](headscale.md#updates-backups-and-removal).
