# Media stack

Prowlarr, Radarr, Sonarr, Bazarr, qBittorrent, Seerr, Recyclarr and FlareSolverr in one LXC with Docker. qBittorrent, Prowlarr and FlareSolverr go through ProtonVPN or Private Internet Access (PIA) via Gluetun.

## Before you start

Install Jellyfin first, from the Apps page or in another way: the installer of the media stack finds a container named `jellyfin` and connects it. See [jellyfin.md](jellyfin.md).

1. Connect the Videos share with your movies and series on the Storage page, with write access. See [media-folder.md](media-folder.md). If Jellyfin already uses a Videos share, the form chooses the same one.
2. For ProtonVPN, create a WireGuard key with "NAT-PMP (Port Forwarding)" on. For PIA, have your username (like `p1234567`) and password, and your dedicated IP token if you have a static IP.
3. Optional: create a Jellyfin API key (Dashboard > API Keys).
4. Optional: make a free account on [opensubtitles.com](https://www.opensubtitles.com). Not opensubtitles.org: that is the old site, and Bazarr can't use it without a paid VIP account.

## Install

Install it on the Apps page. The host agent runs the installer from the release bundle, in its own container, and the page shows the log. Telegram tells you when it is done.

If the install fails, it removes the container, the media mount and the sealed secrets it made. Then:

- **Change answers** opens the form with your answers. Leave the password, the Jellyfin API key and the SMB password empty to keep them. The SMB password only stays for the same SMB user on the same NAS. The VPN login is never kept for a retry, so enter it again; that is also why the media stack has no **Try again**.

The answers stay on the Proxmox host in a file that only root can read, for 24 hours or until the install works. The keys and passwords never go back to the browser. "Remove them now" deletes them at once.

You can also run `stacks/arr/install.sh` from the bundle as root on the Proxmox host. It asks the same questions.

## Folders

Movies, series, anime and anime movies each get their own folder on the Videos share. The form asks for the names:

| Folder       | Default        | On the NAS            | In the apps and Jellyfin   |
| ------------ | -------------- | --------------------- | -------------------------- |
| Movies       | `movies`       | `Videos/movies`       | `/data/media/movies`       |
| Series       | `series`       | `Videos/series`       | `/data/media/series`       |
| Anime        | `anime`        | `Videos/anime`        | `/data/media/anime`        |
| Anime movies | `anime-movies` | `Videos/anime-movies` | `/data/media/anime-movies` |

Radarr puts movies in the movies folder and anime movies in their own, and Sonarr does the same for series and anime. Each folder is a root folder in Radarr or Sonarr. With a Jellyfin API key, the installer adds a library for each: Movies, Series, Anime and Anime Movies. Leave the anime folders empty to keep anime in the series and movies folders, like a stack from before they existed. If a folder already has files, Jellyfin shows them. To add them to Radarr or Sonarr, use Library Import in those apps.

Media stacks that were installed before you could choose the folders use `movies` and `tv`.

## What the installer does

- checks that the containers can write to the movies and series folders on the Videos share, and gives the share to the new LXC as `/data/media`, and to Jellyfin read-only when it does not have it yet;
- keeps downloads on a local disk (`/data/downloads`);
- connects all apps: logins, root folders, qBittorrent, Prowlarr sync, FlareSolverr, subtitles and Jellyfin libraries.

FlareSolverr is a switch in the form, on by default. It runs a browser for indexers behind Cloudflare, which takes a few hundred MB of memory. Turn it off when your indexers don't need it. It is then a Docker Compose profile that doesn't start, and Prowlarr gets no FlareSolverr proxy. Indexers that need it get the `flaresolverr` tag in Prowlarr. A stack from before the switch keeps FlareSolverr after an update.

After the install, the Apps page shows links to each app. Then add your indexers in Prowlarr.

## Subtitles

Bazarr adds subtitles in your languages to every new movie and episode, except anime (see below). The installer turns on these sources:

- **Embedded Subtitles**: subtitles that are already inside the video file;
- **Podnapisi**: free, no account;
- **OpenSubtitles.com**: only when you give its username and password in the form. It has by far the most subtitles. The free account has a daily download limit, which is enough for normal use.

The OpenSubtitles.com password is a secret like the other passwords: it goes to the container on stdin and is never written to a file there. Bazarr keeps it in its own settings.

To add OpenSubtitles.com later: in Bazarr, open **Settings > Providers**, add **OpenSubtitles.com** (not .org), and enter your username, not your email address. Then **Wanted > Search All** searches at once.

## Seerr

Give a Jellyfin admin username and password in the form, and the installer does the setup of Seerr:

- that Jellyfin account becomes the owner of Seerr, so you log in to Seerr with it;
- the movie and series libraries of Jellyfin are turned on;
- Radarr and Sonarr are added, with the quality profile from Recyclarr and your movies and series folders. Anime gets its own profile in both, see below.

Without a Jellyfin admin, open Seerr after the install and do its setup yourself. Until then, anyone on your network can open it. If Seerr is already set up, the installer leaves it alone.

The Jellyfin admin password is a secret like the other passwords: it goes to the container on stdin and is never written to a file there. Seerr then makes its own Jellyfin API key.

The agent checks every answer before it writes them to a file that only root can read. The installer deletes that file when it has read it.

## Anime

Anime goes through the same Sonarr and Radarr, into its own folders, with settings of its own:

- **Trackers.** With **Anime trackers** on in the form, the installer adds the public trackers Nyaa.si and Anime Tosho to Prowlarr, which passes them on to Sonarr and Radarr. They need no account. When one can't be added, the install goes on and its log says so; add it in Prowlarr yourself.
- **Quality.** Recyclarr syncs the TRaSH Guides profile **[Anime] Remux-1080p** into Sonarr and into Radarr, next to the normal profiles. It scores the anime release groups, and it prefers BluRay, then the best web releases. One Sonarr has one set of quality sizes, so anime series use the series sizes; the same goes for films in Radarr.
- **Japanese audio.** Both anime profiles reject a release that is only dubbed: TRaSH's "Dubs Only" format scores -10000. Japanese releases and dual audio (Japanese and English) pass. The profiles read release names, not the tracks in the file, so a dub that doesn't say so in its name can still slip through; delete it in Sonarr or Radarr and search again. For a release group you don't know yet, check one scene with dialogue.
- **Subtitles.** Anime releases bring the official English subtitles in the file. Bazarr leaves anime alone: series of the anime type, and films with the anime tag. Jellyfin prefers an external subtitle, so a downloaded one would push the official subtitles aside. To get a subtitle for an anime that has none, search for it by hand in Bazarr; the exclusion only stops its automatic searches.
- **One download per episode.** A new episode lands from several web rippers within minutes. Sonarr waits 30 minutes for series with the anime tag, then takes the best release once. A release in TRaSH's anime web tier 1 to 3 is grabbed at once. Later, better releases (a BluRay, a fixed version) still replace it, as the profile allows.
- **Requests.** Seerr knows anime by TMDB's "anime" keyword. It sends anime series to Sonarr as the anime series type, which numbers episodes the anime way, with the anime profile and the anime tag, into the anime folder. For films, Seerr has no anime profile, so the installer adds an override rule to Radarr: the anime keyword (210024) gets the anime profile, the anime tag and the anime movies folder.
- **Seasons.** Sonarr numbers seasons as TheTVDB does, and TMDB often splits anime differently. The installer sets Seerr's metadata for anime to TheTVDB, so a request asks Sonarr for the right season. When TheTVDB can't be reached during the install, the log says so; set it later under **Settings > Metadata Providers**.
- **File names.** Recyclarr turns on renaming in Sonarr and Radarr with the TRaSH names for Jellyfin: the TVDB or TMDB ID in the folder, and SxxExx with the absolute number for anime. A release named "- 16" then can't be read as the wrong episode. Files that are already there keep their names.
- **The first sync.** The installer runs Recyclarr once before it sets up Seerr, so both anime profiles exist. When that sync fails, for example because GitHub can't be reached, Recyclarr tries again every day, anime uses the normal profiles, and the log says so. Then pick the anime profile in Seerr under **Settings > Services > Sonarr**, and add the rule for films under **Settings > Services** with **New Override Rule**: the Radarr service, the keyword "anime", and the quality profile [Anime] Remux-1080p.

A media stack from before these changes gets the Radarr anime profile and the file names with its next update, when Recyclarr syncs. The rest is set up during the install only. To add it by hand:

1. In Sonarr and Radarr, add a tag **anime** under **Settings > Tags**. For separate anime folders, make them on the share, add them as root folders in Sonarr and Radarr, and add an Anime and an Anime Movies library in Jellyfin.
2. In Sonarr, add a delay profile under **Settings > Profiles** for the tag anime: torrent delay 30 minutes, **Bypass if Above Custom Format Score** on, with 400.
3. In Seerr, under **Settings > Services > Sonarr**, pick the anime tag as **Anime Tags**, and the anime folder as the anime root folder. Add the rule for films as above, with the tag anime and the anime movies folder. Under **Settings > Metadata Providers**, set anime to TheTVDB.
4. In Bazarr, add **anime** to **Excluded Series Types** under **Settings > Sonarr**, and to **Excluded Tags** under **Settings > Radarr**.

Which audio and subtitles play is up to the player. Set the language preferences in Jellyfin, or in Kodi on the TV.

## VPN

The stack uses one of two providers. Both need port forwarding, because qBittorrent can't seed well without a forwarded port.

|                     | ProtonVPN                                                         | Private Internet Access (PIA)                       |
| ------------------- | ----------------------------------------------------------------- | --------------------------------------------------- |
| Login               | A WireGuard private key, made with "NAT-PMP (Port Forwarding)" on | Your PIA username and password                      |
| Locations           | Countries                                                         | Regions, like `DE Berlin`, or your dedicated IP     |
| Protocol in Gluetun | WireGuard                                                         | OpenVPN (Gluetun has no WireGuard for PIA, v3.41.3) |

The form lists only the countries and regions with port forwarding, from the server list of the Gluetun version the stack runs. Choose up to 10; Gluetun picks a server in one of them. `dev/vpnlocations` makes the list again for a new Gluetun version.

### A PIA dedicated IP

With a dedicated IP token (it starts with `DIP`), the location list gets **Dedicated IP**, which goes alone. Iglo-OS then asks PIA's API for the address and name of that server, writes an OpenVPN file for it in `/opt/arr/config/gluetun`, and gives Gluetun its custom provider with PIA's port forwarding. The VPN login is `dedicated_ip_` and the token, as in PIA's own [manual-connections](https://github.com/pia-foss/manual-connections) scripts; port forwarding logs in with your normal PIA account. PIA has no port forwarding for a dedicated IP in the US, so Iglo-OS refuses one there.

This follows Gluetun v3.41.3 and PIA's scripts at `645377e`, checked on 27 October 2026, and is untested with a real token.

### Changing the VPN

Click **VPN** on the Apps page to choose another provider, other locations or a new login. Leave a key, password or token empty to keep the current one. Switching providers needs the login of the new one.

Iglo-OS writes the new settings in `/opt/arr/.env` in the container, and restarts Gluetun, qBittorrent, Prowlarr and FlareSolverr, when it runs. Downloads stop for about a minute. If the VPN does not connect with the new settings, Iglo-OS puts the old settings back and shows the VPN log.

The form only learns which secrets the host keeps, never their values. Switching back to a provider you used before needs no new login: the host keeps the sealed secrets of both.

### How the VPN login is kept

The login never lies on a disk in plain text:

1. The browser sends it to Iglo-OS over HTTPS, and Iglo-OS to the host agent over its Unix socket.
2. The installer passes it to `iglo-arr` in the container on stdin, which checks it and sends back the files that Gluetun reads, also on stdout. Nothing goes through a file or the process list.
3. The host seals each secret with `systemd-creds` in `/etc/iglo/credentials/<container>`: bound to the TPM2 chip and the host key when the host has a TPM2, otherwise to the host key (systemd v257, `--with-key=auto`).
4. Before Proxmox starts the container, `iglo-secrets@<container>.service` unseals them into RAM, in `/run/iglo/secrets/<container>`. The container sees that folder read-only at `/mnt/iglo-secrets`, and Gluetun reads it as `/run/secrets`, which it prefers to environment variables (v3.41.3).
5. `.env` in the container has no secrets, and a backup of the container doesn't have the folder, so backups on the NAS hold no VPN login.
6. The answers kept for a retry leave the VPN login out.

Root on the running host, or in the container, can still read what Gluetun reads: any VPN client needs its login in usable form. The seal protects the disk, the NAS and the backups. A stack from before the sealed secrets keeps its WireGuard key in `.env` until its next update or VPN change: then the container gets its secrets folder and restarts once, and the key moves to the host, sealed.

## VPN kill switch

qBittorrent, Prowlarr and FlareSolverr share Gluetun's network. Gluetun's firewall blocks all their traffic while the VPN is down, so nothing leaks outside the tunnel. When the VPN drops, qBittorrent is also bound to `lo` until the VPN is back. Downloads then go on where they stopped, through the new VPN address.

qBittorrent writes no log file, so torrent names don't stay behind there.

## Update and remove

The Apps page shows "Update available" when the stack in the container is from an older Iglo-OS release.

**Update** does these steps:

1. It makes a snapshot of the container.
2. It updates the packages in the container.
3. It copies the `compose.yaml`, `recyclarr.yml` and `iglo-arr` of this Iglo-OS release into the container.
4. It downloads the new images and restarts the apps that changed.

The settings in the apps stay, because Iglo-OS does not keep your passwords. If the update fails, Iglo-OS rolls the container back to the snapshot.

**Remove** removes the container with its disks and snapshots. This includes the downloads disk, so downloads that did not finish are lost. Your movies and series stay on the share, and Jellyfin keeps its libraries. The backups of the container stay, so you can restore it on the Backups page.

Iglo-OS only updates and removes a container with the tags `iglo` and `media`. The installer gives it these tags.
