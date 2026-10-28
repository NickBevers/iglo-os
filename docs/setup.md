# Guided setup

The Setup page installs the whole stack in one go: the shares on your NAS, Proxmox backups on the NAS, and the apps, connected to each other. A new server shows a link to it on the overview. Install single apps later from the Apps page, as before.

## The four pages

1. **Apps.** Everything is chosen. Leave out what you don't want. An installed app shows as installed and stays as it is. Gaming needs a prepared host (see [gaming.md](gaming.md#prepare-the-host)); until then the page says what is missing and leaves it out.
2. **NAS.** The address of the NAS, and one share per role that the apps need: Videos, Photos, Backups, Music, Inbox and Games. Each share has its own account by default, so an app can only reach its own folders. **One account for all shares** is easier, and lets every app reach every share. An Umbrel has one SMB login for all its shared folders, so use **One account for all shares** there (see [On an Umbrel](scoped-storage.md#on-an-umbrel)). A share that is connected already is used again. **Keep Proxmox backups on the NAS** is on by default.
3. **Accounts.** One login for Jellyfin, Seerr, the apps of the media stack and Immich, with an email address for Immich. The VPN (ProtonVPN or PIA, with its login and locations) and the anime and FlareSolverr switches show when the media stack is chosen, and the confirmation of the privileged container when Gaming is.
4. **Review.** What will happen, in order, and the memory limits of the chosen apps against the memory of the host. Nothing changes before **Install**.

The manager checks the whole plan before it starts, with the same checks as each installer, so a typo can't stop the run halfway.

## What it does

In this order:

1. Connects the shares, or uses the ones that are connected.
2. Registers the Backups share as Proxmox backup storage and points the backup job at it, every night at 03:00, keeping 7 daily, 4 weekly and 3 monthly backups. A job that you set up before keeps its schedule.
3. Installs Jellyfin, then the media stack with the API key that the Jellyfin install made and the same admin for Seerr, then Photos, Music, Inbox and Gaming.
4. After Photos, creates the Immich admin with your email and password, and an API key that may only read and pause the queues. It connects Background work with that key and turns it on, with heavy work from 02:00 to 07:00 in the time zone of your browser.

Each step that is done already is skipped, so running the setup again only does what is missing.

## When a step fails

The setup stops at that step and shows why. **Try again** runs it again, **Skip this step** goes on without it, and **Stop the setup** ends it. What is installed stays. You can close the page while the setup runs; it goes on in the manager.

Passwords and keys stay in the memory of the manager until the setup is done, and are never written to its disk. When the manager restarts during a setup, fill in the pages again. The steps that are done are skipped.

## Keep your plan

A plan file holds everything the setup asked, without the passwords and keys: the apps, the NAS address, the shares and their usernames, the admin username and email, the VPN provider and locations, and the switches. The PIA username stays out too. A plan file from before PIA has `countries` instead of `locations`, and still loads. Keep it with your own notes, like in a private repository, together with the [settings file](settings-file.md).

- **Download this server's plan** on the first page, or **Download the plan** after a run. It is the plan of the last setup. A server that was set up without the setup gets a plan of its apps and shares; the accounts on the NAS are unknown, so they stay empty.
- **Load a plan file** on the first page fills all four pages. Check them, type the passwords and keys, and confirm the privileged container of Gaming again. Loading starts nothing.

The manager keeps the plan of the last run in `setup-plan.yaml` in its data folder, with mode 0600 and without secrets. A file with a password written in by hand loads without it.

```yaml
version: 1
apps: [jellyfin, media, photos, music, inbox]
nas:
  server: 192.168.1.20
  oneAccount: false
  account:
    username: ""
  shares:
    videos: { share: Videos, username: media }
    photos: { share: Photos, username: immich }
    backups: { share: Backups, username: backup }
    music: { share: Music, username: music }
    inbox: { share: Inbox, username: inbox }
admin: { username: nick, email: nick@example.com }
vpn: { provider: protonvpn, locations: Netherlands }
flareSolverr: true
animeIndexers: true
proxmoxBackups: true
timezone: Europe/Brussels
storage: local-lvm
```

New Photos libraries get bounded heavy-job concurrency and convert video only
when required. Face grouping is off unless you opt into the separately
restricted model weights on the Accounts page. Existing initialized Immich
libraries keep their settings. See [Photos](photos.md#guided-setup-defaults-and-model-licences).
