# Storage

The Umbrel keeps all files: media, music, photos and backups. It runs umbrelOS 2 and has more than one user. The mini PC keeps only what must be fast or local. See [hardware.md](hardware.md#ssd).

## SMB on the Umbrel

The Umbrel shares folders over SMB. Sharing is off by default. Nick turned it on on 19 October 2026 and connected the Mac to it in Finder. Nick has the username and password. The install form asks for them, and they never go in git.

To turn it on, or to change what is shared:

1. On the Umbrel, open **Settings > File sharing > Configure**.
2. Share the whole Home folder, or only some folders. You can also right-click a folder in Files and choose **Share over network**.
3. Note the username and password on that page. They are not your Umbrel login.
4. Test it from the Mac: Finder > Go > Connect to Server > `smb://umbrel.local`.

Source: [Umbrel: accessing your files from other devices](https://umbrel.com/support/files/accessing-your-files-from-other-devices).

### What the Mac sees

Checked on 19 October 2026 with `mount` and `smbutil statshares -a`:

| | |
| --- | --- |
| Share | `Nick's Umbrel`, the whole Home folder |
| User | `umbrel` |
| Protocol | SMB 3.1.1, with signing on and encryption off |
| Free space | 7.1 TiB (the 8 TB disk, counted in TiB) |
| Folders | `Documents`, `Downloads`, `Music`, `Photos`, `Videos`. These are the folders umbrelOS makes itself. |

SMB 3.1.1 is the newest version, and the Linux CIFS client supports it well. The mount can ask for it with `vers=3.1.1`.

### Share only what Iglo-OS needs

Today the Mac sees the whole Home folder, `Documents` included. That is fine for the Mac. For the mini PC, share only the folders Iglo-OS uses: `Videos`, `Music`, `Photos`, `Inbox` and `Backups`. Then the mini PC can't touch `Documents` or `Downloads`, even when something on it goes wrong.

It also avoids a small problem. The name `Nick's Umbrel` has a quote and a space, and a systemd mount unit needs those escaped. A share named after a single folder, like `Videos`, has neither.

Check on the Umbrel that a folder share gets the name of its folder.

Give the Umbrel a fixed IP in the router (a DHCP reservation). Iglo-OS mounts the shares by IP. Then the mount doesn't depend on mDNS on the Proxmox host.

## Folders on the Umbrel

The plan uses the folders that umbrelOS already has. Files then also show up in the right place in the Files app of the Umbrel.

| Folder | What is in it | Used by |
| --- | --- | --- |
| `Videos/Movies` | movies | Radarr, Jellyfin |
| `Videos/Series` | series | Sonarr, Jellyfin |
| `Videos/Anime` | anime series | Sonarr, Jellyfin |
| `Videos/Anime-Movies` | anime movies | Radarr, Jellyfin |
| `Music/Library` | songs, as Artist/Album | yt-dlp, later Lidarr, Jellyfin |
| `Music/Videos` | music videos, only when you ask for one | yt-dlp, Jellyfin |
| `Photos/Immich` | photos and videos from the phones | Immich |
| `Inbox` | your own files, before they are converted | FileFlows |
| `Backups` | Proxmox backups of the containers | Proxmox |

`Inbox` and `Backups` are new. The install forms ask for the folders, like they ask for the movies and series folders today.

Files that are already on the Umbrel:

- **`Videos`** already has some videos. Jellyfin only reads the subfolders above, so loose files in `Videos` stay out of the libraries. Move them to `Inbox` to convert and sort them.
- **`Photos`** may already hold photos. Immich writes new photos to `Photos/Immich`. It can show the older ones as an external library, which it only reads and never changes.

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

Add the `Backups` share as a Proxmox storage of type SMB/CIFS, with the content "VZDump backup". Then choose it as the storage on the Backups page of Iglo-OS.

Proxmox does not back up the media, music or photos folders. A backup of a container skips its mounts. Those folders only live on the Umbrel.

## A second copy of the photos

Media and music can be downloaded again. Photos can't. As long as the Umbrel holds the only copy, one broken disk loses them.

Before you delete photos from your phone, make a second copy. The options:

- an external disk on the mini PC, with a nightly copy;
- an offsite bucket, like Backblaze B2, with restic or rclone;
- both.

This is still open. Also check how many disks the Umbrel has, and whether it mirrors them.
