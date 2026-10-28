# Backups

The Backups page makes a normal Proxmox backup job (`iglo-backup`), so it also runs while Iglo-OS is down, and you can see it in the Proxmox UI.

- Choose the days, the time, the storage and how many daily, weekly and monthly backups to keep. By default, every guest is backed up at 03:00 on `local`, the disk of the host. Keep them on the NAS instead, see below.
- New containers and VMs are included automatically. Turn one off with its switch.
- Back up one guest now, restore a backup or delete it. A restore shuts the guest down, restores it to the storage of its current disk, and starts it again. The host agent restores containers, as root: Proxmox lets only `root@pam` restore a bind mount, and every app container has one, for a NAS share or its secrets (pve-container `7d99e49`).
- **Test** a container backup without touching the container: see below.
- Iglo-OS can't restore its own container. Use `iglo-restore` on the host (see below).
- Failed scheduled backups are sent to Telegram and ntfy.

The host agent creates the job with checked settings, so the API token needs no `Sys.Modify`. The token only has `VM.Backup` and `Datastore.AllocateSpace` for backups.

## Restore checks

A backup only proves that it was written. A restore check proves that it restores:

1. The host agent restores the backup to a new container, with an ID from 9900 to 9999 and a new MAC address.
2. Before it starts, the copy loses its network, every mount point, device passthrough and any hook script, also its NAS shares and its secrets folder. Privileged containers and custom raw LXC settings are refused, so Gaming cannot be checked this way. It can't reach the original's data, and it gets the tag `iglo-check`, so the Apps page doesn't take it for an app.
3. It starts once. Its apps fail without their shares, so a "degraded" system counts as booted.
4. The copy is removed, also when a step failed.

At 05:00, Iglo-OS checks the newest backup of one overdue container. Each container is due 30 days after its last successful check, with at most one attempt per night. A failed check stays overdue; rotation gives other containers their turn before retrying. It waits while a game runs or another backup operation runs. **Test** next to a backup on the Backups page runs one now. The result shows on the Backups page and goes to Telegram.

The copy needs room on the storage of the container: about three times the size of the backup, plus 2 GiB. With less, the check fails and says so. A restore check is not tested on Proxmox yet.

## Backups on the NAS

Backups on the disk of the host are gone when that disk is. To keep them on the NAS, connect a share with the role **Backups** and write access on the Storage page, and press **Use for Proxmox backups**. Then choose its storage, `iglo-<id>`, on the Backups page.

The host agent registers the share's own mount as a Proxmox storage of type `dir`, with `is_mountpoint`. Proxmox gets no copy of the SMB password, and refuses to back up when the share isn't mounted, instead of filling the disk of the host. **Stop using for Proxmox backups** only removes the storage from Proxmox: the backups on the NAS stay. It is refused while the backup job still uses that storage, and a share that Proxmox uses can't be disconnected.

On a new host, connect the share again with the same ID and press the button again. Then `iglo-restore` and `./install.sh --restore` find the backups on it.

## Restoring the manager

The backup job also backs up the manager container. To restore it, run this as root on the Proxmox host:

```bash
iglo-restore                                                          # choose from the backups of the manager
iglo-restore nas:backup/vzdump-lxc-120-2026_09_29-03_00_02.tar.zst   # or give one
```

It stops the manager, restores the backup to the storage of its current disk, and waits until the manager runs again.

On a new Proxmox host, extract the release bundle and run `./install.sh --restore <backup>` with a backup on the NAS. It also installs the host agent and makes a new API token, because the old token only works on the old host.

## Data backup

The Settings page can also download an encrypted copy of the data of Iglo-OS: the account, tokens, settings, history and the certificate authority. Use it when the Proxmox backups are gone. You need your password and a passphrase of at least 12 characters. Keep the passphrase: without it, nobody can open the file.

To restore it, install Iglo-OS, log in, and upload the file on the Settings page. Iglo-OS restarts with the data from the file, and you log in with the account from the backup. The file uses AES-256-GCM with a key from Argon2id.
