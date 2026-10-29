# Photos installation and recovery

The Apps page installs a Debian 13 unprivileged LXC, tagged `iglo;photos`,
with Docker and the **official Immich v3.3.5** stack. `compose.yaml` is the
unchanged release asset from
<https://github.com/immich-app/immich/releases/tag/v3.3.5>.
`compose.override.yaml` adds CPU/memory ceilings and SSD locations.
Updates use the version bundled with Iglo-OS, not an unreviewed `latest` tag.

Connect two writable [scoped SMB bindings](scoped-storage.md): `photos` and
`backups`. The container mounts the Backups share read only: the backups run
on the host. Select local NVMe container storage, 4 cores, 8192 MiB RAM and at least
64 GB of local disk (larger libraries need more for derivatives). The installer
checks host MemAvailable and keeps at least 4096 MiB outside the new guest.
This is admission checking, not a reservation against other future workloads.

| Location | Contents | Included in a guest disk backup? |
| --- | --- | --- |
| `/data/photos` | NAS originals, profile photos, Immich folder markers | No |
| `/data/backups` | The Backups share, read only. restic on the host writes `immich-restic` there | No |
| `/var/lib/immich/postgres` | Live PostgreSQL database on SSD | Yes, but live snapshots alone are not app-consistent |
| `/var/lib/immich/{thumbs,encoded-video,model-cache}` | Rebuildable derivatives and models on SSD | Yes |
| `/opt/photos/.env` | Generated database password, version and paths | Yes; treat guest backups as secrets |

### The first admin and Background work

The install form has **Make the first Immich admin**, on by default. Enter the
email, name and password to log in to Immich with. After the install, Iglo-OS
makes that admin and sets the guided setup defaults below. When
[Background work](background-work.md) has no Immich yet, Iglo-OS also makes an
API key that can only read and pause the queues (`queue.read` and
`queue.update`). It saves the key with the node of the host and turns queue
control on. The other Background work settings stay as they are. The guided
setup does the same, and also turns on the overnight window.

Iglo-OS never replaces a connection you made. When Background work already has
a key, owns paused queues or points at another server, only the admin is made.
The password stays in the memory of the manager until the install ends. It never
reaches the host agent, a log or a file. A failed install, a retry or a restart
of the manager drops it, and you make the admin in Immich instead. The key is
stored like a key you enter by hand: in `workloads.json` (mode 0600), never sent
to the browser and never in `iglo.yaml`. An Immich that already has an admin is
left as it is.

Turn the option off to restore a library from a backup when Immich first starts
(see the rehearsal below), or to make the admin yourself. Then open Immich from
Apps, create the first admin on your trusted LAN and an API key with queue read
and update access, and enter it in **Background work**. The install fills an
empty server URL either way.

Checked against Immich v3.3.5 on 29 October 2026: `GET /api/server/config`
(`isInitialized`), `POST /api/auth/admin-sign-up` (email, password, name),
`POST /api/auth/login` (answers `accessToken`), `POST /api/api-keys` (name and
permissions, answers `secret`), and the `queue.read` and `queue.update`
permissions that `GET /api/queues` and `PUT /api/queues/{name}` ask for. Sources:
[auth controller](https://github.com/immich-app/immich/blob/v3.3.5/server/src/controllers/auth.controller.ts),
[auth schema](https://github.com/immich-app/immich/blob/v3.3.5/server/src/dtos/auth.dto.ts),
[API key controller](https://github.com/immich-app/immich/blob/v3.3.5/server/src/controllers/api-key.controller.ts),
[API key schema](https://github.com/immich-app/immich/blob/v3.3.5/server/src/dtos/api-key.dto.ts),
[queue controller](https://github.com/immich-app/immich/blob/v3.3.5/server/src/controllers/queue.controller.ts),
[permissions](https://github.com/immich-app/immich/blob/v3.3.5/server/src/enum.ts).
The dev mock plays these calls; a real Immich still has to confirm them on the
hardware.

When the host has a GPU (`/dev/dri/renderD128`), the installer passes its
render node to the container and loads `compose.gpu.yaml`. Machine learning
then uses the OpenVINO image of Immich, which runs face detection and smart
search on an Intel GPU by itself. Video conversion needs one switch in Immich:
**Administration > Settings > Video transcoding > Hardware acceleration >
Quick Sync**. Without a GPU, Immich runs on the CPU as before. The machine
learning container gets 2560 MiB, because OpenVINO needs room while it compiles
its models. None of this is measured on the target hardware yet.
Use one admitted heavy queue, 4 GiB headroom and the overnight window initially.
Active jobs finish after a pause; limit native worker concurrency too.

## Backups

Photos backs up every night at **01:00**, before the processing window of
Background work (02:00) and the Proxmox backups (03:00). Change the time, the
retention and the folder off the NAS with **Backup settings** on the Photos
card. The card shows the last backup, the size of the backups on the NAS and
off it, and the last check.

[restic](https://restic.net/) 0.19.1 (BSD-2-Clause) runs on the Proxmox host,
next to the Photos container. The host downloads the pinned release once and
checks its SHA-256 digest. Each backup:

1. Dumps the database with `pg_dump` inside the container. Immich keeps
   running, so there is no downtime. Immich v3.3.5 allows a backup while the
   server runs when the database comes first and the files second ("Backup
   ordering" in its backup guide, checked 29 October 2026). The worst case is a
   file that the database doesn't know yet, never a database entry without its
   file. A cut dump is refused. The dump goes to a folder of this run only,
   readable by root, and moves to `/var/lib/iglo/photos-backup/database` once
   it is complete. It is deleted at the end.
2. Saves the dump and the Photos share in one restic snapshot, in
   `immich-restic` on the Backups share. The derivatives (`thumbs`,
   `encoded-video`) and Immich's own dumps in `backups` are left out, as the
   upstream guide allows. Only new and changed files are read. SMB gives no
   stable inode numbers, so restic compares sizes and times (`--ignore-inode`).
   The snapshot is tagged `iglo-photos-pending` until restic finished it
   without errors, then `iglo-photos`. When restic could not read some files
   (exit code 3), it stays pending. Only `iglo-photos` snapshots count for the
   retention and for Verify recovery. Pending snapshots older than a day are
   forgotten by the next backup.
3. Forgets old snapshots and prunes their data: 7 daily, 4 weekly and 12
   monthly by default (`restic forget --prune`).
4. Does the same into `iglo-photos` in the folder off the NAS, when one is set.

The nightly backup waits while Proxmox backups or updates run, and tries
again each minute for 6 hours. While a Photos backup or check runs, Background
work starts no new heavy work: no Immich analysis, no Inbox conversions and no
Music downloads. The manager asks the host agent what runs, so this also
holds after the manager restarted, and during the backup at the start of a
Photos update. A backup by hand from the card always starts at once.

One backup or check runs at a time on the host: the nightly one, one from the
card, one by hand with `install.sh` and the backup of an update share one lock
(`/run/lock/iglo-photos-backup.lock`). A second one stops with "Another Photos
backup or check runs on this host", and leaves the state of the first alone.

### The copy off the NAS

The Photos share and the Backups share are on one NAS, so one failure can take
both. Attach a disk to the host, mount it (for example at `/mnt/backup-disk`
in `/etc/fstab`), and enter that folder in **Backup settings**. The host agent
refuses a folder that is not on a mounted local disk: the root disk, another
partition of the system disk and network shares like the NAS are refused. For
ZFS, the disks of the vdevs of the pool count (`zpool list -v -H -P -L`,
OpenZFS 2.4.4 in Proxmox VE 9, checked 29 October 2026). When the disk of the
system can't be found, every folder is refused. The repository folder
`iglo-photos` in it must be a real folder on the same file system: a link, for
example to the NAS, or a share mounted there is refused.

When the agent saves the folder, it writes `.iglo-photos-disk` with a random
ID in it, and keeps the ID in its settings. The backup only writes to the disk
while it finds that marker: it checks the folder again before every run, and
the marker and the mounted file system right before and after each restic
command. So an empty mount point of an unplugged disk never gets a repository
on the root disk. When the disk is unplugged, or another disk is in its place,
the backup on the NAS still runs, and the card and the notification say that
the copy off the NAS is missing. A snapshot made while the disk went away stays
pending. After formatting the disk, save the folder again. The last result off
the NAS keeps the ID of its disk: after another disk is saved, the card shows
"the next backup makes the first copy", without the time or size of the old one.

### Shares that go away during a run

Each restic command runs in a private mount namespace (`unshare --mount
--propagation private`). There the mounts can't change anymore: when the NAS
or the disk goes away on the host, restic keeps the share or disk it started
with, and never sees an empty mount point. Otherwise restic would back up an
empty folder as if the library were empty, or write a repository to the root
disk, because its local backend makes missing folders itself
(`internal/backend/local/local.go` of v0.19.1).

Before each command, in its namespace, and again right after it, the backup
checks that the Photos share, the Backups share and the disk off the NAS are
still the mounts it checked at the start (`findmnt`). For the Photos share it
also checks the `.immich` files that Immich v3.3.5 writes into `library`,
`upload`, `profile` and `backups` when it starts (`storage.service.ts`, checked
29 October 2026). When a check fails after the backup, the snapshot stays
pending and the run fails with a message that names the share. Folders of a
run that stopped without cleaning up, like at a power cut, are removed by the
next run.

### The backup password

The repositories are encrypted with the **backup password** that you type when
you install Photos, on the Apps page or in the guided setup. Save it in your
password manager first: Iglo-OS seals it on the host with `deploy/secrets.sh`,
like the VPN login, and can't show it again. Without it, no backup can be read,
also not the copy off the NAS after the host is lost.

It goes from the form to the host agent, which checks it (12 to 128
characters, one line), and on to the installer in a root-only answers file that
the installer removes at once. The installer checks it again and seals it.
restic gets it on stdin. It is never in a command line, a log, the retry file
of a failed install, a plan file or the saved setup draft, `.env`, a backup, an
answer of the API or the Photos container. A failed install asks for it again,
and a setup that restarts drops it like the other passwords.

When backups already exist, on the Backups share or on the disk off the NAS,
the password must open them. The installer checks that on the host before it
makes the container or seals anything (a guarded `restic cat config`), so a
wrong password stops the install with "The backup password does not open the
Photos backups…". This runs in the installer because the shares are mounted on
the host there, restic runs on the host, and nothing exists yet that needs to
be undone. A sealed password that opens the backups is never replaced by one
that doesn't.

A backup without a sealed password stops with "Photos has no backup password.
Set one with iglo-photos-key set on the host." That happens for a Photos from
before the password, or when the TPM of the host was reset and the sealed
password can't be read anymore. As root on the host:

```sh
iglo-photos-key set    # asks for the password twice, without showing it
```

It checks the same 12 to 128 characters, and that the password opens every
Photos backup on a mounted Backups share and off the NAS. Then it seals it.
Changing the password of existing backups (`restic key passwd`) is not part of
Iglo-OS. The password stays when Photos is removed, so a new install continues
the same backups.

### Verify recovery

**Verify recovery** runs `restic check` on the NAS repository and reads a
random 5% of its data. It restores the database of the latest snapshot into a
temporary PostgreSQL container with no network and no host folders, and checks
the restored public table count against the saved count. The container and its
volume are removed afterwards. When a folder off the NAS is set, its copy gets
the same check. When the disk is missing, has no copy yet, or goes away during
the check, Verify recovery fails and says so: it never reads as checked. This
tests the repository and the database restore, not every
Immich feature. Leave roughly 2 GiB of free guest RAM and enough local disk for
the temporary database.

The same from the Proxmox host (replace `170` with the real ID):

```sh
/usr/local/lib/iglo/stacks/photos/install.sh --backup --ctid 170
/usr/local/lib/iglo/stacks/photos/install.sh --verify --ctid 170
```

Updates back up first, and stop when the backup fails. They do not run Verify
recovery; do that before an important upgrade. Automatic guest-disk rollback is
disabled for scoped apps because it cannot rewind NAS originals alongside the
database. If an update fails, inspect the failed operation and restore
deliberately from a snapshot. The generic Proxmox Backups page covers guest
disks, not SMB data.

## Recovery

On a new host, after the old one is lost:

1. Install Iglo-OS and connect the Photos and Backups shares on the Storage
   page. For the copy off the NAS, mount the backup disk on the host.
2. Install Photos with the **same backup password**. The installer checks that
   it opens the backups on the NAS. To restore the database, turn off **Make
   the first Immich admin**, so Immich offers **Restore from backup** when it
   first starts.
3. Restore, as below. `<backups>` and `<photos>` are the IDs of the shares on
   the Storage page. restic asks for the backup password:

   ```sh
   R=/usr/local/lib/iglo/bin/restic-0.19.1
   REPO=/mnt/iglo/shares/<backups>/immich-restic   # or <disk>/iglo-photos off the NAS
   $R -r $REPO snapshots --tag iglo-photos
   ```

   When the originals on the Photos share are gone, stop the Immich services
   and restore them from a snapshot. restic restores the full path under the
   target. Copy the content of `/tmp/photos-restore/mnt/iglo/shares/<photos>/`
   into the share:

   ```sh
   $R -r $REPO restore <snapshot> --include /mnt/iglo/shares/<photos> --target /tmp/photos-restore
   ```

   Dump the database of the same snapshot, compressed for Immich, open Immich
   and choose **Restore from backup** with that file. The database uses fixed
   in-container `/data` paths, so the share keeps the expected layout:

   ```sh
   $R -r $REPO dump <snapshot> /var/lib/iglo/photos-backup/database/database.sql | gzip > database.sql.gz
   ```

4. Regenerate missing thumbnails and encoded videos. Confirm counts, users,
   albums, search and representative photo and video downloads.

Rehearse this once before you rely on it: in a **separate recovery guest** with
its own Photos-role share, on an isolated network, never against the live
library. Do not point two Immich instances at the live writable library, and
never test deletion against the live share.

Follow the version-specific upstream recovery guide:
<https://docs.immich.app/administration/backup-and-restore/>.
Do not delete a production Photos container until a backup is verified.

## Verification boundary

Agent/API tests cover typed requests, retry storage and validation. Development
installs/recovery logs explicitly say **simulation** and create no real guests.
The mini-PC install, SMB outage/reboot, a real library restore and end-to-end
phone backup remain hardware acceptance checks, not completed deployment claims.

Local Docker validation on 22 October 2026 passed the merged volume configuration
and a real SQL dump/restore using the pinned PostgreSQL image, two disposable
network-isolated databases and a sample record. This was not a complete Immich
library or phone-backup rehearsal.

`bash dev/check-photos-recovery.sh` runs the backup and the check of
`backup.sh` and `recovery.sh` with the pinned restic in a Linux container, on
real repositories in temporary folders. Proxmox, Docker, ZFS and most mounts
are faked. The Photos share is a real tmpfs, and the backup really runs restic
in its own mount namespace, so the test unmounts the share on the host during
a backup. It covers the snapshot of the dump with the originals, one run at a
time, abandoned staging folders, pending snapshots and their cleanup, the
retention, a Photos share lost during a backup or without its Immich markers,
a Backups share lost during a backup, the refusal of a cut dump and a missing
password, the copy off the NAS, the disk marker and the disk of the last
result, a disk unplugged before and during the copy and the check, a
repository folder that links to the NAS and a ZFS pool on the system disk. It
is separate from the real PostgreSQL dump/restore check.

## Guided setup defaults and model licences

Checked against Immich v3.3.5 on 28 October 2026. Guided setup, and an Apps
page install that makes the first admin, use the new admin's temporary session
to read and update `/api/admin/config`. It limits
video conversion, face detection, smart search and OCR to one job per queue,
sets video conversion to `required` with two CPU threads, and leaves other
configuration intact. The long-lived key still has only `queue.read` and
`queue.update`. Existing initialized libraries are never reconfigured.

Face grouping is off by default for new guided setups and Apps page installs.
Its `buffalo_l` weights come from InsightFace, whose model terms restrict use to
non-commercial research. This is separate from Immich's application licence.
The setup and the Apps page install form have an explicit opt-in for those
weights. The setup saves it in the secret-free plan.
This does not change the model terms or establish unrestricted use. Manual
admin creation inside Immich retains upstream defaults: disable facial
recognition there if you want the same policy. Existing people and photos
are never deleted by this setup change.

The ML compose override uses one worker, two request threads and bounded CPU
inference pools. Immich still unloads non-preloaded models after its normal
idle timeout. These are conservative concurrency limits, not measured RAM
savings or a promise of faster backlog processing.

Sources: [configuration controller](https://github.com/immich-app/immich/blob/v3.3.5/server/src/controllers/config-admin.controller.ts),
[configuration schema](https://github.com/immich-app/immich/blob/v3.3.5/server/src/dtos/config.dto.ts),
[ML settings](https://github.com/immich-app/immich/blob/v3.3.5/machine-learning/immich_ml/config.py),
[model card](https://huggingface.co/immich-app/buffalo_l),
[InsightFace model terms](https://github.com/deepinsight/insightface#license).
