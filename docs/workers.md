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

## Inbox

Inbox uses a native Go service and Debian FFmpeg, without Docker. One
worker runs at a time, with two CPU cores and a 1536 MiB systemd ceiling in the
existing 2048 MiB guest budget. These are limits, not measured memory use.
The loopback API on port 8789 reaches clients only through the manager/agent.

Open **Services → Inbox work**, review the profile and resume the initially
paused queue. Sources are read only at `/data/inbox`. Output goes to
`/data/videos/home-videos`. SSD staging and state live in
`/var/lib/iglo-inbox/worker`. Scans accept files unchanged for 30 seconds.
Remote SMB writes do not reliably generate local inotify events, so the worker
lists the share itself. Each listing can wake the NAS disks, so timed scans
follow admission:

- With **Control Music and Inbox admission** on, a scan runs when the lease
  first lets work start, then at most every 30 minutes while it holds. A held
  lease means no timed scan, so nothing outside the processing window. Gaming,
  low memory, a backup or an update hold scans like they hold jobs.
- With it off (lease `-1`), a scan runs every 6 hours.
- A paused queue gets no timed scans. **Scan Inbox now** always works and
  restarts the timer.

There is no separate startup scan. A restarted worker scans the first moment it
may work, so in a window that is the next lease.

- **Original codecs**, the default, copies primary video, audio and subtitles,
  metadata and chapters without re-encoding. MOV/MP4 keeps that container family;
  other sources publish Matroska. Unsupported combinations fail without publishing.
- **Playback** publishes H.264 8-bit MP4, copying compatible streams. Other video
  uses a verified VAAPI encoder or CPU fallback. Incompatible audio becomes AAC.
  Resolution stays. HDR needing conversion and subtitled inputs require the
  original-codec profile. Automatic tone mapping is not enabled.

Limits are one primary video stream, four hours and 16 GiB per source/output.
Free SSD space must cover the source, 16 GiB output allowance and 4 GiB headroom.
FFprobe validates duration, dimensions, counts and codecs. FFmpeg reads all
output video/audio packets. This checks containers/packets, not visual quality.
Review small samples on your clients before processing an archive.

Publication uses a hidden file on the NAS, flushes it and renames to a unique
job filename. Originals are never replaced/deleted. Failed work and unavailable
mounts never publish completed output. The last 100 jobs are retained, with a
separate bounded fingerprint set to avoid processing old completed versions
again after history eviction. Size/mtime changes identify a new version.
Interrupted jobs fail explicitly; inspect output before retrying.

The Go worker uses the repository license. Debian FFmpeg is FOSS, under LGPL/GPL
depending on build options. Do not use `--enable-nonfree` builds. Checked on
28 October 2026 against FFmpeg 7.1.5 source and Debian 13; installed package
revisions may differ.

Removing the guest deletes worker state and history. NAS originals and output
stay.

## Shared processing admission

In **Background work**, enable **Control Music and Inbox admission**. The page
fills in the node of the host. This setting is independent of an Immich key.
Set 4 GiB headroom and the desired window/timezone. A two-hour gaming hold defers new jobs.
Existing jobs finish. A manual worker pause also applies, and an external lease
never overrides it.

The manager checks every 15 seconds and sends a new 60-second admission lease
about every 30 seconds, before the last one runs low. When
enabled, unknown memory, a gaming hold or an excluded window writes `0`.
A fixed answer (`0` or `-1`) is only written when it changes, when a worker
starts again, or every 10 minutes. Each write is a `pct exec` on the host.
If the manager disappears, the lease expires and new work is deferred. Disabling
external control writes `-1` and returns authority to native/manual controls.
A Moonlight session holds new work, but there is no hard GPU reservation.

## Checks completed and still required

Go tests cover URL restrictions, durable publication, failed download/NAS-loss
behavior, symlink rejection, manual pause and expired leases. Controller tests
cover worker leases without Immich, gaming, low RAM and the daily window.
The development fixture uses explicit simulated jobs and downloads no media.
Real YouTube extraction, Intel VAAPI encoding, Proxmox guest installation,
NAS outages and power-cycle recovery still need hardware acceptance testing.

Primary references:
[yt-dlp](https://github.com/yt-dlp/yt-dlp).


Native Inbox tests run real local FFmpeg copying, CPU conversion, GPU failure
fallback, subtitles and malformed input. The mock simulates
publication. It does not run host installers or measure hardware performance.

FFmpeg references: [7.1.5 source](https://github.com/FFmpeg/FFmpeg/tree/n7.1.5),
[license](https://github.com/FFmpeg/FFmpeg/blob/n7.1.5/LICENSE.md),
[Debian 13 package](https://packages.debian.org/trixie/ffmpeg).
