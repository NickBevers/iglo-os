# Your own media

You want to add your own files. They come in all kinds of formats, and they must play everywhere. So they are converted once, to a format that the TV box, the PC and the phones all play.

## Two layers

1. **Live, in Jellyfin.** When a device can't play a file, Jellyfin transcodes it on the fly with the GPU (Intel Quick Sync). This already works for every file.
2. **Once, in FileFlows.** You drop a file in the `inbox` folder on the Umbrel. [FileFlows](https://fileflows.com) sees it, converts it with ffmpeg on the GPU, and moves it to the right library. After that, no live transcode is needed.

ffmpeg does the real work. HandBrake is mostly a window around the same idea. FileFlows adds a watched folder and the moves to the libraries.

## Only for your own files

FileFlows does not touch what Sonarr and Radarr download. Converting those again loses quality, breaks seeding, and can damage anime subtitles. Jellyfin transcodes those live, when needed.

## The target format

For the Raspberry Pi 5 on the LG C5 (2025), and for the PC and the phones:

| | Target |
| --- | --- |
| Container | MKV |
| Video, 4K or HDR | HEVC 10-bit. HDR stays HDR. |
| Video, 1080p and smaller | HEVC or H.264. |
| Audio | AAC, AC3 and EAC3 stay as they are. Everything else becomes AAC. A DTS or TrueHD track stays, and gets an EAC3 copy next to it. |
| Subtitles | Text subtitles (SRT, ASS) stay. |

Not AV1. The UHD 770 can't encode it, and a Raspberry Pi 5 can't decode it.

If the TV box becomes the Ugoos AM6B+, this target still works.

## Music

Music files are not converted. Jellyfin converts audio live, and that costs almost nothing. A FLAC file stays FLAC.

## Inbox folders

| Folder | Goes to |
| --- | --- |
| `inbox/movies` | `media/movies` |
| `inbox/series` | `media/series` |
| `inbox/anime` | `media/anime` |
| `inbox/music` | `music/library`, without converting |

## Open questions

- What happens to the original after converting? Delete it, or keep it for some days in `inbox/done`?
- How should a file be named so it lands in the right series and season? Jellyfin needs names like `Show/Season 01/Show S01E01.mkv`.
