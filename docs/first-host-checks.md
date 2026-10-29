# First host checks

Much of Iglo-OS has only run on the dev mock, in unit tests and in the
recovery fixtures. This list is what only a real Proxmox host, a real NAS and
a real browser can show. Work through it once on a new install, in this order.
Each check says what to do, what you should see, and the PR that left it open.

Mark a check done only when you saw the result yourself. A check that fails is
a bug: write down what you did and what you saw, and keep going where the next
check doesn't depend on it.

## 1. The host

- [ ] **TPM.** On the host, `systemd-creds has-tpm2` says yes for firmware,
      driver and system. Without a TPM, secrets are still sealed, but only with
      the host key. (#93)
- [ ] **Disk budget.** On the setup's review page, the storage total and free
      space match what Proxmox shows for the LVM-thin pool. A plan bigger than
      the storage is refused. (#81)

## 2. Install and deploy

- [ ] **First install.** `make deploy HOST=root@<host>` on a host without
      Iglo-OS runs the first install, and `https://iglo.local` opens. (#79)
- [ ] **The root-only socket.** On the host, `/run/iglo-agent/admin.sock`
      exists and only root can use it. (#79)
- [ ] **Upgrade.** Tag a newer version and deploy again. The Updates page shows
      the new version and the log of the upgrade. Deploying the same version
      again is refused. (#78, #79, #80)
- [ ] **Rollback.** Deploy a build whose manager can't pass `iglo ready`, for
      example one that exits at start. Within about a minute the previous
      version comes back, and the Updates page says so. (#78)
- [ ] **Reach the host when the manager is down.** Follow
      [the Tailscale steps](tailscale.md#reach-the-host-when-the-manager-is-down)
      and reach the host from away from home with the manager container
      stopped. (#78)

## 3. The setup and the browser

Do these with the browser's developer tools open, and look for errors.

- [ ] **Long forms.** Fill a long form halfway, reload, and the notice offers
      your answers back. Esc or a click outside asks before it closes. (#77)
- [ ] **After an operation.** An install, update or removal ends with a line
      that says what happened. A failed step shows its log, and form errors
      show next to their field. (#72)
- [ ] **Pages to open once:** the setup review page, the Updates card, the
      Photos install form and Photos card, the FlareSolverr switch, the
      Jellyfin form, the Background work page with one node, the Gaming
      pairing card and the Headscale form. (#71, #75, #76, #80, #81, #89, #90,
      #91, #93)
- [ ] **Quiet when idle.** Leave the dashboard open for 10 minutes. The host's
      CPU in Proxmox stays flat, with no steady stream of `pct` processes. (#68)

## 4. Photos

- [ ] **Install** Photos with **Make the first Immich admin** on and a backup
      password from your password manager. You can log in to Immich with that
      admin. (#91, #93)
- [ ] **Background work connects by itself.** A few minutes after the install,
      Background work shows Immich as connected, with this host's node. (#91)
- [ ] **Back up Photos** on the Apps page finishes, and the Photos card shows
      the snapshot. Photos keeps working during the backup. (#93)
- [ ] **Off the NAS.** Set a folder on a USB disk in Backup settings. The next
      backup copies to it. The system disk and the NAS are refused. (#93)
- [ ] **Unplug during a copy.** Pull the USB disk during a backup. The off-NAS
      copy shows as missing, and nothing new appears on the system disk under
      the empty mount point. (#93)
- [ ] **NAS gone during a backup.** Stop the Backups share (or the NAS) during
      a backup. The run fails with a clear message and nothing is promoted.
      (#93)
- [ ] **Verify recovery** passes, and fails with the USB disk unplugged.
      (#93)
- [ ] **Hold.** While a backup or check runs, Background work says it is
      holding. Restart the manager container during a backup: it still holds.
      (#93)
- [ ] **Recovery rehearsal.** Follow [Recovery](photos.md#recovery) once on a scratch
      container: restore the database and a few originals with the backup
      password from your password manager. (#93)

## 5. Jellyfin and the media stack

- [ ] **Libraries.** With a Music share and the Inbox in place, Jellyfin gets
      the Music, Music Videos and Home Videos libraries by itself. (#89)
- [ ] **Update** Jellyfin from the Apps page with its API key. The libraries
      stay, and the container comes back after the reboot. (#89)
- [ ] **FlareSolverr** is off after a media install. Turn it on: Prowlarr gets
      the FlareSolverr proxy. Turn it off: the proxy goes and the container
      stops using the memory. (#90)
- [ ] **Container updates.** Updating a guest from the Updates page works for
      each kind of app. (#92)

## 6. Inbox, Music and Background work

- [ ] **Inbox.** Drop a file in the Inbox. **Scan Inbox now** picks it up, and
      it ends up in Home Videos. (#84, #88)
- [ ] **NAS disks sleep.** Outside the processing window, the NAS disks spin
      down and stay down. The NAS's own log shows it. (#88)
- [ ] **Backups hold heavy work.** During the nightly Proxmox backup, Background
      work shows "Backups are running" and starts no new heavy work. (#87)
- [ ] **Backup history.** The overview shows the last nightly backup, and a
      failed backup turns the card red. (#71)

## 7. Gaming and notifications

- [ ] **Pair** a Moonlight client from the Gaming page with its PIN, press
      **Play**, and play with the controller. A second device can't start a
      second session. (#76)
- [ ] **ntfy.** A test message reaches your phone, from ntfy.sh or your own
      server. A failed backup sends one too. (#86)
- [ ] **Headscale,** if you use it: the gateway is filled in and the prefix
      shows. (#75)
