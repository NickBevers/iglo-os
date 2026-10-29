# Photos installation and recovery

The Apps page installs a Debian 13 unprivileged LXC, tagged `iglo;photos`,
with Docker and the **official Immich v3.3.5** stack. `compose.yaml` is the
unchanged release asset from
<https://github.com/immich-app/immich/releases/tag/v3.3.5>.
`compose.override.yaml` adds CPU/memory ceilings and SSD locations.
Updates use the version bundled with Iglo-OS, not an unreviewed `latest` tag.

Connect two writable [scoped SMB bindings](scoped-storage.md): `photos` and
`backups`. Select local NVMe container storage, 4 cores, 8192 MiB RAM and at least
64 GB of local disk (larger libraries need more for derivatives). The installer
checks host MemAvailable and keeps at least 4096 MiB outside the new guest.
This is admission checking, not a reservation against other future workloads.

| Location | Contents | Included in a guest disk backup? |
| --- | --- | --- |
| `/data/photos` | NAS originals, profile photos, Immich folder markers | No |
| `/data/backups/immich` | Matched database/originals backups | No |
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

## Recovery operations

**Back up Photos** pauses the currently running Immich server/ML services, dumps
PostgreSQL, copies original files to a new immutable directory under the Backups
share, writes checksums and restarts those services. It never deletes or replaces
NAS originals. A large library can mean hours of downtime and a full extra copy.
There is no automatic pruning. Monitor free space and maintain retention yourself.
Failed copies remain `.partial.*` and never replace the `latest` pointer.

**Verify recovery** checks the latest backup's file checksums, creates a temporary
PostgreSQL container with no network or host bind mounts, restores the SQL dump,
and checks the restored public table count against the saved count. The database
container and its temporary volume are removed afterwards. This tests database
restore and file integrity, not every Immich UI feature or your off-site copies.
Leave roughly 2 GiB of free guest RAM and enough local disk for the temporary DB.

These commands do the same from the Proxmox host (replace `170` with the real ID):

```sh
pct exec 170 -- bash /opt/photos/recovery.sh backup
pct exec 170 -- bash /opt/photos/recovery.sh verify
```

Updates require a successful matched backup first. They do not require a prior
verification automatically; run Verify recovery before an important upgrade.
Automatic guest-disk rollback is disabled for scoped apps because it cannot rewind
NAS originals alongside the database. If an update fails, inspect the failed
operation and recover deliberately from the matched export.
Keep another encrypted copy off the NAS: two shares on one Umbrel are still one
failure domain. The generic Proxmox Backups page covers guest disks, not SMB data.

## Restore rehearsal before phone backups

1. Create a separate Photos-role share and a **new recovery guest** on an isolated
   network. Do not point two Immich instances at the live writable library.
2. Install the same bundled Immich version. Stop the recovery guest's Immich
   services. Copy the selected backup's `originals/` into its fresh Photos share.
3. Start it, open Immich onboarding and choose **Restore from backup**. Supply
   `database.sql.gz`. The database uses fixed in-container `/data` paths, so the
   separate share preserves the expected path layout.
4. Regenerate missing thumbnails and encoded videos. Confirm counts, users,
   albums, search and representative photo/video downloads. Never test deletion
   against the live share. Destroy the recovery guest only after recording results.

Follow the version-specific upstream recovery guide:
<https://docs.immich.app/administration/backup-and-restore/>.
Do not delete a production Photos container until a recovery backup is verified.
Power loss while writers are stopped may require manually running
`cd /opt/photos && docker compose start immich-server immich-machine-learning`
inside the guest; inspect any partial backup first.

## Verification boundary

Agent/API tests cover typed requests, retry storage and validation. Development
installs/recovery logs explicitly say **simulation** and create no real guests.
The mini-PC install, SMB outage/reboot, a real library restore and end-to-end
phone backup remain hardware acceptance checks, not completed deployment claims.

Local Docker validation on 22 October 2026 passed the merged volume configuration
and a real SQL dump/restore using the pinned PostgreSQL image, two disposable
network-isolated databases and a sample record. This was not a complete Immich
library or phone-backup rehearsal.

The recovery shell also passed a Linux fixture test for failed-dump cleanup,
matched export and corrupt-file rejection (`bash dev/check-photos-recovery.sh`).
That fixture simulates Docker/SMB operations and is separate from the real
PostgreSQL dump/restore check.

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
