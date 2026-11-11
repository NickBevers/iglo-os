# Library shrink

Video tools can shrink selected library files in place. Its app ID stays
`inbox`, and the page stays at `/inbox`. Inbox imports and their API keep
working. Inbox uses Debian FFmpeg;
shrink uses Jellyfin FFmpeg 7, including its Intel driver and oneVPL runtime.
Jellyfin FFmpeg is GPL software. The installer uses the same repository key
and deb822 sources file as the Jellyfin installer. No extra container is needed.

Shrink starts paused. It only accepts requests with
`"confirm": "replace-originals"`. This replaces originals, so keep a NAS backup.
The worker serializes encodes with Inbox conversion. Both use the guest's
existing two-core CPU quota and 1536 MiB systemd limit. Shrink runs with nice 15
and idle I/O priority. A manual pause or held admission lease interrupts shrink,
removes the temporary file, and queues that file again. Cancellation removes
the temporary file and ends that job. Existing Inbox pause behaviour stays as it
was. Admission comes from the agent's host memory and PSI policy, through the
same `admit-until` file. An expired lease holds work.

## Pick files and start

Open **Services → Video tools → Shrink library**. Inbox imports stay in the
**Inbox** section with their existing controls. Each queue starts paused and
has its own pause button.

Scan the library, then expand Anime, Series, Films or Anime films. Expand a
title and season to see files. A checkbox on a folder selects its ready files.
A partly selected folder shows a mixed checkbox. Already small files, skipped
files and files already shrunk cannot be selected. Skipped files show why,
including HDR and files outside Matroska. Home Videos is excluded.

Choose **Standard** or **High**. A 24-minute episode is about 0.27 GB with
Standard or 0.37 GB with High. The rows and selection summary show the current
size and an estimate for that profile. Actual output varies. The GPU encodes
HEVC where available, with a CPU fallback. Anime keeps Japanese audio when
present. If there is no Japanese track, it keeps all audio. Subtitles and fonts
stay.

Choose **Shrink…**, review the numbers and tick **I understand the originals
are replaced**. Only then can you confirm. **The originals are replaced and
can't be restored.** Keep a separate backup if you need the source later.
A successful confirmation adds the selected files and resumes the shrink
queue. You can pause it or cancel a queued or encoding job. Once replacement
has been recorded and the swap starts, cancellation no longer applies. Jobs show progress and saved space. A source that changed or an
output that did not save enough space stays untouched, with a reason.

Shrink jobs run only while background work is admitted, usually during the
processing window. A queued job can wait because of the work window, gaming,
low host memory, a backup or an update. The queue header shows the hold reason
and the configured window. To change it, open **Background work** in Iglo-OS,
keep **Control Music and Video tools admission** enabled, and edit the daily
window's start, end and timezone. Resume releases a manual pause; it does not
override background work admission.

## Media apps after shrinking

Before adding jobs, the manager asks Sonarr and Radarr whether the selected
titles can safely shrink. A title is refused when its profile scores a size
custom format or cannot be inspected. Refused titles appear with a reason and
never reach the worker. If the check fails, nothing is submitted. With no media
stack installed, the files count as unmanaged and can still shrink. A stopped
or unreachable installed media stack must be started or fixed first.

After successful replacement, the manager groups completed files by title and
asks Sonarr or Radarr to rescan it. This updates size and media info while
keeping the existing file record, quality and release identity. Stored audio
language labels are kept too. The command is accepted asynchronously, so the
media app may still be scanning after the job is acknowledged. Unmanaged titles
need no media rescan. Once no queued or running jobs remain, the manager asks
the connected Jellyfin to refresh once for the completed batch, then
acknowledges those jobs. A failed rescan, refresh or acknowledgement is retried.
Progress updates do not start rescan rounds. An unreachable worker uses retry
intervals of 1, 2, 4, 8, 16 and then 30 minutes. Availability changes are logged
once. There is no repeated queue poll while idle.

## Library and conversion

The library folders are `anime`, `series`, `movies` and `anime-movies`. These
names are the defaults in the arr and Jellyfin installers. `deploy/storage.sh`
mounts shares and has no folder list. The shared Go definition is
`shrink.LibraryFolders`. Video tools does not receive the media stack's custom
folder settings. Differently named library folders are outside this view.
Home Videos is excluded.

The installer creates the default library directories and a hidden
`.iglo-shrink` directory inside each kind, using the existing share permissions.
The unit has one optional write path for each kind, without a separate write
path for staging. This keeps both rename endpoints on the same systemd bind
mount. Missing kind folders are skipped and do not prevent Inbox from starting.
Restart Video tools after adding a previously missing folder.

The view groups kind, title, season where present, and files. It reports size,
duration, codec, height, bit depth, HDR, audio tracks and estimated output sizes.
A scan runs on startup, on request, and for the containing title after a job.
Probes run one at a time in the background. State caches probes by path, size
and modification time. Unchanged scans do not rewrite the cache. Hidden paths
and symlinks are refused. Encodes read from an already opened source descriptor.
Replacement uses a retained parent directory descriptor, so it cannot follow a
new symlink into another folder. CIFS link counts and chmod are not used to
validate or replace files.

Only explicitly selected `ready` files become jobs. Folder checkboxes expand
to the visible files in the browser; the worker never expands folder paths.
A file is skipped if it is not `.mkv`, is HDR or Dolby Vision, is under 300 MB,
or changed in the last 30 minutes. Extra primary video streams, data streams,
unknown stream types and missing video-stream duration are also refused rather
than being dropped silently. Probe errors skip the file with a reason. HEVC or AV1 files at or below
1.25 times the standard estimate are `small`. A cached result replaced by this
worker is `done`, even if its modification time is recent.

Estimates use 1.55 Mbps for standard or 2.1 Mbps for high at 1920 by 1080,
scaled by actual pixel count, plus kept audio. Unknown lossy audio bitrates use
the corresponding Opus rate as an estimate. Estimates are not output limits.

With a verified QSV encoder, video uses HEVC Main10, veryslow, global quality
21 for standard or 17 for high. Without `/dev/dri/renderD128`, or if the startup
QSV probe fails, video uses libx265 slow, 10-bit, CRF 19 or 17,
`aq-mode=3:no-sao=1`, with two encoder pool threads. These settings were measured
on the owner's i7-12700T and UHD 770, checked 2026-11-08. Debian's FFmpeg and QSV
runtime cannot replace Jellyfin FFmpeg for this command on that host.

Anime keeps Japanese audio if any Japanese track exists, otherwise all audio.
Other kinds keep all tracks. FLAC, TrueHD, PCM, ALAC and DTS-HD MA become Opus:
160 kbps for up to two channels, 384 kbps through six channels, 512 kbps above
six channels. Multichannel Opus uses mapping family 1. Lossy audio is copied.
Subtitles, attachments, chapters, ordinary metadata and stream dispositions
are kept. Attached cover images are mapped and copied as video streams.
Re-encoded video and audio lose old mkvmerge statistics tags, including `BPS`,
`NUMBER_OF_BYTES`, `NUMBER_OF_FRAMES`, `DURATION` and `_STATISTICS_*`, with their
language suffixes. Copied streams retain their unchanged metadata.

Output stages in `/data/videos/<kind>/.iglo-shrink`, outside title folders.
Worker scans skip the hidden directory. Sonarr v4.0.20.3014 and Radarr
v6.4.4.10685 also exclude dot folders (`DiskScanService.cs:73`, checked
2026-11-08). Jellyfin ignores the hidden directory. Staging never uses Home
Videos.

Verification compares primary video duration, not just container duration. It
uses the stream's duration or its Matroska `DURATION` tag. The allowed change
is one second plus 0.1 percent, capped at three seconds. The result must have
one HEVC 10-bit primary video, unchanged cover images, planned audio codecs,
languages and channel counts, and the same chapter, subtitle and attachment
counts. Output must be strictly below 90 percent of the original size.

Before replacement, the worker flushes and closes both media files, then checks
the source's size, inode and modification time again. It records a durable swap
intent in worker state. From that point, cancellation cannot apply. The swap moves the
original to `.iglo-shrink/backup-<id>`, moves the verified output to the exact
original filename, then checks the installed file against the verified size and
inode. CIFS can defer the output's modification time until after close and rename.
When the inode is unknown, the result uses a 30-second timestamp window instead.
Originals and backups use a two-second timestamp window with size and inode.
It saves `done` before removing the backup. A failed rename restores the backup
when possible. It never removes a temporary result after a
failed rename unless the original is confirmed present and unchanged.
Install and restore use atomic no-replace renames. If Sonarr or Radarr imports
an upgrade at the library name during the swap, the worker keeps the upgrade,
backup and temporary output, then pauses with a recovery error.

Startup replays recorded swaps. It restores a missing original from its backup
before resuming a verified replacement, or completes `done` when the result was
already installed. Interrupted encodes are requeued after confirming that the
original is intact and removing only their own temporary file. Unrecognized
files and old share-level `.iglo-shrink` files are retained. Recovery actions
are logged. An ambiguous or failed recovery pauses the queue, retains the
remaining copies and reports the recovery error on the job. Pending intents
retry when the share returns, checked locally every 30 seconds, or every ten
minutes while the share stays available. Retry checks do not scan the library
or call the host agent. Only a completed recovery refreshes its title once.
Recovery leaves the queue paused. A path with a pending intent cannot be
submitted again. Fix storage before retrying; do not remove
those copies blindly. Restarting Video tools also retries recovery.

`not-smaller` and `changed` leave the source untouched. Progress comes from
FFmpeg's output time. FFmpeg 7.1 reports that time as unknown while any output
stream has written nothing, and font attachments never write a packet. Progress
then counts the video frames written against the duration times the average
frame rate (Jellyfin FFmpeg v7.1.4-3, checked 2026-11-11). Progress stays in
memory and emits at most once every 30 seconds while encoding. State changes emit at
once. Jobs, probe cache and settings persist only when they change. History has
at most 100 jobs. Unacknowledged done jobs and pending recoveries cannot be
evicted, so a full history can hold new submissions until media rescans finish.

## Pinned rescan and score check

Sonarr **v4.0.20.3014** and Radarr **v6.4.4.10685**, checked **2026-11-08** against
the pinned upstream source. References below name the checked source files.

| Behaviour | Sonarr source | Radarr source |
| --- | --- | --- |
| Command selects one title by ID | `Sonarr-RescanSeriesCommand.cs:5-17` | `Radarr-RescanMovieCommand.cs:5-17` |
| Cleanup retains a present, linked file | `Sonarr-MediaFileTableCleanupService.cs:36-56` | `Radarr-MediaFileTableCleanupService.cs:34-47` |
| Existing files are filtered from imports; changed size is saved | `Sonarr-DiskScanService.cs:134-171` | `Radarr-DiskScanService.cs:134-170` |
| Media info refresh assigns only MediaInfo on the same record | `Sonarr-UpdateMediaInfoService.cs:59-88` | `Radarr-UpdateMediaInfoService.cs:59-88` |
| Existing-file custom format input uses stored identity, quality, languages and current size | `Sonarr-CustomFormatCalculationService.cs:170-210` | `Radarr-CustomFormatCalculationService.cs:175-216` |
| Language formats use input.Languages | `sonarr/LanguageSpecification.cs:49-59` | `radarr/CFLanguageSpecification.cs:49-59` |
| Upgrade decisions recalculate existing-file formats | `Sonarr-UpgradeDiskSpecification.cs:41-60` | `Radarr-UpgradeDiskSpecification.cs:40-61` |
| Scores determine upgrade acceptance and cutoffs | `Sonarr-UpgradableSpecification.cs:83-113,142-159` | `Radarr-UpgradableSpecification.cs:83-113,142-159` |

Rescan keeps the file record, quality, release group, scene name and stored
`Languages`. It does not re-derive languages when the English dub is removed.
Media info updates only when `EnableMediaInfo` is on; size still updates when
it is off. The existing-file format builders do not pass `MediaInfo` or media
stream data to any specification. Thus Language, ReleaseGroup, ReleaseTitle,
Resolution, Source and QualityModifier formats cannot use refreshed media info
on this path. They receive the stored identity, quality and languages. This
conclusion follows from the checked existing-file input builders and the
Language implementations.

Checked on the installed system on 2026-11-08: Sonarr had 76 custom
formats and Radarr 73, with only the six kinds above and no scored size format.
A future size format can still change score when the stored size updates.
`iglo-arr shrink-check PATH...` reads each managed title's quality profile and
custom formats. It refuses the title if any format with a non-zero score,
including a negative score, contains `SizeSpecification`. Zero scores are safe.
CLI parent-folder selections are checked against every managed title beneath
them, so they cannot bypass the guard by appearing unmanaged.
Missing profiles or uninspectable scored formats are refused. API errors fail
the command instead of claiming a title is unmanaged. A successful library
lookup with no matching title returns `ok-unmanaged`, reason
`not managed by Sonarr or Radarr`. These files may still be shrunk.

The manager runs shrink-check before submission and leaves out refused titles
with their reasons. A failed or incomplete check submits nothing. This is a
check at submission time. Changes to profiles after
the check require checking again before submitting more work.

`iglo-arr rescan PATH...` maps share-relative paths to `/data/media`, matches
whole title path components, and submits one `RescanSeries` or `RescanMovie`
command per managed title. Unmanaged paths need no rescan. JSON output lists
commands accepted by the apps, not completed scans. After the manager has told
the media apps, it acknowledges the worker's done jobs through `rescanned`.
The manager retries failed notifications. Jobs stay unacknowledged until the
rescan request and the Jellyfin refresh succeed.

## Manager and worker contract

Types are in **`iglo/internal/shrink`**:

- `Input { paths: string[], profile: "standard" | "high", confirm: "replace-originals" }`.
- `Settings { paused: *bool }`. Omission is invalid.
- `Library { kinds: Folder[], scanning, scannedAt, error? }`.
- `Folder { path, name, folders: Folder[], files: File[] }`.
- `File { path, bytes, modified, duration, codec, height, bitDepth, hdr, audio: Audio[], estimates: Estimates, status, reason? }`.
- `Audio { index, codec, language, channels }`; `Estimates { standard, high }` in bytes.
- `View { paused, jobs: Job[] }`.
- `Job { id, path, profile, state, progress, sourceBytes, resultBytes, savedBytes, sourceModified, error?, finishedAt?, rescanned }`.

Job states: `queued`, `running`, `done`, `failed`, `not-smaller`, `changed`,
`cancelled`. Progress is 0 through 1. Paths include the library kind and are
relative to the Videos share. IDs are 32 hexadecimal characters. Durations
are seconds. Times use Go's JSON time format. File statuses: `ready`, `small`,
`skipped`, `done`.

Worker routes on **127.0.0.1:8789**:

| Method and route | Result |
| --- | --- |
| `GET /library` | `Library`, 200 |
| `POST /library/scan` | Queue background scan, 202 |
| `GET /shrink` | `View`, 200 |
| `POST /shrink` | `Input` body with explicit file paths; accepted `Job[]`, 202 |
| `PUT /shrink/settings` | `Settings` body, 204 |
| `POST /shrink/cancel/{id}` | Cancel pending or running job, 204 |
| `POST /shrink/rescanned/{id}` | Acknowledge done job, 204 |
| `GET /events` | Existing stream; new `library` and `shrink` event names, empty data |

Invalid requests return 400. Files which are not ready are left out of an
otherwise valid submission. Repeated active paths are deduplicated. All paths
are validated before any jobs are added. A request accepts at most 100 file
paths. Folder paths return 400. A queue capacity failure adds nothing and
returns a clear message to wait for jobs and rescans, or select fewer files.
The worker API retains its separate pause setting; the confirmation dialog
resumes it through the existing settings route.

Agent relays use the same methods and suffixes under **`/v1/inbox/{vmid}`**.
The existing event stream stays `/v1/inbox/{vmid}/events`: Inbox jobs map to
`inbox`, while `library` and `shrink` stay distinct. Only names pass through.
The manager and all tabs share worker status for 30 seconds. During an encode,
`shrink` emits at most twice a minute. Status reads are shared across tabs, so
steady progress costs at most two host `pct exec` calls a minute instead of
multiplying that cost by the number of tabs. State changes and user actions
refresh immediately. A one-minute fallback remains while jobs are active or
unacknowledged. A page polls the library once a minute while a scan is pending.
Idle queue polling stops. Container discovery continues every 30 seconds
through the shared Proxmox client.

Media agent routes:

| Method and route | Body | Result |
| --- | --- | --- |
| `POST /v1/apps/media/shrink-check` | `agent.MediaLibraryRequest { vmid, paths }` | `arr.ShrinkCheckResult[]`, 200 |
| `POST /v1/apps/media/rescan` | Same | `arr.RescanResult[]`, 200 |

`ShrinkCheckResult { path, paths, title, app?, id?, profileId?, state, reason? }`
groups inputs by managed title. State is `ok`, `refused` or `ok-unmanaged`.
`RescanResult { app, id, path }` identifies each accepted command. The same
commands are available as `iglo-arr shrink-check [--env FILE] PATH...` and
`iglo-arr rescan [--env FILE] PATH...`. API keys stay inside the media guest.

Agent client helpers: `Library`, `ScanLibrary`, `ShrinkJobs`, `SubmitShrink`,
`ShrinkSettings`, `CancelShrink`, `ShrinkRescanned`, `ShrinkCheck` and
`RescanLibrary`. `InboxEvents` follows the shared event stream.

## Validation and hardware limits

Tests use sparse files and fake probes and encoders. Fault tests interrupt every
swap and recovery step, inject rename and state-write failures, and check that
remaining copies survive. A generated one-second clip tests FFmpeg statistics
cleanup and metadata retention without using library media. Other tests cover
stream and chapter verification, file selection, limits, history protection,
progress event rate, profile guards and rescan command bodies.

The development fixture simulates conversion and media rescans. Real QSV
output quality and the complete install and recovery flow still need testing
on the target host and CIFS share. Check samples on the playback devices before
processing a library.
