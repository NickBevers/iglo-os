# Guided setup

The Setup page installs the whole stack in one go: the shares on your NAS, Proxmox backups on the NAS, and the apps, connected to each other. A new server shows a link to it on the overview. Install single apps later from the Apps page, as before.

## The four pages

1. **Apps.** Everything is chosen. Leave out what you don't want. An installed app shows as installed and stays as it is. Gaming needs a prepared host (see [gaming.md](gaming.md#prepare-the-host)); until then the page says what is missing and leaves it out.
2. **NAS.** The address of the NAS, and one share per role that the apps need: Videos, Photos, Backups, Music, Inbox and Games. Each share has its own account by default, so an app can only reach its own folders. **One account for all shares** is easier, and lets every app reach every share. A share that is connected already is used again. **Keep Proxmox backups on the NAS** is on by default.
3. **Accounts.** One login for Jellyfin, Seerr, the apps of the media stack and Immich, with an email address for Immich. The ProtonVPN key and the anime and FlareSolverr switches show when the media stack is chosen, and the confirmation of the privileged container when Gaming is.
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
