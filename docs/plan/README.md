# The plan for Iglo-OS

Iglo-OS started from [DriesH/homelab](https://github.com/DriesH/homelab) v0.10.1. This folder describes what we want it to become, and what we decided along the way. The other files in `docs/` describe what Iglo-OS does today.

## Current implementation versus these planning notes

The original decisions and delivery order below are historical planning material.
The current branch adds [scoped SMB bindings](../scoped-storage.md),
[Photos installation and recovery](../photos.md), [Music/FileFlows workers](../workers.md)
and expanded [background-work controls](../background-work.md). Gaming has an
[experimental Wolf/ES-DE test path](../gaming-poc.md), not a production installer.
See the [main README](../../README.md#storage-processing-and-gaming) for the supported
entry points, manual setup and hardware-validation boundaries. Deployment-specific
hardware choices and retained workloads belong in the separate setup documentation.

## What Iglo-OS will do

A mini PC with Proxmox runs all apps. An Umbrel device on the network keeps all files. Iglo-OS manages both from one web app.

- **Video:** search a catalog, request a movie, series or anime, and it downloads and shows up in Jellyfin. Anime is the main use. See [video.md](video.md).
- **Music:** a separate library, with the cover art on screen while a song plays, like YouTube Music. Videos only when you ask for them. See [music.md](music.md).
- **Your own media:** drop files in a folder, and they become a format that plays everywhere. See [own-media.md](own-media.md).
- **Photos:** the phone backs up its photos to the Umbrel. See [photos.md](photos.md).
- **Playback:** on the LG TV without internet, and on the PC and phone, at home and away. See [playback.md](playback.md).
- **Minecraft:** later, with one CPU core of its own. See [minecraft.md](minecraft.md).

The hardware is in [hardware.md](hardware.md), the files and backups in [storage.md](storage.md). Whether an external GPU helps is in [egpu.md](egpu.md).

## Decisions

| Topic | Decision |
| --- | --- |
| Name | Iglo-OS. Identifiers use `iglo`: the Go module, `iglo.local`, `IGLO_*`, `iglo@pve`. |
| Git | Fresh history, in a new repo `NickBevers/iglo-os`. |
| Storage | Everything that matters lives on the Umbrel, over SMB. The Umbrel runs no apps for us. The mini PC only gets the folders it needs, each as its own share. |
| Second copy | Photos and family videos also go to an external backup disk. The Umbrel itself survives one broken disk out of three. |
| Own media | Converted, then moved to `Videos/Home`. The original goes away after a working conversion. |
| Apps | All apps run on the mini PC, in containers that Iglo-OS makes. |
| Music source | yt-dlp first. Lidarr with torrents later. |
| Transcoding | Jellyfin transcodes live with Intel Quick Sync. FileFlows makes your own files TV-safe once. |
| Photos | Immich on the mini PC, with the photos on the Umbrel. |
| TV | The LG never goes online, not even on the LAN. A Raspberry Pi 5 with LibreELEC and Kodi plays the media over HDMI. No Apple or Google box. |
| Phones | Android first, iPhone second. |
| Remote access | Tailscale only. No ports open on the router. |
| Minecraft | Later. It gets one P-core that no other container uses. |
| External GPU | Not now. The iGPU of the mini PC is enough. See [egpu.md](egpu.md). |

## Order

1. **Rename and restart.** Done. Still to do: a first release v0.1.0.
2. **The Umbrel over SMB.** The SMB mount for the media is done, and tested up to a real Samba share. Still to do: a test on the mini PC with the Umbrel, and Proxmox backups on the Umbrel.
3. **Video with anime.**
4. **Playback on the TV.**
5. **Photos with Immich.**
6. **Your own media through FileFlows.**
7. **Music with yt-dlp.** Lidarr later.
8. **Minecraft with its own P-core.**

Steps 1 to 3 can start before the mini PC arrives. `make dev` runs a fake Proxmox, so most of the work can be built and tested on a laptop.

## Status of the rename

Done:

- `homelab` became `iglo` in the Go module, the binaries, the systemd units, the hostname, the Proxmox user and role, the env vars and the Proxmox tag.
- The UI and the docs say "Iglo-OS".
- `LICENSE` keeps the copyright line of Dries Heyninck, as MIT asks, and adds Nick Bevers.
- Versions start again at v0.1.0.
- `make test` passes.
- The repo is [NickBevers/iglo-os](https://github.com/NickBevers/iglo-os). It is public, so self-update reads the releases without a token.
- A new signing key, made on 19 October 2026. The public key is in `internal/release/signing.pub`. The private key went straight into the `IGLO_SIGNING_KEY` secret of the repo, and is nowhere else. To replace it, run:

  ```sh
  go run ./cmd/iglo-release keygen -public internal/release/signing.pub | gh secret set IGLO_SIGNING_KEY
  ```

Still to do:

- A first release: push the tag `v0.1.0`. The install command in the main README already says v0.1.0.
- The test fixtures still use names like `Dries_H`. They are only sample data.

## Open questions

- Where does the external backup disk plug in, and which tool makes the nightly copy? See [storage.md](storage.md#a-second-copy-of-the-photos).
