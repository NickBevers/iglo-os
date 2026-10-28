# Updates

## Containers and the host

The Updates page checks the Proxmox host and all running containers for updates. It uses apt, the `update` command of community-scripts.org containers, and Docker Compose images.

- **Containers**: once a week (Sunday 04:00 by default), each container with auto-update on gets a snapshot and then its updates. If the update fails, or the container stops, the manager rolls it back to the snapshot. The last 3 snapshots are kept.
- **Proxmox host**: the schedule only checks it, because the host has no snapshots. Install host updates from the dashboard. The page tells you when a new kernel needs a reboot.
- **The manager itself** is never updated by the schedule, and a failed update of it is not rolled back automatically.

## Notifications

Iglo-OS tells you when an update ran, a backup failed, a health check failed or a console opened. It sends each message to Telegram and to ntfy, as far as you set them up on the Updates page. Both are optional.

### Telegram

Create a bot with @BotFather, send it a message, and get your chat ID from @userinfobot. Then enter both on the Updates page.

### ntfy

[ntfy](https://ntfy.sh) sends push notifications to your phone without Google or Apple. The server and the Android app are open source (Apache-2.0), and free. The F-Droid version of the app never uses Firebase: it keeps its own connection to the server. Checked on 28 October 2026, with ntfy v2.34.0 and the app 1.25.2.

1. Install ntfy from F-Droid.
2. On the Updates page, keep the server `https://ntfy.sh`, press **New topic**, and save.
3. In the app, subscribe to that topic, and press **Send test** on the Updates page.

On ntfy.sh, anyone who knows a topic can read it and send to it. **New topic** makes a random name of 25 characters, so treat it like a password. The messages are short lines like "✅ jellyfin (101): updated", without passwords or keys.

To keep the messages off ntfy.sh, run your own ntfy server and enter its address. With logins on, make an access token with `ntfy token add <user>` and enter it too. The manager keeps the token like the Telegram bot token: it never goes to the browser or to the settings file, and it only goes to the server it was entered for. Your phone has to reach that server, at home or through Tailscale.

Seerr can send its own messages to ntfy, for requests and new media: set it up in Seerr under **Settings > Notifications**.

## Updating Iglo-OS

The manager checks the GitHub releases every 6 hours. When there is a new version, the header shows "Update available" and you get a Telegram message. Install it from the Updates page. You can also turn on automatic installs there.

The host agent only installs a bundle that is signed with the release key. It keeps the previous version, and brings it back when the new version does not start within a minute.

For a private repo, create a fine-grained token with read-only access to Contents of this repo only, and enter it on the Updates page.
