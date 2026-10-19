# Storage

The Umbrel keeps all files: media, music, photos and backups. It has 8 TB free, runs umbrelOS 2 and has more than one user. The mini PC keeps only what must be fast or local. See [hardware.md](hardware.md#ssd).

## Turn on SMB on the Umbrel

The Umbrel shares folders over SMB, but sharing is off by default. It is on since 19 October 2026. Nick has the username and password. The install form asks for them, and they never go in git.

1. On the Umbrel, open **Settings > File sharing > Configure**.
2. Share the whole Home folder, or only the folders below. You can also right-click a folder in Files and choose **Share over network**.
3. Note the username and password on that page. They are not your Umbrel login.
4. Test it from the Mac: Finder > Go > Connect to Server > `smb://umbrel.local`.

Source: [Umbrel: accessing your files from other devices](https://umbrel.com/support/files/accessing-your-files-from-other-devices).

Give the Umbrel a fixed IP in the router (a DHCP reservation). Iglo-OS mounts the share by IP. Then the mount doesn't depend on mDNS on the Proxmox host.

## Folders on the Umbrel

| Folder | What is in it | Used by |
| --- | --- | --- |
| `media/movies` | movies | Radarr, Jellyfin |
| `media/series` | series | Sonarr, Jellyfin |
| `media/anime` | anime series | Sonarr, Jellyfin |
| `media/anime-movies` | anime movies | Radarr, Jellyfin |
| `music/library` | songs, as Artist/Album | yt-dlp, later Lidarr, Jellyfin |
| `music/videos` | music videos, only when you ask for one | yt-dlp, Jellyfin |
| `inbox` | your own files, before they are converted | FileFlows |
| `photos` | the originals of your photos and videos | Immich |
| `backups` | Proxmox backups of the containers | Proxmox |

The names are a proposal. The install forms ask for them, like they ask for the movies and series folders today.

## What changes in Iglo-OS

Today the media folder is on an NFS share or on a disk of the host. See [media-folder.md](../media-folder.md). The Umbrel speaks SMB, so Iglo-OS needs an SMB mount:

- The host mounts the share with a systemd `.mount` unit, like `mnt-iglo-media.mount` today, with the type `cifs`.
- The password goes in a file that only root can read, like `/etc/iglo/umbrel.credentials`. It is never written in the unit.
- The containers are unprivileged, so the files must belong to user 101000 on the host. The mount sets that with `uid=101000,gid=101000`.
- The Health page already shows shares with their free space. It also has to understand the SMB mount.

The code lives in `internal/agent/mediafolders.go`, `internal/agent/mounts.go` and `deploy/lib.sh`.

## Downloads stay on the mini PC

qBittorrent downloads to the SSD of the mini PC. When a download is done, Sonarr or Radarr copies the file to the Umbrel. Over SMB a copy is the only option, because a hard link can't cross two machines.

So a finished file exists twice while it seeds. Set a seeding limit in qBittorrent, by ratio or by time. Then the copies on the SSD go away.

## Proxmox backups on the Umbrel

Add the `backups` share as a Proxmox storage of type SMB/CIFS, with the content "VZDump backup". Then choose it as the storage on the Backups page of Iglo-OS.

Proxmox does not back up the media, music or photos folders. A backup of a container skips its mounts. Those folders only live on the Umbrel.

## A second copy of the photos

Media and music can be downloaded again. Photos can't. As long as the Umbrel holds the only copy, one broken disk loses them.

Before you delete photos from your phone, make a second copy. The options:

- an external disk on the mini PC, with a nightly copy;
- an offsite bucket, like Backblaze B2, with restic or rclone;
- both.

This is still open. Also check how many disks the Umbrel has, and whether it mirrors them.
