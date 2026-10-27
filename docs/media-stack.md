# Media stack

Prowlarr, Radarr, Sonarr, Bazarr, qBittorrent, Seerr, Recyclarr and FlareSolverr in one LXC with Docker. qBittorrent, Prowlarr and FlareSolverr go through ProtonVPN via Gluetun.

## Before you start

Install Jellyfin first, from the Apps page or in another way: the installer of the media stack finds a container named `jellyfin` and connects it. See [jellyfin.md](jellyfin.md).

1. Connect the Videos share with your movies and series on the Storage page, with write access. See [media-folder.md](media-folder.md). If Jellyfin already uses a Videos share, the form chooses the same one.
2. Create a ProtonVPN WireGuard key with "NAT-PMP (Port Forwarding)" on.
3. Optional: create a Jellyfin API key (Dashboard > API Keys).
4. Optional: make a free account on [opensubtitles.com](https://www.opensubtitles.com). Not opensubtitles.org: that is the old site, and Bazarr can't use it without a paid VIP account.

## Install

Install it on the Apps page. The host agent runs the installer from the release bundle, in its own container, and the page shows the log. Telegram tells you when it is done.

If the install fails, it removes the container and the media mount it made. Then:

- **Try again** runs the install again with the same answers.
- **Change answers** opens the form with your answers. Leave the WireGuard key, the password, the Jellyfin API key and the SMB password empty to keep them. The SMB password only stays for the same SMB user on the same NAS.

The answers stay on the Proxmox host in a file that only root can read, for 24 hours or until the install works. The keys and passwords never go back to the browser. "Remove them now" deletes them at once.

You can also run `stacks/arr/install.sh` from the bundle as root on the Proxmox host. It asks the same questions.

## Folders

Movies and series each get their own folder on the Videos share. The form asks for both names:

| Folder | Default  | On the NAS      | In the apps and Jellyfin |
| ------ | -------- | --------------- | ------------------------ |
| Movies | `movies` | `Videos/movies` | `/data/media/movies`     |
| Series | `series` | `Videos/series` | `/data/media/series`     |

Radarr puts movies in the first folder and Sonarr puts series in the second. With a Jellyfin API key, the installer adds a "Movies" and a "Series" library for them. If a folder already has files, Jellyfin shows them. To add them to Radarr or Sonarr, use Library Import in those apps.

Media stacks that were installed before you could choose the folders use `movies` and `tv`.

## What the installer does

- checks that the containers can write to the movies and series folders on the Videos share, and gives the share to the new LXC as `/data/media`, and to Jellyfin read-only when it does not have it yet;
- keeps downloads on a local disk (`/data/downloads`);
- connects all apps: logins, root folders, qBittorrent, Prowlarr sync, FlareSolverr, subtitles and Jellyfin libraries.

FlareSolverr is a switch in the form, on by default. It runs a browser for indexers behind Cloudflare, which takes a few hundred MB of memory. Turn it off when your indexers don't need it. It is then a Docker Compose profile that doesn't start, and Prowlarr gets no FlareSolverr proxy. Indexers that need it get the `flaresolverr` tag in Prowlarr. A stack from before the switch keeps FlareSolverr after an update.

After the install, the Apps page shows links to each app. Then add your indexers in Prowlarr.

## Subtitles

Bazarr adds subtitles in your languages to every new movie and episode. The installer turns on these sources:

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

Anime goes through the same Sonarr and Radarr and the same folders, with settings of its own:

- **Trackers.** With **Anime trackers** on in the form, the installer adds the public trackers Nyaa.si and Anime Tosho to Prowlarr, which passes them on to Sonarr and Radarr. They need no account. When one can't be added, the install goes on and its log says so; add it in Prowlarr yourself.
- **Quality.** Recyclarr syncs the TRaSH Guides profile **[Anime] Remux-1080p** into Sonarr and into Radarr, next to the normal profiles. It scores the anime release groups, and it prefers BluRay, then the best web releases. One Sonarr has one set of quality sizes, so anime series use the series sizes; the same goes for films in Radarr.
- **Japanese audio.** Both anime profiles reject a release that is only dubbed: TRaSH's "Dubs Only" format scores -10000. Japanese releases and dual audio (Japanese and English) pass. Most anime releases bring English subtitles in the file, and Bazarr adds them where they're missing. A dub that doesn't say so in its name can still slip through; delete it in Sonarr or Radarr and search again.
- **Requests.** Seerr knows anime by TMDB's "anime" keyword. It sends anime series to Sonarr as the anime series type, which numbers episodes the anime way, with the anime profile. For films, Seerr has no anime profile, so the installer adds an override rule to Radarr: the anime keyword (210024) gets the anime profile.
- **The first sync.** The installer runs Recyclarr once before it sets up Seerr, so both anime profiles exist. When that sync fails, for example because GitHub can't be reached, Recyclarr tries again every day, anime uses the normal profiles, and the log says so. Then pick the anime profile in Seerr under **Settings > Services > Sonarr**, and add the rule for films under **Settings > Services** with **New Override Rule**: the Radarr service, the keyword "anime", and the quality profile [Anime] Remux-1080p.

A media stack from before this change gets the Radarr anime profile with its next update, when Recyclarr syncs. Its Seerr is already set up, so add the override rule for films yourself, as above.

Which audio and subtitles play is up to the player. Set the language preferences in Jellyfin, or in Kodi on the TV.

## VPN settings

To use other server countries or a new WireGuard key, click **VPN** on the Apps page. Leave the key empty to keep the current key. A new key needs "NAT-PMP (Port Forwarding)" on, like the first one.

Iglo-OS writes the new settings in `/opt/arr/.env` in the container, and restarts Gluetun, qBittorrent, Prowlarr and FlareSolverr, when it runs. Downloads stop for about a minute. If the VPN does not connect with the new settings, Iglo-OS puts the old settings back and shows the VPN log.

The key goes to the container on stdin. It never goes back to the browser.

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
