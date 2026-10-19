# Your own media

You want to add your own files. They are additions to the library, not movies or series: old forgotten videos, behind the scenes footage, something online, or small videos of the family. They come in all kinds of formats, and they must play everywhere. So they are converted once, to a format that the TV box, the PC and the phones all play.

## Two layers

1. **Live, in Jellyfin.** When a device can't play a file, Jellyfin transcodes it on the fly with the GPU (Intel Quick Sync). This already works for every file.
2. **Once, in FileFlows.** You drop a file in the `Inbox` folder on the Umbrel. [FileFlows](https://fileflows.com) sees it, converts it with ffmpeg on the GPU, and moves it to the right library. After that, no live transcode is needed.

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
| `Inbox/Videos` | `Videos/Home`, converted |
| `Inbox/Music` | `Music/Library`, without converting |

`Videos/Home` gets its own Jellyfin library of the type "Home videos and photos". Jellyfin then looks up nothing online and shows the files by folder, with their own names. Subfolders, like `Family` or `Behind the scenes`, become folders in Jellyfin.

A movie or a series of your own can still go straight to `Videos/Movies` or `Videos/Series`, with the names Jellyfin expects, like `Show/Season 01/Show S01E01.mkv`.

## The original

Decided on 19 October 2026: FileFlows converts the file and moves it to the library. The original does not stay.

FileFlows only deletes the original after the new file is complete and plays. If a conversion fails, the original stays in `Inbox` and FileFlows reports it.

Family videos can't be downloaded again. `Videos/Home` goes on the external backup disk, with the photos. See [storage.md](storage.md#a-second-copy-of-the-photos).
