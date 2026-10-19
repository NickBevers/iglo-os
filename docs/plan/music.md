# Music

Music is a separate library, apart from movies, series and anime. It should feel like YouTube Music: while a song plays, its cover art fills the screen.

## The rules

- A song is always audio with cover art.
- A song that has a video on YouTube is still a song. It goes in the music library, without the video.
- A music video is only downloaded when you ask for it. It goes in a separate Music Videos library.

## Where the music comes from

### yt-dlp first

[yt-dlp](https://github.com/yt-dlp/yt-dlp) downloads from YouTube Music. You give it a song, an album or a playlist:

1. It downloads the audio only. AAC (m4a) is the first choice, because every client plays it without a transcode.
2. It puts the cover art and the tags (artist, album, track number) in the file.
3. It crops the cover art to a square. Songs from YouTube Music have square art, but plain YouTube uploads have a 16:9 video frame.
4. It saves the file as `Music/Library/<Artist>/<Album>/<number> <title>.m4a`.

For a music video, you ask for it apart. Then it saves the video in `Music/Videos`.

YouTube changes often, and an old yt-dlp stops working. So the music container must update yt-dlp often. The weekly container updates on the Updates page can do that.

In Iglo-OS this becomes:

- a music stack: an installer and a container with yt-dlp and ffmpeg;
- a Music page, to paste a link to a song, album or playlist, and to see the downloads;
- later, maybe: follow a YouTube Music playlist and download what is new.

### Lidarr later

[Lidarr](https://lidarr.audio) is the arr app for music. It works like Sonarr: you follow an artist, and it finds the albums through Prowlarr and qBittorrent. Public music torrents are thin, and the metadata service of Lidarr had a long outage in 2025. Check its state before we build on it.

## Playback

Jellyfin serves the music too, in its own **Music** library. One server then plays music on the TV, the PC and the phone.

- **Phones:** [Finamp](https://github.com/jmshrv/finamp), a music app for Jellyfin. It is the closest to YouTube Music, with the cover art full screen.
- **PC:** the Jellyfin web client.
- **TV:** Kodi on a Raspberry Pi 5, with the Jellyfin add-on. See [playback.md](playback.md#music-on-the-tv).

[Navidrome](https://www.navidrome.org) is a good music server too, but it has no app for the TV. We can add it later next to Jellyfin, on the same folder.

## Open questions

- There are no playlists to import yet. This stays open until the mini PC is there.
