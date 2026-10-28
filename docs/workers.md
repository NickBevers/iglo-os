# Music and Inbox workers

Install Music and Inbox from Apps after connecting their [scoped shares](scoped-storage.md).
Both are tagged unprivileged LXC guests. The installer retains at least 4 GiB of
host MemAvailable outside a new guest; keep the complete machine's workload
budget within its physical RAM.

## Music

Music uses a native Go service, **one worker**, two CPU cores, a 768 MiB systemd
memory ceiling and a 1024 MiB guest by default. No Docker daemon is needed.
The release bundle includes `iglo-music`; the installer downloads checksum-pinned
yt-dlp **2026.10.21** and Deno **v2.9.8** from their official releases. FFmpeg comes
from Debian. Upstream site changes can break downloads; tools update with a
reviewed Iglo-OS release, not self-updates on every start.

Use **Services → Music work** to submit a single YouTube/YouTube Music video,
choose audio or optional video, inspect the queue and pause/resume new work.
Only canonical HTTPS video URLs are accepted; playlists are not expanded.
The worker limits jobs to 20 minutes of media, a 30-minute execution timeout,
one download fragment at a time and a 512 MiB final file. Download-size estimates
can be inaccurate: staging can be larger than the final file. Allow several GB
of local free space. Concurrency and the local disk bound the working set.

Downloads stage under `/var/lib/iglo-music/staging` on SSD. After successful
extraction, the best source audio codec is retained where supported (for example
M4A or Opus); a fixed lossy audio conversion is not imposed. After successful
processing the worker copies to a hidden temporary file on the Music share,
flushes it and publishes by rename under `audio/` or `videos/`. Filenames use
unique job IDs; title/artist/cover metadata remains embedded in the media. Failed
downloads and missing mounts never publish a completed file. The queue retains
the most recent 100 jobs in `queue.json`; it is not a library inventory. Jobs
interrupted by a restart fail explicitly and require resubmission.

Add a **read-only Music share mount** to your Jellyfin guest, then a Music library
for `audio/` and, if wanted, a separate Music videos library for `videos/`.
The existing Jellyfin installer only binds its legacy movie/series share; this
extra library mount is still a manual Proxmox/Jellyfin step. Set a host mount
dependency as for the scoped guests and test NAS loss/reboot before relying on
it. The worker does not automatically modify existing Jellyfin libraries.
Finamp connects to Jellyfin, rather than to the worker.

The worker API binds **127.0.0.1:8788 inside the guest**. The authenticated manager
uses the host agent and `pct exec`; it does not expose the worker port to clients.
Music uses its guest's normal network connection. The media stack's Gluetun VPN
does not automatically cover this separate guest.
Guest backups include the queue, not the NAS music. Only submit media you are
entitled to download; no account cookies, subscriptions or login bypass are set up.

## Inbox / FileFlows

Inbox installs the stable FileFlows image, pinned to the registry digest checked
22 October 2026. It has two CPU cores and a 1536 MiB Docker memory ceiling inside
a 2048 MiB guest. The UI listens on guest port **19200**. Its built-in Agent is
the processor: no second machine or extra worker container is required.

| Container path | Access and purpose |
| --- | --- |
| `/media/inbox` | Read-only Inbox share: sources cannot be replaced/deleted by a flow |
| `/media/output` | Only the Videos share's `home-videos` output folder is exposed to FileFlows |
| `/temp` | SSD staging, disposable |
| `/app/Data` | SSD flow/configuration database, included in guest backups |
| `/app/common` | Persistent tools/DockerMods on SSD |
| `/gate` | Read-only manager admission lease |

Finish the native setup before enabling a library:

1. Open FileFlows, set authentication and restrict access to your LAN/Tailscale.
2. In DockerMods, install the current **FFmpeg FileFlows Edition** only; set its
   FFmpeg/ffprobe paths as indicated by that mod. The minimal stable image does
   not promise a bundled FFmpeg. Do not install every mod.
3. Use the internal Agent with **one runner**, temporary directory `/temp`, and
   an overnight native schedule. If optional runner-cost fields are licensed,
   leave them at defaults; additional agents and a paid API are not assumed.
4. Paste the bundled `/opt/inbox/pre-execute.cs` into that Agent's **Pre-Execute
   Script**. It reads `/gate/admit-until`. A missing/expired lease returns false.
5. Create a library at `/media/inbox` and a copy-only flow: inspect the video,
   convert/remux in `/temp`, validate the output, then copy to a unique filename
   under `/media/output`. Do not use **Replace Original**, delete
   source or move-source actions; the read-only source mount intentionally refuses
   them. Use a small test clip first and confirm playback, duration, streams and
   subtitles before processing an archive.
6. Enable the library only after the flow and admission script have been tested.

This installs the real service and supplies its admission script. It does **not**
claim an imported flow, a licensed API connection or verified conversions.
Configure and export the tested flow from the native UI. When the host has a
GPU, the installer passes its render node to the container and loads
`compose.gpu.yaml`, so FileFlows can use VAAPI or Quick Sync in its flows. Pick
the hardware encoder in the flow; FileFlows doesn't switch by itself.

Updates stop FileFlows, archive `/app/Data` on local SSD, then replace the image.
Scoped Music/Photos/Inbox updates do not automatically rewind guest disks on
failure: NAS publications cannot be rolled back with those disks. Inspect the
operation and exported app state before choosing a recovery action.
Local pre-update archives are included in guest disk backups and need retention;
they are not a substitute for a separate NAS/off-device backup. Removing the
guest deletes its flows/database, while NAS sources and outputs remain.

## Shared processing admission

In **Background work**, select the Proxmox node and enable **Control Music and
Inbox admission**. This setting is independent of an Immich key. Set 4 GiB
headroom and the desired window/timezone. A two-hour gaming hold defers new jobs.
Existing jobs finish. Native FileFlows scheduling and manual Music pause also
apply; an external lease never overrides either one.

The manager checks every 15 seconds and sends a new 60-second admission lease
about every 30 seconds, before the last one runs low. When
enabled, unknown memory, a gaming hold or an excluded window writes `0`.
A fixed answer (`0` or `-1`) is only written when it changes, when a worker
starts again, or every 10 minutes. Each write is a `pct exec` on the host.
If the manager disappears, the lease expires and new work is deferred. Disabling
external control writes `-1` and returns authority to native/manual controls.
FileFlows only obeys the lease after its pre-execute script is configured.
A Moonlight session holds new work, but there is no hard GPU reservation.

## Checks completed and still required

Go tests cover URL restrictions, durable publication, failed download/NAS-loss
behavior, symlink rejection, manual pause and expired leases. Controller tests
cover worker leases without Immich, gaming, low RAM and the daily window.
The development fixture uses explicit simulated jobs and downloads no media.
Real YouTube extraction, FileFlows flow execution, Proxmox guest installation,
NAS outages and power-cycle recovery still need hardware acceptance testing.

Primary references:
[yt-dlp](https://github.com/yt-dlp/yt-dlp),
[FileFlows Docker](https://fileflows.com/docs/installation/docker),
[Agent controls](https://fileflows.com/docs/webconsole/agents/),
[Pre-Execute Script](https://fileflows.com/docs/webconsole/agents/pre-execute-script).

## FOSS replacement assessment

FFmpeg is the preferred candidate for replacing FileFlows in a later change.
The current Inbox installer still uses FileFlows. Checked on 28 October 2026
against [FFmpeg 7.1.5 licensing](https://github.com/FFmpeg/FFmpeg/blob/n7.1.5/LICENSE.md)
and the [Debian 13 package](https://packages.debian.org/trixie/ffmpeg). FFmpeg
is LGPL or GPL depending on its build options. Use a distribution build
without incompatible additions enabled through `--enable-nonfree`.

A native Inbox worker could reuse Music's durable queue, admission leases,
SSD staging and checked NAS publication. FFprobe would inspect sources,
compatible files would be remuxed without re-encoding, and conversion would
use a detected hardware encoder with a CPU fallback. It would retain the
read-only source mount and validate duration and streams before publishing.
This needs a worker and migration path; replacing a container image alone
does not replace FileFlows scheduling, flow state or admission behavior.
