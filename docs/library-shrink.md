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
Confirmation adds jobs. Resume the shrink queue when ready. You can pause the
queue or cancel queued and running jobs. A completed replacement cannot be
cancelled. Jobs show progress and saved space. A source that changed or an
output that did not save enough space stays untouched, with a reason.

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
need no media rescan. The manager asks the connected Jellyfin to refresh once
per round, then acknowledges those jobs. A failed rescan, refresh or
acknowledgement is retried. There is no repeated queue poll while idle.

## Library and conversion

The library folders are `anime`, `series`, `movies` and `anime-movies`. These
names are the defaults in the arr and Jellyfin installers. `deploy/storage.sh`
mounts shares and has no folder list. The shared Go definition is
`shrink.LibraryFolders`. Home Videos is excluded. The installer creates the
library directories and `.iglo-shrink` with the existing share permission
approach before systemd enables their write paths.

The view groups kind, title, season where present, and files. It reports size,
duration, codec, height, bit depth, HDR, audio tracks and estimated output sizes.
A scan runs on startup, on request, and for the containing title after a job.
Probes run one at a time in the background. State caches probes by path, size
and modification time. Unchanged scans do not rewrite the cache. Hidden paths
and symlinks are refused. Encodes read from an already opened source descriptor.
Replacement uses a retained parent directory descriptor, so it cannot follow a
new symlink into another folder. CIFS link counts and chmod are not used to
validate or replace files.

Only `ready` files expand into jobs. A file is skipped if it is not Matroska,
is HDR or Dolby Vision, is under 300 MB, or changed in the last 30 minutes.
Probe errors also skip the file with a reason. HEVC or AV1 files at or below
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
Subtitles, attachments, metadata, chapters and stream dispositions are kept.

Output stages in `/data/videos/.iglo-shrink`, outside title folders on the same
filesystem. Startup removes stale worker temporary files. The result must have
one HEVC 10-bit video, the planned audio codecs, languages and channel counts,
and unchanged subtitle and attachment counts. Duration must be within the
larger of one second and 0.2 percent. Output must be strictly below 90 percent
of the original size. The worker checks source size and mtime again before
renaming over its exact original name. `not-smaller` and `changed` leave the
source untouched. Progress is transient; jobs, probe cache and settings persist
only when they change. History retains at most 100 jobs. A restart requeues
interrupted jobs after removing their temporary output.

## Pinned rescan and score check

Sonarr **v4.0.20.3014** and Radarr **v6.4.4.10685**, checked **2026-11-08** from
the supplied local source under `scratchpad/up/rescan`, plus the sibling
`sonarr` and `radarr` source folders. No upstream network requests were made.

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
conclusion follows from those input builders; source for each individual
specification was not supplied. The Language implementations were supplied
and were checked directly.

The coordinator checked the live host on 2026-11-08: Sonarr had 76 custom
formats and Radarr 73, with only the six kinds above and no scored size format.
A future size format can still change score when the stored size updates.
`iglo-arr shrink-check PATH...` reads each managed title's quality profile and
custom formats. It refuses the title if any format with a non-zero score,
including a negative score, contains `SizeSpecification`. Zero scores are safe.
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
| `POST /shrink` | `Input` body; expanded `Job[]`, 202 |
| `PUT /shrink/settings` | `Settings` body, 204 |
| `POST /shrink/cancel/{id}` | Cancel pending or running job, 204 |
| `POST /shrink/rescanned/{id}` | Acknowledge done job, 204 |
| `GET /events` | Existing stream; new `library` and `shrink` event names, empty data |

Invalid requests return 400. Files which are not ready are left out of an
otherwise valid submission. Repeated active paths are deduplicated. All paths
are validated before any jobs are added. A queue capacity failure adds nothing.

Agent relays use the same methods and suffixes under **`/v1/inbox/{vmid}`**.
The existing event stream stays `/v1/inbox/{vmid}/events`: Inbox jobs map to
`inbox`, while `library` and `shrink` stay distinct. Only names pass through.
The manager keeps a one-minute fallback while jobs are queued, running or
unacknowledged. It stops polling the queue when idle. Container discovery
continues every 30 seconds through the shared Proxmox client.
The active fallback costs one worker-status call per minute in the manager,
plus one per open Video tools page. A page polls the library once a minute
while a scan is pending. These fallback calls stop when idle. Events still
refresh their normal queries when something changes.

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

Tests use sparse files and fake probes/encoders, without real library media.
The dev mock exposes the same routes and simulates a four-second conversion.
Its `shrink-size-format` state file simulates an unsafe scored format.
Real QSV output quality, CIFS rename behaviour and guest installation still need
the coordinator's host acceptance check. Docker ShellCheck and HTTP-listener
tests must run outside the restricted agent sandbox.

The T23 checks cover authenticated manager routes, validation, refusal and
failed-check behaviour, grouped rescans, acknowledgements, refresh failures,
retries, event topics and idle fallback timing. The mock flow runs the real
manager and worker through in-process handlers without opening a listener.
The web checks render the section and exercise selection and confirmation.
They live under `web/src` to keep this task within its write scope.

Run the full checks outside a sandbox that blocks TCP and Unix listeners:

```sh
(cd web && npm run build)
make test
go test -race ./internal/... ./cmd/...
(cd web && node --experimental-strip-types --test src/features/apps/shrink.test.mjs)
gofmt -l cmd internal dev web/embed.go
(cd web && ./node_modules/.bin/prettier --check '**/*.{ts,tsx}')
make shellcheck
```

The existing listener tests cannot run in the restricted sandbox. No dependency
installation, network access, host changes or installer changes are needed for
the in-process checks. The real browser flow on the dev mock still needs a
listener-capable environment. Hardware acceptance still needs the coordinator's
host.

The refresh method was checked on 2026-11-08 against the supplied local
Jellyfin `LibraryController.cs:337-351`: `POST /Library/Refresh` requires an
admin API key and returns 204. The local snapshot has no version marker. The
installed Jellyfin version follows its package repository. Match this snapshot
to that package version during host acceptance.
