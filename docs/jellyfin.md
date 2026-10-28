# Jellyfin

## Install Jellyfin from Iglo-OS

The Apps page installs Jellyfin in its own container. Install it before the media stack: the media stack then finds Jellyfin and adds its libraries and Seerr.

### Before you start

1. Connect the Videos share with your movies and series on the Storage page. See [media-folder.md](media-folder.md).
2. Choose a username and password for the Jellyfin admin.

If the media stack already uses a Videos share, the form chooses that one.

### What the installer does

1. Checks the Videos share, and makes the movies, series and anime folders on it when they do not exist and the share has write access.
2. Makes a Debian 13 container named `jellyfin` (4 cores, 3 GB memory, 16 GB disk) with the tags `iglo` and `jellyfin`.
3. Gives the container the share at `/data/media`, read-only. The share mounts before the container starts. Jellyfin keeps its own data on the disk of the container.
4. Installs Jellyfin from the official repository, `repo.jellyfin.org`. The Updates page updates it later, with the other containers.
5. Passes the GPU of the host to the container, when the host has one (see below).
6. Finishes the setup wizard with your admin account.
7. Adds the libraries **Movies** (`/data/media/<movies folder>`) and **Series** (`/data/media/<series folder>`), and **Anime** and **Anime Movies** for the anime folders, when they are set.
8. Makes scrub previews and chapter images at night (see below).
9. Turns on the Netflix theme, if you chose it.
10. Makes an API key for Iglo-OS and connects the Jellyfin page of Iglo-OS with it. The key never goes to the browser.

The installer takes about 5 minutes. If it fails, it removes the container and the mount it made. Then click **Try again**, or **Change answers** to fix an answer. Your answers stay on the host for 24 hours. The admin password and the SMB password never go back to the browser.

You can also run the installer on the Proxmox host, as root: `bash /usr/local/lib/iglo/stacks/jellyfin/install.sh`. It asks the same questions.

### Hardware transcoding

When the host has a GPU (`/dev/dri/renderD128`, Intel or AMD), the installer:

1. gives the container that device, with the `render` group of the container;
2. checks with `vainfo` of Jellyfin that Jellyfin can use it;
3. turns on VA-API for every codec that `vainfo` says the GPU decodes. On an Intel UHD 770 that is H.264, HEVC (also 10-bit), VP8, VP9 (also 10-bit), AV1, MPEG-2 and VC-1. Without that list it keeps to H.264 and HEVC;
4. on an Intel GPU, turns on VPP tone mapping, so HDR films play on an SDR screen without the CPU.

If the check fails, Jellyfin transcodes on the CPU, and the log says so. For Intel, the `jellyfin-ffmpeg` package brings its own drivers, so you install nothing on the host. For AMD, the check shows whether the drivers in the container are enough.

Check or change the result in Jellyfin: **Dashboard > Playback > Transcoding**. On Intel, you can also switch to **Intel QuickSync (QSV)** there, which is a bit faster. The cores and memory are limits, not reservations, so an idle Jellyfin uses no more than before. The headroom helps when the CPU has to step in, like for burned-in subtitles.

### Scrub previews at night

Scrub previews (trickplay) are the small pictures you see while you drag through a video in the Jellyfin apps. Chapter images show the chapters. Jellyfin makes them by reading every video, so they take hours for a new library. Jellyfin has both off for a library made through its API.

The installer turns both on for its libraries, but never during a library scan: a scan starts whenever Sonarr or Radarr imports something, maybe while you watch. Two night tasks make them instead, daily from 01:00 for at most two hours. They stop before the backups at 03:00, and the next night goes on where the last one stopped. Jellyfin's own times, 02:00 and 03:00 without a limit, would run into the backups.

The previews read keyframes only, which is much faster, with previews a little off their second. With a GPU, the GPU decodes them, and encodes the pictures when `vainfo` lists a JPEG encoder.

Change the times in Jellyfin under **Dashboard > Scheduled Tasks**: **Generate Trickplay Images** and **Extract Chapter Images**. The library settings are under **Dashboard > Libraries**, and the preview settings under **Dashboard > Playback > Trickplay**. Checked against Jellyfin v12.4 on 28 October 2026.

### A Jellyfin that is already installed

A container named `jellyfin` counts as installed, also when Iglo-OS did not make it (for example from community-scripts.org). The Apps page then shows it, and does not install a second one. Iglo-OS can't update or remove that container from the Apps page. Update it on the Updates page.

### Update and remove

The Apps page can update and remove a Jellyfin that Iglo-OS installed.

- **Update** makes a snapshot, and then updates Jellyfin and the other packages. Your settings, users and libraries stay. If the update fails, Iglo-OS rolls the container back to the snapshot.
- **Remove** removes the container with its disks and snapshots, and the Jellyfin page disconnects. The backups of the container stay, so you can restore it on the Backups page. Your movies and series stay on the share, and the share stays connected.

## The Jellyfin page

Connect Jellyfin on the Jellyfin page with its URL and an API key (Jellyfin: Dashboard > API Keys). The page shows library counts, what is playing (and whether it transcodes), and recently added items. A Jellyfin that Iglo-OS installed is connected already.

The page can also turn on a Netflix-style theme for the Jellyfin web client. The theme is written for the Modern layout of Jellyfin 12 and lives in `internal/jellyfin/netflix.css`. The manager adds it to the custom CSS of Jellyfin between two marker comments, so your own custom CSS stays. The TV and phone apps do not use custom CSS.
