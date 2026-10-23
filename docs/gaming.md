# Experimental Wolf / ES-DE proof of concept

This is a reproducible **hardware test path**, not a production Apps installer.
It prepares Wolf plus the upstream Games on Whales ES-DE image, which includes
RetroArch and standalone Dolphin. Image digests were checked on 22 October 2026.
No ROMs, BIOS files, console keys or copyrighted games are included.

The [Wolf Proxmox quickstart](https://games-on-whales.github.io/wolf/stable/user/quickstart.html)
currently requires a **privileged LXC**, broad device access and a Docker socket.
Treat the guest and all launched images as trusted host software; this is much
weaker isolation than the unprivileged Photos/Music/Inbox guests. If that model
is unsuitable, evaluate a separate VM with GPU assignment or separate gaming
hardware instead. Do not assume the same GPU can be assigned exclusively to a VM
and simultaneously used by the host's Jellyfin container.

## Prepare and create

Run only on a backed-up Proxmox VE 9 amd64 host after reviewing the scripts.
Nothing here is run automatically by opening the Iglo UI.

1. Confirm `/dev/dri/renderD128` is the intended Intel GPU. Verify the loaded
   driver on the host, and test Jellyfin hardware playback independently.
2. Follow the quickstart's virtual-input section: load `uinput` and `uhid`, review
   and install the upstream `85-wolf.rules`, reload udev, and confirm
   `/dev/uinput` and `/dev/uhid`. Make module loading persistent if the test passes.
   Review the rules rather than downloading arbitrary device-permission scripts.
3. Connect a **Games** binding in Iglo Storage, ideally using a read-only NAS
   account. On that share prepare `roms/{gb,gbc,gba,n64,gc,wii}` and `bios` folders.
   Use game dumps and BIOS files you supply yourself. ROM folders are read only
   to the emulator guest. Scraped art/metadata and saves must use the SSD profile.
4. Review local disk capacity for the emulator images and persistent profile.
   The test creates a 96 GB root disk, four cores, **6144 MiB** RAM, no swap and
   **onboot disabled**. It requires another 4096 MiB of host MemAvailable. This
   stays within a budgeted gaming allowance; it is not proof of acceptable FPS.
5. From the installed release's host scripts directory:

```sh
bash /usr/local/lib/iglo/gaming-poc/install.sh \
  --storage local-lvm --games-binding games --allow-privileged
```

Replace `games` with the real binding ID. The script prints the guest ID/IP.
It creates only a new guest and binding dependency, not a replacement host OS.
The guest shows in Iglo's general guest list; gaming has no dedicated catalog
update/remove flow yet. Failed creation attempts destroy their new guest while
leaving the Games share intact.

## Pair and configure on a PC first

1. Install the official Moonlight client on a PC. Add the gaming guest's IP.
2. Click the host in Moonlight to display its PIN. Read Wolf's logs inside the
   guest (`cd /opt/gaming-poc && docker compose logs --tail 100`). Open the
   indicated local pairing URL, replacing localhost with the guest IP, and enter
   that PIN. Pair only clients you recognize. Do not expose Wolf's control API
   or pairing endpoint to the internet.
3. Start at **1080p, 60 FPS, SDR, H.264**, with hardware decoding enabled and a
   wired connection. Enable Iglo's **Protect gaming for 2 hours** first and verify
   new background work stops. This timer is manual; session detection is not wired.
4. Launch **Retro games**. In ES-DE choose `/ROMs` as the ROM root. If it asks to
   create folders, create them on the NAS beforehand; do not grant write access
   just to satisfy onboarding. Disable ROM-directory media/gamelist writes.
5. Install/select RetroArch cores for GB/GBC/GBA/N64 from the native Core
   Downloader where necessary. Use standalone Dolphin for GameCube/Wii. Set
   Dolphin's Vulkan backend initially, native resolution first, then test 2×.
6. In RetroArch set save/state directories under `/home/retro/saves` and
   `/home/retro/states`. In Dolphin confirm its normal user-data folder is under
   `/home/retro`. Check the actual paths before playing something important.

Wolf persists the default app home under
`/etc/wolf/profile_data/moonlight-profile-id/Retro games` in this config.
Both PC and TV launch the same **Retro games** profile so sequential sessions
share settings and saves. Use **only one active session at a time**. The proof of
concept does not enforce a single-client limit; simultaneous writes can corrupt
saves. Save in-game and quit the emulator before disconnecting/switching clients.

## Acceptance tests on the target hardware

Run inside the gaming guest:

```sh
bash /opt/gaming-poc/diagnose.sh
```

Record the chosen render node and the **actual live encoder in Wolf logs**;
`vainfo` alone only proves capabilities. Check there is no CPU/software encoder
fallback. The ES-DE image must also pass its own Vulkan rendering/controller
tests: host VA-API success does not establish emulator rendering support.

Test one representative game from each required family: GB/GBC, GBA, N64,
GameCube, and a gamepad-friendly Wii game. Record the title, backend, internal
resolution, FPS/frame pacing, Moonlight network/decode times and visible input
latency. Test sound, one/two controllers, reconnecting and quitting to ES-DE.
Repeat a save/load after guest reboot and after switching from PC to a second
client. A Wii Remote/DolphinBar needs a separate tested USB/motion-input path;
ordinary Moonlight gamepad forwarding is not an equivalent replacement.

Run a Jellyfin playback stream alongside it, then an allowed background job.
Watch host RAM, Intel GPU usage, thermals and frame pacing. Lower emulator
resolution or defer more work before buying a GPU. End the gaming hold only
after exiting the emulator. Original Switch emulation is outside this test.

## Persistence, rollback and backup

Guest disk backups include `/etc/wolf` (pairing credentials, profile, normal saves,
states and caches) and the config. They exclude the read-only Games share. Keep
an additional versioned copy of important normal saves on a separate backup
target, then test restore into a fresh profile before trusting it. Save-state
compatibility across emulator versions is not guaranteed.

The POC does not self-update. Keep its pinned digests until acceptance succeeds;
record versions and back up the profile before testing a newer image. To retire
it, shut down and delete the test guest in Proxmox after backing up saves. Remove
its `iglo-<binding>.conf` systemd drop-in only after the guest is removed; the
Storage page then permits disconnecting the unused binding. Do not delete NAS ROMs.

Prepared and syntax/config checked locally. **Not installed, paired, streamed or
benchmarked on the target Intel/Proxmox hardware.** The TV client can only be
chosen confidently after these checks pass.

Sources: [Wolf configuration](https://games-on-whales.github.io/wolf/stable/user/configuration.html),
[ES-DE image](https://games-on-whales.github.io/wildlife/apps/es-de/),
[ES-DE build](https://github.com/games-on-whales/gow/tree/master/apps/es-de/build),
[base emulator image](https://github.com/games-on-whales/gow/tree/master/images/base-emu/build),
[Wolf API](https://games-on-whales.github.io/wolf/stable/dev/api.html).
