# Hardware

## The mini PC

| Part | What we plan |
| --- | --- |
| CPU | Intel Core i7-12700T: 8 P-cores with Hyper-Threading and 4 E-cores, so 20 threads. 35 W base power, up to 4.7 GHz. |
| GPU | Intel UHD 770, inside the CPU. |
| Memory | 32 GB DDR5, as two modules of 16 GB. |
| SSD | 512 GB or 1 TB NVMe, whichever the mini PC comes with. |
| Network | Wired. |

The mini PC is not bought yet. Nick aims for 32 GB of memory. The SSD comes with the mini PC, 512 GB or 1 TB, and can't be chosen.

## CPU

Linux usually numbers the threads of a hybrid Intel CPU like this:

| Logical CPUs | Cores |
| --- | --- |
| 0 to 15 | the 8 P-cores, 2 threads each |
| 16 to 19 | the 4 E-cores |

Check it on the mini PC itself with `lscpu -e`. The P-cores have a higher `MAXMHZ`. Minecraft needs this to get its own P-core. See [minecraft.md](minecraft.md).

The "T" in 12700T means a low power limit. One core still boosts high, which suits Minecraft. Long work on all cores, like encoding on the CPU, runs slower than on the desktop chip. That is fine, because the GPU does the video work.

## GPU (UHD 770)

The UHD 770 does video in hardware, with Intel Quick Sync:

| | H.264 | HEVC 8 and 10-bit | VP9 | AV1 |
| --- | --- | --- | --- | --- |
| Decode | yes | yes | yes | yes |
| Encode | yes | yes | yes | no |

Three apps use it. They can share it, because each container gets the same `/dev/dri/renderD128` device:

- **Jellyfin** for live transcoding. The Jellyfin installer of Iglo-OS already passes the GPU through.
- **Immich** for face recognition and smart search (OpenVINO), and for phone videos.
- **FileFlows** to convert your own media.

The UHD 770 uses the memory of the system. Two memory modules (dual channel) give it twice the bandwidth of one. So buy 2 × 16 GB, not 1 × 32 GB.

Whether an external GPU adds something is in [egpu.md](egpu.md). In short: not for this plan.

## Memory

A rough budget of what each part uses:

| Part | Memory |
| --- | --- |
| Proxmox | 2 GB |
| Iglo-OS manager | 0.5 GB |
| Jellyfin | 3 GB |
| Media stack (arr apps, qBittorrent, Seerr) | 4 GB |
| Immich, with machine learning | 4 to 6 GB |
| FileFlows | 1 to 2 GB |
| Music (yt-dlp) | 0.5 GB |
| Minecraft | 5 to 6 GB |
| **Total** | **20 to 24 GB** |

16 GB is too little once Minecraft runs. Get 32 GB.

## SSD

The SSD holds what must be fast or must stay local:

- Proxmox and the containers;
- the settings of all apps;
- the database and the thumbnails of Immich;
- downloads that are not finished yet, and torrents that still seed;
- the temporary files of transcodes.

A season of anime or one 4K movie can be 20 to 80 GB, so downloads fill 512 GB quickly. The movies, series, music and photos themselves go to the Umbrel. See [storage.md](storage.md).

With 512 GB, keep the downloads disk of the media stack small, around 200 GB, and set a seeding limit in qBittorrent. A second SSD or an external SSD can come later, as a Proxmox storage for the downloads.

Proxmox backups also go to the Umbrel, not to the SSD. Then a broken SSD does not take the backups with it.

## Checks when you buy

- The exact model and its spec sheet. Business mini PCs with this CPU, like the Lenovo ThinkCentre M90q Gen 3, come in several versions.
- Two memory slots, for dual channel.
- A second M.2 slot is nice for a second SSD later.
- Thunderbolt only matters for an external GPU, and we advise against one. See [egpu.md](egpu.md).
