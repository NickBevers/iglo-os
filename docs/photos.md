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

Open Immich from Apps and create the first admin account on your trusted LAN.
Create an API key granting queue read/update access, then open **Background work**,
select the Proxmox node, enter the key, and enable control. Installation prefills
an empty server URL; existing connections are preserved. The key is never created
before the Immich admin exists. The queue API is exercised by the local fixture,
but must also be checked against the installed release before relying on it.

CPU processing is the default. No GPU performance or acceleration is claimed.
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
