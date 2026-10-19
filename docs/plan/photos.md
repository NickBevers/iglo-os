# Photos

The photo library lives on the Umbrel. The phone adds new photos by itself.

## Immich

[Immich](https://immich.app) works like Google Photos, but on your own hardware. It has apps for iOS and Android that back up the camera roll automatically.

## Where it runs

Immich runs in its own container on the mini PC, not on the Umbrel. Then it uses the GPU of the mini PC, and it is part of the backups, the updates and Tailscale of Iglo-OS. The app store of the Umbrel also has Immich, but we keep the Umbrel for files only.

| What | Where | Why |
| --- | --- | --- |
| Originals of photos and videos | Umbrel, `Photos/Immich` | The 8 TB are there. |
| Database (Postgres) | SSD of the mini PC | Immich warns against a database on a network share. |
| Thumbnails | SSD of the mini PC | Scrolling through the library stays fast. |

## The GPU

The UHD 770 helps Immich in two ways:

- **Machine learning**, for faces and for search like "beach" or "dog". This uses OpenVINO. The Immich docs list Intel GPUs as supported, but call it experimental. OpenVINO also uses more memory than the CPU does.
- **Videos from the phone** are converted with Quick Sync, so they play everywhere.

The container needs `/dev/dri`, like Jellyfin. Check that the GPU is used with `intel_gpu_top` on the host. Source: [Immich: machine learning hardware acceleration](https://docs.immich.app/features/ml-hardware-acceleration).

The first import of a big library takes a long time, because each photo goes through machine learning. That happens once.

## Away from home

The phone backs up over Tailscale, so the phone needs the Tailscale app. Immich then gets its own Tailscale Service, like Seerr and Jellyfin. See [tailscale.md](../tailscale.md).

## A second copy

Photos can't be downloaded again. Make a second copy before you delete photos from the phone. See [storage.md](storage.md#a-second-copy-of-the-photos).

## In Iglo-OS

- a Photos app in the catalog, with an installer, like Jellyfin;
- the form asks for the photos folder on the Umbrel;
- a Tailscale Service `svc:immich`, and a LAN name `immich.iglo.local`.
