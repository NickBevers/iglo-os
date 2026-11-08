# Media stack

Prowlarr, Radarr, Sonarr, Bazarr, qBittorrent, Seerr, Recyclarr and FlareSolverr in one LXC with Docker. qBittorrent, Prowlarr and FlareSolverr go through ProtonVPN or Private Internet Access (PIA) via Gluetun.

## Before you start

Install Jellyfin first, from the Apps page or in another way: the installer of the media stack finds a container named `jellyfin` and connects it. See [jellyfin.md](jellyfin.md).

1. Connect the Videos share with your movies and series on the Storage page, with write access. See [media-folder.md](media-folder.md). If Jellyfin already uses a Videos share, the form chooses the same one.
2. For ProtonVPN, create a WireGuard key with "NAT-PMP (Port Forwarding)" on. For PIA, have your username (like `p1234567`) and password, and your dedicated IP token if you have a static IP.
3. Optional: create a Jellyfin API key (Dashboard > API Keys).
4. Optional: make a free account on [opensubtitles.com](https://www.opensubtitles.com). Not opensubtitles.org: that is the old site, and Bazarr can't use it without a paid VIP account.

## Install

Install it on the Apps page. The host agent runs the installer that the last deploy put on the host, in its own container, and the page shows the log. ntfy tells you when it is done.

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
- keeps downloads on a local disk (`/data/downloads`), 120 GB by default. Grow it later on the host with `pct resize <container> mp1 +<size>G`;
- connects all apps: logins, root folders, qBittorrent, Prowlarr sync, FlareSolverr, subtitles and Jellyfin libraries.

FlareSolverr is a switch in the form, off by default. It runs a browser for indexers behind Cloudflare, which can take up to 1 GB of memory. Turn it on when an indexer needs it, at the install or later (see [FlareSolverr](#flaresolverr)). While it is off, it is a Docker Compose profile that doesn't start, and Prowlarr gets no FlareSolverr proxy. Indexers that need it get the `flaresolverr` tag in Prowlarr. A stack from before the switch keeps FlareSolverr after an update.

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

## Languages

Open **Languages** on the media stack card to choose subtitle languages for anime, Dutch titles, series and films. Choose up to 10 languages for each kind. Series and Films always need at least one language. **None** is only for Anime and Dutch. With None, anime stays excluded by series type and the anime tag, as before, because the releases bring the official subtitles. Dutch titles are excluded by the `dutch` tag when their list is empty.

Series use Bazarr profile 1. Changing Series changes profile 1. Films share profile 1 when their languages match Series. Otherwise they use **Films (Iglo-OS)**.

With subtitles for Anime or Dutch, Bazarr's tag profiles are on. Bazarr itself keeps titles with that tag on that profile, including titles whose profile you set by hand. Any tag profile you had before also starts to apply. When both lists are empty, the tag profile switches stay as they were.

Audio is fixed for each kind because Recyclarr resets the language scores every day. Anime uses Japanese, its original language. Series and films use their original language. Dutch titles use Dutch audio, including dubs. The Dutch rule uses custom formats and a separate quality profile in Sonarr and Radarr. `Dutch (Iglo-OS)` is copied once from WEB-1080p in Sonarr or HD Bluray + WEB in Radarr. It does not follow later Recyclarr changes to those profiles. Both accept Dutch dubs and prefer a Dutch track.

Titles whose original language is Dutch get the Dutch profile and the `dutch` tag through a Seerr rule. A failed Seerr rule only warns, so the language change still reports success. For a dub such as De Smurfen, whose original language is French, choose the **Dutch (Iglo-OS)** profile and the `dutch` tag in Seerr's request dialog.

The settings are saved in `/opt/arr/iglo-media.json`. A media stack from before this feature needs **Update** first. This behavior follows Sonarr v4.0.20.3014, Radarr v6.4.4.10685, Bazarr v1.6.2, Seerr v3.5.1 and Recyclarr v8.8.0, checked 7 November 2026.

## Films in 4K when available

Open **Languages** on the media stack card and turn on **Films in 4K when available**. It is off by default. The setting is `uhdFilms` in `iglo-media.json`, separate from the size limit `sizes.films4k`. Old files without the switch read as false. With it off, the Recyclarr template, HD profiles and Seerr defaults keep their old behavior.

New films then use **4K when available (Iglo-OS)** in Radarr. It prefers these qualities, in order:

1. Bluray-2160p.
2. WEB 2160p, with WEBDL-2160p and WEBRip-2160p grouped together.
3. Bluray-1080p.
4. WEB 1080p, with WEBDL-1080p and WEBRip-1080p grouped together.

Remux and 720p are excluded. If no suitable 4K release exists, Radarr takes 1080p. A 4K release above the 4K size limit is not suitable: Radarr rejects any release above the maximum of its quality (`AcceptableSizeSpecification` in Radarr v6.4.4.10685, checked 2026-11-08) and takes 1080p within the HD limit instead. Radarr has no 1440p quality, so 1080p is the fallback. Upgrades stay on until Bluray-2160p, with the guide's cutoff format score of 10000. A later suitable 4K release can replace the 1080p copy. The HD and 4K size limits still apply to their own qualities.

Dolby Vision releases without an HDR fallback score -10000 and are skipped. The Raspberry Pi 4 with Kodi can play 4K HEVC, HDR10 and HDR10+, but cannot play Dolby Vision alone. HDR and HDR10+ keep their TRaSH scores. DV Boost is not selected. Format matching follows release names and the guide's rules, so check a release in Kodi if its name is misleading. Atmos has no extra preference here.

Recyclarr creates and keeps this profile from **UHD Bluray + WEB**, TRaSH ID `64fb5f9858489bdac2af690e27c8f42f`. The HD profile remains separate. The v8.8.0 schema allows `trash_id` together with `name`, `qualities`, `quality_sort`, `min_format_score` and `upgrade`. We use those overrides and assign extra formats by profile name. The guide's release tiers, repacks and default unwanted formats keep their guide scores. The minimum score stays zero, so a clean 1080p H.264 WEB-DL or Bluray encode with no positive formats is accepted. It matches neither x265 format. Golden Rule UHD keeps its x265 (no HDR/DV) penalty, including for 4K SDR x265 re-encodes. Iglo-OS also assigns x265 (HD) a score of -10000. This matches the existing HD film profile on the live host and blocks 1080p x265 releases even when they have HDR. The UHD group alone does not select x265 (HD) by default. Optional SDR penalties are not selected. Their required 2160p condition would not match the fallback anyway.

These choices were checked offline on 2026-11-08 against the supplied Recyclarr v8.8.0 quality profile, custom format and profile reference schemas, the TRaSH UHD and HD profile snapshots, the HDR format groups, unwanted formats and Golden Rule groups, and the HDR, HDR10+, DV and SDR format definitions. The schemas and relevant guide snapshots are kept in `internal/stacks/arr/testdata/films4k`. The guide clone is not pinned and can change later. HDR scores in this snapshot are 500, HDR10+ Boost 100 and DV without fallback -10000. On the live host on 2026-11-08, the HD film profile scored x265 (HD) at -10000 with a minimum score of zero. Iglo-OS fixes that HD penalty and the DV penalty in the rendered config. x265 (no HDR/DV) keeps its guide score through the default UHD group. Other guide scores continue to follow Recyclarr's guide clone.

Dutch films follow the switch through **Dutch 4K (Iglo-OS)**. It is copied once from the new 4K profile and gets the same Dutch audio changes as **Dutch (Iglo-OS)**. Like the existing Dutch profile, it does not follow later Recyclarr changes except for the two x265 penalties. Saving media settings copies those penalties from the Recyclarr profile, including into an existing Dutch 4K profile. A separate profile keeps existing Dutch films from gaining 4K upgrades when you turn the switch on. Seerr's Dutch film rule selects it. For a Dutch dub, choose **Dutch 4K (Iglo-OS)** and the `dutch` tag in the request dialog. Dutch series and anime keep their existing profiles.

Seerr keeps one normal Radarr server with `is4k: false`. Its `activeProfileId` and `activeProfileName` select the new profile. Make a normal film request in Seerr. Radarr chooses the resolution through that profile. Seerr must be set up and reachable when you turn the switch on or off. Iglo-OS checks its key and Radarr service before changing any app. Other media settings can still be saved while Seerr is unavailable. A failed Dutch override rule still only warns, as with language settings.

Existing films keep their profiles. To opt one in, open that film in Radarr, choose **Edit**, set **Quality Profile** to **4K when available (Iglo-OS)**, or **Dutch 4K (Iglo-OS)** for Dutch audio, and save. Use Radarr's search for an immediate search. Turning the switch on itself never changes film records or starts a library search.

When you turn it on, Iglo-OS saves Seerr's current film profile ID and name in `iglo-media.json`. Turning it off restores that profile if it still exists. Otherwise new films go back to **HD Bluray + WEB**. Dutch films return to **Dutch (Iglo-OS)**. The 4K profiles are removed only when no film, import list or collection uses them and no Seerr service, override rule or movie request still points at them. Radarr keeps a new collection on its film's profile, so a collection can keep a 4K profile after you move its films to HD. Each profile is checked separately. A failed deletion does not stop cleanup of the other profile. The log says which profile was kept and why. A kept profile retains its current settings after its Recyclarr config is removed. Existing films on it can still upgrade. Change those films' profiles in Radarr if you also want their upgrades to stop. The stored 4K size limit stays when the switch is off. Unused HDR, DV and HDR10+ custom formats can remain at score zero in other profiles.

Install, Update, VPN changes and FlareSolverr changes use `iglo-arr media render` to combine the saved switch and size limits with the template before Recyclarr starts. Turning the switch on renders and syncs the film profile before setting Seerr's default. Turning it off uses one Recyclarr sync in the size step, including when sizes also change. A failed sync or missing profile fails the change. A later failure restores the previous YAML, Seerr default and owned Dutch film rule. Harmless new profiles can remain after a failed change, without changing any film records.

Cleanup follows Radarr v6.4.4.10685 `QualityProfileService.Delete` and `RefreshMovieService.cs:136-147`, as supplied in the review on 2026-11-08. The local Seerr v3.5.1 `MediaRequest.ts:266-346` stores a request's profile ID, and `sub.ts:263-266` uses it on approval. Its request list route and status constants were not supplied. Iglo-OS assumes `GET /api/v1/request?filter=all&take=100&skip=N`, with `pageInfo.results` as the total. It reads every page and conservatively keeps profiles referenced by any movie request, including pending and approved requests. A failed read or missing pagination keeps the profiles and logs the reason.

## Maximum download size

Open **Languages** on the media stack card for **Maximum download size**. Series, Dutch series and anime share one limit because Sonarr's quality definitions apply to all series. Films have separate Radarr limits for **Films in HD (1080p and lower)** and **Films in 4K**. The choices show sizes per 24-minute episode or 2-hour film. They use decimal GB (1000 MB), rounded down to whole MB per minute of runtime. The 4K choices are 40 GB (333 MB/min), 25 GB (208 MB/min), 20 GB (166 MB/min) and 15 GB (125 MB/min). A saved value outside the choices appears as **Custom** and stays available until you close the dialog.

4K films are only downloaded when the media settings allow 4K. A size limit does not enable 4K downloads.

Releases above the limit are skipped. If only larger releases exist, nothing is downloaded until a smaller one appears. Files you already have are not touched. Smaller releases save space in both the seeding copy on the download disk and the library copy on the NAS.

The settings file stores `sizes.series`, `sizes.films` and `sizes.films4k` as integer MB per minute. Radarr qualities with `2160p` in their name use `films4k`. All other Radarr qualities use `films`. Sonarr keeps its shared series limit at every resolution. Zero means **No limit** for that kind, which keeps the guide's sizes. Old files without `films4k` read as zero, so their HD limit does not cap 4K after these settings are applied. Series and HD film limits must be zero or between 10 and 400. The 4K film limit must be zero or between 10 and 1000.

Iglo-OS writes maximum and preferred size overrides in `/opt/arr/config/recyclarr/recyclarr.yml` for qualities above the relevant limit. Every override also gives an explicit minimum: the smaller of the guide minimum and half that limit. This keeps a later guide increase from making the minimum exceed the maximum. Integer limits have an exact half with at most one decimal place.

Recyclarr carries the guide overrides on its daily sync. Iglo-OS prefers names and sizes from its local TRaSH guide clone under `/opt/arr/config/recyclarr`. The embedded snapshots are a fallback when the clone is absent. The guide updates independently of the pinned Recyclarr image. The loader finds the clone's `docs/Sonarr/quality-size` and `docs/Radarr/quality-size` folders without relying on its cache directory name.

Iglo-OS also caps every quality definition that the guide leaves alone, including SD qualities, through the Sonarr and Radarr APIs after the sync. It uses the smaller of the app's default maximum and your limit, and keeps preferred and minimum sizes below that maximum. **No limit** restores those qualities to the pinned app defaults, including unlimited values stored as `null`. The defaults and API rules follow Sonarr v4.0.20.3014 and Radarr v6.4.4.10685, checked 2026-11-08.

Saving a changed limit runs the stack's existing `docker exec recyclarr recyclarr sync` command immediately for both instances, after the language steps finish. A failed sync, config parse error or missing successful size result fails the settings change. The YAML and live API sizes are restored if a later size update or settings save fails. On install and update, `iglo-arr media render` combines `/opt/arr/recyclarr.template.yml` with the saved choices before Recyclarr starts. If those choices cannot be read, it warns and uses the plain template so Update can continue. VPN and FlareSolverr changes use the same step. With no limit, the rendered quality definition has no `qualities` key. This uses the per-quality override schema of Recyclarr v8.8.0, checked 2026-11-08.

## FlareSolverr

The card of the media stack on the Apps page says whether FlareSolverr runs. Click **FlareSolverr** to turn it on or off. It needs no password and no reinstall, and the other apps keep running.

- **On** starts the `flaresolverr` service, waits until Prowlarr can reach it, and adds the FlareSolverr indexer proxy with the `flaresolverr` tag.
- **Off** removes that proxy from Prowlarr, then stops and removes the service. A FlareSolverr proxy that points elsewhere stays.

Both write `FLARESOLVERR` and `COMPOSE_PROFILES` in `/opt/arr/.env`, so an update keeps the choice. Indexers keep the `flaresolverr` tag when it is off, and use FlareSolverr again once it is back on. Prowlarr only removes a tag that nothing uses. The image stays in the container after off, so turning it on again needs no download.

`iglo-arr flaresolverr on|off` talks to Prowlarr with the API key that the installer wrote in `.env`. This follows Prowlarr v2.7.0.5723 (`POST` and `DELETE /api/v1/indexerProxy`, and the test that a save runs first), checked on 29 October 2026.

If Prowlarr does not take FlareSolverr, the installer stops the service again and the switch stays off. If it can't remove the proxy, FlareSolverr keeps running.

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
3. The host seals each secret with `systemd-creds` in `/etc/iglo/credentials/<container>`: bound to the TPM2 chip and the host key when the host has a TPM2, otherwise to the host key (systemd v257, `--with-key=auto`). The seal is bound to no PCRs, so a BIOS update or a change of the Secure Boot keys leaves the secrets readable.
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
4. It downloads the new images and restarts the apps that changed. Every image is pinned to a digest, so an update gets exactly the images that this Iglo-OS release was tested with; the version is in a comment next to each image in `compose.yaml`.

The settings in the apps stay, because Iglo-OS does not keep your passwords. If the update fails, Iglo-OS rolls the container back to the snapshot.

**Remove** removes the container with its disks and snapshots. This includes the downloads disk, so downloads that did not finish are lost. Your movies and series stay on the share, and Jellyfin keeps its libraries. The backups of the container stay, so you can restore it on the Backups page.

Iglo-OS only updates and removes a container with the tags `iglo` and `media`. The installer gives it these tags.
