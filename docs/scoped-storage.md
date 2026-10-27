# Scoped SMB bindings

Use **Server → Storage** to connect a dedicated SMB share with a folder role.
Use a separate NAS account restricted to that share. Binding IDs are immutable.
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
including stopped containers. It removes only the mount and host credential
files, never NAS contents. Change credentials by removing an unused binding and
reconnecting it. Every installer, Jellyfin and the media stack included, uses
`deploy/storage.sh`.

The local dev fixture labels its mounts as simulated and does not contact a NAS.
Unit tests exercise validation, secret redaction, consumer protection and mount
failure. Real mount and guest boot behavior must be verified on Proxmox before
entrusting production data to it.
