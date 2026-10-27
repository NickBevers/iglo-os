# Jellyfin

## Install Jellyfin from Iglo-OS

The Apps page installs Jellyfin in its own container. Install it before the media stack: the media stack then finds Jellyfin and adds its libraries and Seerr.

### Before you start

1. Connect the Videos share with your movies and series on the Storage page. See [media-folder.md](media-folder.md).
2. Choose a username and password for the Jellyfin admin.

If the media stack already uses a Videos share, the form chooses that one.

### What the installer does

1. Checks the Videos share, and makes the movies and series folders on it when they do not exist and the share has write access.
2. Makes a Debian 13 container named `jellyfin` (2 cores, 2 GB memory, 16 GB disk) with the tags `iglo` and `jellyfin`.
3. Gives the container the share at `/data/media`, read-only. The share mounts before the container starts. Jellyfin keeps its own data on the disk of the container.
4. Installs Jellyfin from the official repository, `repo.jellyfin.org`. The Updates page updates it later, with the other containers.
5. Passes the GPU of the host to the container, when the host has one (see below).
6. Finishes the setup wizard with your admin account.
7. Adds the libraries **Movies** (`/data/media/<movies folder>`) and **Series** (`/data/media/<series folder>`).
8. Turns on the Netflix theme, if you chose it.
9. Makes an API key for Iglo-OS and connects the Jellyfin page of Iglo-OS with it. The key never goes to the browser.

The installer takes about 5 minutes. If it fails, it removes the container and the mount it made. Then click **Try again**, or **Change answers** to fix an answer. Your answers stay on the host for 24 hours. The admin password and the SMB password never go back to the browser.

You can also run the installer on the Proxmox host, as root: `bash /usr/local/lib/iglo/stacks/jellyfin/install.sh`. It asks the same questions.

### Hardware transcoding

When the host has a GPU (`/dev/dri/renderD128`, Intel or AMD), the installer:

1. gives the container that device, with the `render` group of the container;
2. checks with `vainfo` of Jellyfin that Jellyfin can use it;
3. turns on VA-API for H.264 and HEVC.

If the check fails, Jellyfin transcodes on the CPU, and the log says so. For Intel, the `jellyfin-ffmpeg` package brings its own drivers, so you install nothing on the host. For AMD, the check shows whether the drivers in the container are enough.

Newer GPUs can decode more, like VP9, AV1 and 10-bit HEVC. Turn those on in Jellyfin: **Dashboard > Playback > Transcoding**. On Intel, you can also switch to **Intel QuickSync (QSV)** there, which is a bit faster.

### A Jellyfin that is already installed

A container named `jellyfin` counts as installed, also when Iglo-OS did not make it (for example from community-scripts.org). The Apps page then shows it, and does not install a second one. Iglo-OS can't update or remove that container from the Apps page. Update it on the Updates page.

### Update and remove

The Apps page can update and remove a Jellyfin that Iglo-OS installed.

- **Update** makes a snapshot, and then updates Jellyfin and the other packages. Your settings, users and libraries stay. If the update fails, Iglo-OS rolls the container back to the snapshot.
- **Remove** removes the container with its disks and snapshots, and the Jellyfin page disconnects. The backups of the container stay, so you can restore it on the Backups page. Your movies and series stay on the share, and the share stays connected.

## The Jellyfin page

Connect Jellyfin on the Jellyfin page with its URL and an API key (Jellyfin: Dashboard > API Keys). The page shows library counts, what is playing (and whether it transcodes), and recently added items. A Jellyfin that Iglo-OS installed is connected already.

The page can also turn on a Netflix-style theme for the Jellyfin web client. The theme is written for the Modern layout of Jellyfin 12 and lives in `internal/jellyfin/netflix.css`. The manager adds it to the custom CSS of Jellyfin between two marker comments, so your own custom CSS stays. The TV and phone apps do not use custom CSS.
