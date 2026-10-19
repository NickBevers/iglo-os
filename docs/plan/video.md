# Video and anime

Most of this exists in Iglo-OS already. See [media-stack.md](../media-stack.md) and [jellyfin.md](../jellyfin.md).

## How a request flows

1. You find a movie, series or anime in **Seerr** and request it.
2. Seerr sends it to **Radarr** (movies) or **Sonarr** (series and anime).
3. **Prowlarr** searches the indexers.
4. **qBittorrent** downloads it behind ProtonVPN, through Gluetun.
5. Radarr or Sonarr renames the file and copies it to the Umbrel.
6. **Jellyfin** finds it and shows it in the right library.
7. **Bazarr** adds subtitles that are missing.

## What changes for us

### Storage

The media folder is on the Umbrel, over SMB. See [storage.md](storage.md).

### Jellyfin runs on the mini PC

The Umbrel could run Jellyfin, but it only keeps files. Jellyfin needs the GPU of the mini PC for transcoding. Anime often has styled ASS subtitles, and a client that can't draw them needs Jellyfin to burn them into the picture. Burning in subtitles means a transcode.

The LG Jellyfin app is the Jellyfin web client in a TV app, and it can draw ASS subtitles itself. Kodi on the TV box draws them too, with libass. See [playback.md](playback.md).

### Anime

Anime gets its own folders, quality profiles and indexers:

- **Folders:** `media/anime` for series and `media/anime-movies` for movies, next to `movies` and `series`.
- **Sonarr:** the series type **Anime**. It uses absolute episode numbers, the way most anime releases are numbered.
- **Quality profiles:** the anime profiles of [TRaSH Guides](https://trash-guides.info), synced by Recyclarr. They prefer good release groups and dual audio when you want it. This goes in `deploy/stacks/arr/recyclarr.yml`.
- **Indexers:** Nyaa and AnimeTosho in Prowlarr. Both are public, so you need no account.
- **Seerr:** Seerr has separate anime settings for each Sonarr and Radarr server: a root folder and a quality profile. The installer fills them in, so an anime request lands in the anime folder.
- **Jellyfin:** the libraries **Anime** (series) and **Anime movies**, next to Movies and Series. A metadata plugin like AniList can give better anime titles and artwork. That is optional.
- **Watchlist (optional):** Sonarr has import lists for AniList and MyAnimeList. Anime on your list there then downloads by itself. Check that these lists are in the Sonarr version we install.

Most of this is a change to `internal/stacks/arr/configure.go`, `deploy/stacks/arr/install.sh` and the install form in `web/src/features/apps/`.

## Open questions

- Dual audio or Japanese only? This decides the anime quality profile.
- Which subtitle languages does Bazarr look for?
