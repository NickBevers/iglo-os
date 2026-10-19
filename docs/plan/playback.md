# Playback

## The TV

The TV is an LG OLED55C54, a C5 from 2025. It never goes online, not even on the LAN. LG TVs have been shown to send data even when they are turned off.

So the TV gets no app. A small box plugs into it over HDMI. The box goes on the network, and the TV only shows a picture.

## The box: Kodi, without Apple or Google

The aim is to move away from big tech. So no Apple TV, no Google TV Streamer and no Nvidia Shield. Those all need an Apple or Google account.

The open-source way is [Kodi](https://kodi.tv), on a small Linux system made only for Kodi. It needs no account. Kodi plays from Jellyfin with an add-on.

### The options

| | Raspberry Pi 5 with LibreELEC | Ugoos AM6B+ with CoreELEC | Intel N100 mini PC with LibreELEC |
| --- | --- | --- | --- |
| System | [LibreELEC](https://libreelec.tv) | [CoreELEC](https://coreelec.org). The box comes with Android, and CoreELEC replaces it. | LibreELEC |
| HEVC and HDR10 | Yes, 4K60 in hardware | Yes | Yes, but HDR is unreliable in reports |
| Dolby Vision | No | Yes, the best of all Kodi boxes | No |
| AV1 | No. The mini PC converts it. | No. The mini PC converts it. | Yes |
| LG remote (HDMI-CEC) | Yes, built in | Yes, built in | No. It needs a USB CEC adapter. |
| Price | About €100 with case, power supply and SD card | About €150 to €200 | About €150 to €200 |
| Watch out | Micro-HDMI port, so you need an adapter or cable | One user reports the HDMI signal sometimes drops with LG TVs | One report says it needs a 48 Gbps HDMI cable |

These prices are estimates.

### Advice: Raspberry Pi 5 with LibreELEC

- It is the cheapest, and the hardware is the most open.
- The LG remote controls Kodi over HDMI-CEC (LG calls it SimpLink). This works without internet on the TV.
- Kodi draws styled ASS subtitles itself, with libass. That is the most important point for anime: no burn-in, so no transcode.
- Most anime is 1080p HEVC or H.264. The Pi plays that without effort.
- What it can't play, the mini PC converts on its GPU: AV1, and Dolby Vision files without an HDR10 fallback.

Take the Ugoos AM6B+ with CoreELEC instead when Dolby Vision movies matter. The C5 shows Dolby Vision well, and the CoreELEC forum still calls this box the best for it.

### Jellyfin in Kodi

There are two add-ons. Both play music too.

- **[Jellyfin for Kodi](https://github.com/jellyfin/jellyfin-kodi)** syncs the Jellyfin libraries into the library of Kodi. Kodi then feels like a local library. Version 2.0.0 came out in March 2026.
- **JellyCon** browses Jellyfin without a sync. It is lighter, but less polished.

Start with Jellyfin for Kodi. Check that it supports the Kodi version of LibreELEC before you install it.

### Music on the TV

Kodi plays the Music library of Jellyfin. While a song plays, it shows the cover art and the artist art, like YouTube Music. Music videos stay in their own library, so they only play when you choose one.

## Phones and PC

The main phone is Android. There is an iPhone as well.

- **Android:** the Jellyfin app for video, and [Finamp](https://github.com/jmshrv/finamp) for music. Both apps also work without the Google Play Store. Check F-Droid, or download the APK from the project.
- **iPhone:** the same apps, from the App Store.
- **PC:** the Jellyfin web client, at `https://jellyfin.iglo.local` at home.
- **Remote:** [Kore](https://kodi.wiki/view/Kore), the official Kodi remote app, can control the TV box from the Android phone.

## Away from home

Tailscale only. No ports open on the router.

- Iglo-OS already puts Tailscale in the manager container.
- Jellyfin gets its own address on the tailnet, like `https://jellyfin.<tailnet>.ts.net`, with a trusted certificate. See [tailscale.md](../tailscale.md).
- The PC and the phones need the Tailscale app.

Tailscale is a small company, not big tech, but its servers coordinate the tailnet. [Headscale](https://headscale.net) is a self-hosted version of that coordination server. We can switch to it later.

A public subdomain with port forwarding stays possible later. It would need a domain, a Let's Encrypt certificate, and a reverse proxy like Caddy. It also puts the Jellyfin login on the open internet, so we don't do it now.

## Sources

- [LibreELEC forum: an N100 that slips in and out of HDR](https://forum.libreelec.tv/thread/30266-x86-64-n100-slips-in-and-out-of-hdr-on-sony-bravia-8-xr80/)
- [LibreELEC 12.2 with Kodi 21.2 (August 2025)](https://betanews.com/2025/08/17/libreelec-12-2-arrives-with-kodi-21-2-omega-and-better-raspberry-pi-support/)
- [Jellyfin forum: an N100 as client and the HDMI cable](https://forum.jellyfin.org/t-combine-server-and-client-into-one-machine?pid=15042)
- [raspberry.tips: Jellyfin, Plex and Kodi on a Raspberry Pi (2026)](https://raspberry.tips/en/raspi-media-center/raspberry-pi-media-center-comparison)
- [Evetech: the best Raspberry Pi for a living room streamer](https://evezone.evetech.co.za/deep-dives/best-raspberry-pi-for-a-living-room-media-streamer)
- [CoreELEC forum: best device for CoreELEC in 2025 and 2026](https://discourse.coreelec.org/t/best-device-for-coreelec-in-2025-and-2026/52405)
- [Jellyfin for Kodi: the add-on on the Jellyfin mirror](https://nyc1.mirror.jellyfin.org/files/client/kodi/py3/plugin.video.jellyfin/addon.xml)
