# Gaming (experimental)

The Gaming app runs Wolf with the upstream Games on Whales ES-DE image, which
includes RetroArch and standalone Dolphin. It streams retro games from your
Games share to Moonlight on a PC or TV box. Image digests were checked on
22 October 2026. No ROMs, BIOS files, console keys or copyrighted games are included.

The [Wolf Proxmox quickstart](https://games-on-whales.github.io/wolf/stable/user/quickstart.html)
currently requires a **privileged LXC**, broad device access and a Docker socket.
Treat the guest and all launched images as trusted host software; this is much
weaker isolation than the unprivileged Photos/Music/Inbox guests. If that model
is unsuitable, evaluate a separate VM with GPU assignment or separate gaming
hardware instead. Do not assume the same GPU can be assigned exclusively to a VM
and simultaneously used by the host's Jellyfin container.

## Prepare the host

Do this once, on a backed-up Proxmox VE 9 amd64 host. The installer checks each
step and stops if one is missing.

1. Confirm `/dev/dri/renderD128` is the intended Intel GPU. Verify the loaded
   driver on the host, and test Jellyfin hardware playback independently.
2. Follow the quickstart's virtual-input section: load `uinput` and `uhid`, review
   and install the upstream `85-wolf.rules`, reload udev, and confirm
   `/dev/uinput` and `/dev/uhid`. Make module loading persistent if the test passes.
   Review the rules rather than downloading arbitrary device-permission scripts.
3. Connect a **Games** binding in Iglo Storage, ideally using a read-only NAS
   account. On that share prepare `roms/{gb,gbc,gba,n64,gc}` and `bios` folders.
   Use game dumps and BIOS files you supply yourself. ROM folders are read only
   to the emulator guest. Scraped art/metadata and saves must use the SSD profile.

## Install from Apps

Choose **Install** on the Gaming card. The form asks for the Games share, the
local container storage, CPU, RAM and disk. It starts at four cores,
**6144 MiB** RAM and a 96 GB disk, and needs at least 4096 MiB, two cores and
64 GB. The host also needs another 4096 MiB of available RAM. Nothing installs
until you confirm the privileged container in the form; the host agent refuses
the answers without that confirmation.

The container gets the tags `iglo;gaming`, no swap and **onboot disabled**. A
failed install removes the new container and leaves the Games share intact.
**Update** pulls the pinned images again and keeps the pairings and saves.
**Remove** deletes the container; its backups stay.

## The Gaming page

The Gaming page shows whether the container runs and how many games each system
has in the roms folder. The host counts them on the share, so the library shows
while the container is stopped. A file or a folder counts as one game; hidden
files and scraper folders like `media` don't.

The **Library** shows every game with its cover, with a filter per system and a
search box. Covers come from two places, in this order:

1. **Your own cover** on the Games share, in `covers/<system>/`, named like the
   game file without its extension. For `roms/gba/Metroid Fusion (USA).gba` that
   is `covers/gba/Metroid Fusion (USA).png`. PNG, JPEG and WebP work.
2. **Box art from libretro's thumbnail server**, which needs no account. The
   manager looks up the full file name first, then the name without its tags
   like `(USA)`. Standard No-Intro and Redump file names match best. The manager
   shrinks each cover to a small JPEG once and keeps it in `.covers` in its data
   folder, which data backups skip. A game without box art is asked again after a
   week. libretro sees the names of the games that the manager looks up.

A game without either cover shows a placeholder with its title. Keep `covers`
next to `roms`, not inside a system folder, or ES-DE lists the images as games.

**Play** holds heavy background work for up to 8 hours first, then starts the
container. Its address then shows: add it as a host in Moonlight. **Stop** shuts
the container down and ends the hold. Save in the game and quit the emulator
before you stop. If the start fails, the hold ends again. Stopping the container
in Proxmox does not end the hold; it expires on its own.

While the container runs, the manager follows the event stream of Wolf, so it
sees a session start, pause or stop at once. Each event makes it ask Wolf for
its sessions. The host agent passes on only the names of those events, never
their data, which holds the stream keys. When the stream is down, the manager
asks every 10 seconds instead, and every minute while it is up. A running session holds heavy background work too, at least 15
minutes ahead, also when the container was started in Proxmox and not with
Play. It never shortens the hold of Play. The page shows which device plays,
at what resolution. Wolf's stream keys stay on the host.

Every screen shares the same saves, so only the first session may run. When a
second device starts one, the manager stops it at once, before its emulator
gets far enough to write saves, and logs a warning. Without the event stream
this takes up to 10 seconds. A device that
disconnects without quitting keeps its session. To switch to another screen,
quit the game on the first one, or press **End session** on the Gaming page.
Save in the game first: ending a session stops its emulator.

## Pair and configure on a PC first

1. Install the official Moonlight client on a PC. Add the gaming guest's IP.
2. Press **Play**, then click the host in Moonlight to display its PIN. The
   Gaming page lists the device under **Pair a device**, by its address. Type
   the PIN there and press **Pair**. Pair only clients you recognize. Do not
   expose Wolf's control API or pairing endpoint to the internet.

   Wolf keeps a pair secret for each waiting device. It stays on the host: the
   page gets a hash of it, and the agent looks the secret up again to pair.
   Without the Gaming page, Wolf's log (Logs page, Docker tab, gaming
   container) still has the pairing link. Replace localhost in it with the
   guest IP.
3. Start at **1080p, 60 FPS, SDR, H.264**, with hardware decoding enabled and a
   wired connection. Press **Play** on the Gaming page and verify that new
   background work stops. Once the stream runs, the page shows it as playing.
4. Launch **Retro games**. In ES-DE choose `/ROMs` as the ROM root. If it asks to
   create folders, create them on the NAS beforehand; do not grant write access
   just to satisfy onboarding. Disable ROM-directory media/gamelist writes.
5. Install/select RetroArch cores for GB/GBC/GBA/N64 from the native Core
   Downloader where necessary. Use standalone Dolphin for GameCube. Set
   Dolphin's Vulkan backend initially, native resolution first, then test 2×.
6. In RetroArch set save/state directories under `/home/retro/saves` and
   `/home/retro/states`. In Dolphin confirm its normal user-data folder is under
   `/home/retro`. Check the actual paths before playing something important.

Wolf persists the default app home under
`/etc/wolf/profile_data/moonlight-profile-id/Retro games` in this config.
Both PC and TV launch the same **Retro games** profile so sequential sessions
share settings and saves. The manager keeps one session at a time, so two
screens can't write the same saves at once. Save in-game and quit the emulator
before disconnecting or switching clients.

## Systems

Gaming covers Game Boy, Game Boy Color, Game Boy Advance, Nintendo 64 and
GameCube. Wii games are left out on purpose: they need Wii Remotes with motion
and pointing, which Moonlight can't carry, and an original Wii plays them on
the TV with the remotes it was made for.

The installer reads the device number of `hidraw` on the host and allows it in
the emulator container. Wolf's virtual pads need it, and the Wolf quickstart's
244 only fits some kernels.

## Acceptance tests on the target hardware

Run inside the gaming guest:

```sh
bash /opt/gaming/diagnose.sh
```

Record the chosen render node and the **actual live encoder in Wolf logs**;
`vainfo` alone only proves capabilities. Check there is no CPU/software encoder
fallback. The ES-DE image must also pass its own Vulkan rendering/controller
tests: host VA-API success does not establish emulator rendering support.

Test one representative game from each required family: GB/GBC, GBA, N64,
and GameCube. Record the title, backend, internal
resolution, FPS/frame pacing, Moonlight network/decode times and visible input
latency. Test sound, one/two controllers, reconnecting and quitting to ES-DE.
Repeat a save/load after guest reboot and after switching from PC to a second
client.

Run a Jellyfin playback stream alongside it, then an allowed background job.
Watch host RAM, Intel GPU usage, thermals and frame pacing. Lower emulator
resolution or defer more work before buying a GPU. Press Stop only after
exiting the emulator. Original Switch emulation is outside this test.

## Persistence, rollback and backup

Guest disk backups include `/etc/wolf` (pairing credentials, profile, normal saves,
states and caches) and the config. They exclude the read-only Games share. Keep
an additional versioned copy of important normal saves on a separate backup
target, then test restore into a fresh profile before trusting it. Save-state
compatibility across emulator versions is not guaranteed.

Gaming does not self-update. A new Iglo-OS release can pin newer image digests;
back up the profile before you run **Update**. To retire it, back up the saves and
choose **Remove** on the Apps page. That also removes the share dependency, so the
Storage page can disconnect the unused binding. Removing never touches NAS ROMs.

Tested with unit tests and the dev mock, and syntax/config checked locally.
**Not installed, paired, streamed or benchmarked on real Intel/Proxmox hardware.**

Sources: [Wolf configuration](https://games-on-whales.github.io/wolf/stable/user/configuration.html),
[ES-DE image](https://games-on-whales.github.io/wildlife/apps/es-de/),
[ES-DE build](https://github.com/games-on-whales/gow/tree/master/apps/es-de/build),
[base emulator image](https://github.com/games-on-whales/gow/tree/master/images/base-emu/build),
[Wolf API](https://games-on-whales.github.io/wolf/stable/dev/api.html).
