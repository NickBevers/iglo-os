# Scoped SMB bindings

Use **Server → Storage** to connect a dedicated SMB share with a folder role.
Use a separate NAS account restricted to that share where the NAS allows it.
An Umbrel doesn't; see [On an Umbrel](#on-an-umbrel). Binding IDs are immutable.
The API never returns credentials; they live in root-only files under
`/etc/iglo/storage/` on the Proxmox host, outside manager exports.
After rebuilding a host, reconnect shares with their original IDs before starting
guests. Keep NAS credentials in your password manager. A manager data backup does
not replace this step or a backup of NAS files.

Bindings mount at `/mnt/iglo/shares/<id>` using SMB 3.0, `nosuid,nodev,noexec`,
with UID 100000 and GID 101000 for default unprivileged LXC mappings. Custom ID
maps are unsupported. Each installer binds only the required folder, with an
explicit read-only flag where possible. systemd dependencies mount it before the
guest starts. The empty underlying host directory has mode 0000, preventing
unprivileged applications from filling the host disk if a mount is missing.
Network loss after mounting can still stall SMB I/O; it does not switch to a
local directory. This is availability protection, not NAS redundancy.

Disconnect refuses if any LXC config references the mount or a subdirectory,
including stopped containers, or when Proxmox keeps its backups on the share
(see [backups.md](backups.md#backups-on-the-nas)). It removes only the mount and host credential
files, never NAS contents. Change credentials by removing an unused binding and
reconnecting it. Every installer, Jellyfin and the media stack included, uses
`deploy/storage.sh`.

The local dev fixture labels its mounts as simulated and does not contact a NAS.
Unit tests exercise validation, secret redaction, consumer protection and mount
failure. Real mount and guest boot behavior must be verified on Proxmox before
entrusting production data to it.

## On an Umbrel

An Umbrel has one SMB login for every folder its owner shares. The username is
`umbrel`, and the password is in **Settings > File sharing**. It is not your
Umbrel login. So use **One account for all shares** in the setup, and the same login
for each share on the Storage page. Each container still mounts only its own
folder, but the host holds a login that opens every shared folder.

In **Settings > File sharing**, choose **Specific folders** and add only the
folders that Iglo-OS needs. Don't share the whole Home folder, or that one
login opens all of it.

Member accounts have their own SMB login, but a member can only share folders
in its own Home over SMB, not the owner's Videos or Photos. Iglo-OS doesn't make
these accounts for you, because umbrelOS has no public API for it. Checked
against umbrelOS 2.0.0 (`packages/umbreld/source/modules/files/samba.ts` and
`files.ts`) on 28 October 2026.
