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

Decided on 19 October 2026: the mini PC gets only the folders Iglo-OS uses, `Videos`, `Music`, `Photos`, `Inbox` and `Backups`, each as its own share. Not the whole Home folder. Then the mini PC can't touch `Documents` or `Downloads`, even when something on it goes wrong. The Mac keeps its share of the whole Home folder.

It also avoids a small problem. The name `Nick's Umbrel` has a quote and a space, and a systemd mount unit needs those escaped. Iglo-OS only accepts a share name without spaces or slashes, like `Videos`.

Check on the Umbrel that a folder share gets the name of its folder.

Give the Umbrel a fixed IP in the router (a DHCP reservation). Iglo-OS mounts the shares by IP. A test mount showed that `mount.cifs` must resolve the name itself, and a Proxmox host can't look up `.local` names like `umbrel.local`.

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
| `Videos/Home` | your own videos, converted: old videos, behind the scenes, family videos | FileFlows, Jellyfin |
| `Photos/Immich` | photos and videos from the phones | Immich |
| `Inbox` | your own files, before they are converted | FileFlows |
| `Backups` | Proxmox backups of the containers | Proxmox |

`Inbox`, `Backups` and `Videos/Home` are new. The install forms ask for the folders, like they ask for the movies and series folders today.

Files that are already on the Umbrel:

- **`Videos`** already has some videos. Jellyfin only reads the subfolders above, so loose files in `Videos` stay out of the libraries. Move them to `Inbox` to convert and sort them.
- **`Photos`** may already hold photos. Immich writes new photos to `Photos/Immich`. It can show the older ones as an external library, which it only reads and never changes.

## SMB in Iglo-OS

Done on 19 October 2026. The Jellyfin and media stack forms have a third choice next to NFS and a folder on the host: **SMB share**. See [media-folder.md](../media-folder.md#on-a-nas-smb).

- The host mounts the share with `mnt-iglo-media.mount`, with the type `cifs`.
- The password goes in `/etc/iglo-media.credentials`, a file that only root can read. The unit only names it. A failed install keeps it in the saved answers on the host for 24 hours, like the other secrets. It never goes back to the browser.
- The containers are unprivileged, so the mount gives every file to user 101000 on the host, with `uid=101000,gid=101000`. Files are `0644` and folders `0755`, so Jellyfin reads and the media stack writes.
- The installer makes the movies and series folders on the share when they are missing.
- The Health page shows the share with its free space, like an NFS share.

How it was tested:

- unit tests for the answers, the saved password and the mount list;
- the real `deploy/lib.sh` in a Debian container, with a fake `systemctl` (29 checks);
- a real CIFS mount of a Samba share, with the exact options of the unit: SMB 3.1.1, the container user can write, another user can only read;
- an install through the dev mock, with a failed try first and then "Change answers" with the saved password.

Not tested yet: a mount of the Umbrel itself from a Proxmox host. That waits for the mini PC.

There is one media mount today, for `Videos`. Music and photos need their own shares and mounts. They come with the music and photo steps of the plan.

## Downloads stay on the mini PC

qBittorrent downloads to the SSD of the mini PC. When a download is done, Sonarr or Radarr copies the file to the Umbrel. Over SMB a copy is the only option, because a hard link can't cross two machines.

So a finished file exists twice while it seeds. Set a seeding limit in qBittorrent, by ratio or by time. Then the copies on the SSD go away.

## Proxmox backups on the Umbrel

Add the `Backups` share as a Proxmox storage of type SMB/CIFS, with the content "VZDump backup". Then choose it as the storage on the Backups page of Iglo-OS.

Proxmox does not back up the media, music or photos folders. A backup of a container skips its mounts. Those folders only live on the Umbrel.

## A second copy of the photos

Media and music can be downloaded again. Photos can't, and neither can your own family videos in `Videos/Home`.

The Umbrel has 3 disks of 4 TB. It uses one of them for redundancy, so one broken disk loses nothing. That leaves the 8 TB. But redundancy is not a backup. A deleted folder, a broken Umbrel or ransomware still takes every copy with it.

Decided: the second copy goes to an external backup hard disk. Nick gets one.

- It holds `Photos` and `Videos/Home`.
- A nightly copy keeps it up to date. Where the disk plugs in, and which tool copies (rsync, restic or the Umbrel itself), is still open.
- Delete photos from the phone only after the first full copy is on that disk.
