# Your own Headscale controller

Install **Headscale** from Apps to run the private network controller on the
Mini PC. It uses native Headscale and Caddy services in a separate unprivileged
Proxmox container. SQLite, device identities and certificates stay on local SSD.
It has no Docker daemon and no NAS mounts. The existing remote connection stays
as it is until you explicitly change it on Remote access.

## Resource budget

The container has one CPU core, a fixed **512 MiB** RAM cap, no swap and an
**8 GiB** local disk. Headscale is capped at 320 MiB and Caddy at 128 MiB, leaving
about 64 MiB within the guest cap for other processes. Go memory targets are
256 MiB and 96 MiB. These are limits, not measured hardware usage or reservations.

The default apps plus manager have a 24.5 GiB peak cap including Gaming. Adding
this controller makes **25 GiB**, leaving 7 GiB against 32 GiB before firmware
and GPU reservations. Installation counts **every existing guest cap on the
node, including stopped guests**, adds 512 MiB and 4 GiB of host headroom, and
compares that with measured host memory, capped at 32 GiB. It also checks current
MemAvailable. The host script repeats the guard just before creating the guest.
A stopped VM still counts. Iglo refuses an unknown or excessive budget and does
not reduce other guest limits automatically.

The network defaults to a **5 MiB/s** Proxmox interface cap, adjustable between
1 and 25 MiB/s during installation. This includes relay traffic. Direct device
connections do not send their traffic through the controller. Watch guest CPU,
RAM and network charts in Proxmox and Iglo before increasing the cap. Relayed
video can consume much more upload bandwidth than registration traffic.

## Before installation

- Use a public DNS hostname with an A record to your public IPv4 address.
  Leave its AAAA record unset for this IPv4 guest setup.
- Pick an unused static address outside the router's DHCP pool. Its subnet and
  prefix must match `vmbr0`. Iglo rejects the host's own address and checks for
  duplicates with `arping` when available. You must still verify the address
  is free, including any stopped device or reserved DHCP lease.
- Forward TCP **80 and 443**, and UDP **3478**, to that guest address. Allow them
  through the Proxmox firewall if enabled. Port 80 handles redirects and ACME;
  443 serves HTTPS and relays; 3478 provides STUN for NAT traversal.
- Your ISP connection must accept incoming traffic. Ordinary port forwarding
  does not work behind carrier-grade NAT. A domain alone does not fix that.
- LAN devices need NAT loopback on the router or a local DNS override resolving
  this hostname to the guest's LAN IP.

The installer does not change your DNS, router, current VPN or host firewall.
The guest firewall allows only its public HTTP/HTTPS/STUN ports, established
connections, loopback and ICMP. Admin HTTP routes, metrics and gRPC listen only
on loopback, and Caddy blocks admin routes. The unauthenticated admin Unix
socket is 0600. No API key is generated or shown in the browser.

Caddy obtains certificates from **Let's Encrypt**, explicitly selected as the
issuer. Certificate issuance may remain pending after local installation passes.
Do not repeatedly remove/reinstall to retry issuance; that can hit CA limits.

The controller is on the same host as the manager. If the Mini PC or home
connection goes down, registration and this relay go down too. Existing direct
connections may keep working for a while, but do not rely on that for recovery.
Keep a LAN recovery path. A separate VLAN/DMZ can provide further isolation for
this internet-facing guest; configure its routes and firewall deliberately.

## Register devices

The installer creates an `iglo` Headscale user. In the guest console, run:

```sh
headscale users list
```

Note that user's numeric ID. On Remote access, disconnect the current session,
save `https://YOUR_HOSTNAME` as the Headscale control URL, then register. Open
the returned registration page and follow its approval command from the guest
console. At Headscale 0.29.6 the command is:

```sh
headscale auth register --auth-id AUTH_ID_FROM_LOGIN_PAGE --user USER_ID
```

A one-time key is another option. Create it in the guest console:

```sh
headscale preauthkeys create --user USER_ID --expiration 10m
```

Paste that key only into the login form for the intended device. Iglo uses it
for the CLI login and removes its temporary file afterwards. Do not put it in
shell history, a saved plan, issue, screenshot or log. Keys and approval actions
are not generated through an Iglo browser API. Join the other devices to this
same controller. Existing devices on hosted Tailscale do not migrate by themselves.

The initial policy permits communication between registered devices on this
private network. Edit `/etc/headscale/policy.hujson` in the guest to restrict it.
Updates preserve this policy. MagicDNS uses `nodes.YOUR_HOSTNAME`, a separate
namespace from the controller. Client LAN DNS is retained. The manager itself
keeps `--accept-dns=false` as before.

The embedded DERP server verifies that clients belong to this controller. Its
relay map has no external URLs and no periodic external map fetch. It does not
fall back to Tailscale-operated relays. Disabling this relay without supplying
another valid map is unsupported by the pinned Headscale release.

## Check operation

The install/update log distinguishes local health and certificate issuance while
the operation is visible. For current status afterwards, use the guest console:
`systemctl status headscale caddy` and `journalctl -u caddy`.
Iglo does not perform a persistent or outside-network reachability poll.

1. Check the log for passed local configuration/health checks.
2. Check whether the certificate was obtained or remains pending.
3. From mobile data, open `https://YOUR_HOSTNAME/health` without bypassing TLS
   validation. Test client registration and `tailscale netcheck` outside the LAN.
4. Confirm two registered devices connect both directly and through your relay
   where NAT requires it. Test again after a router and host reboot.

An active container alone is not proof that public DNS, TLS or NAT forwarding
works. Managed Tailscale Serve, Services and `.ts.net` certificates remain
unavailable in Headscale mode. Use private IPs or your own DNS and TLS for apps.

## Updates, backups and removal

Updates download and checksum-check binaries before stopping the controller.
Only reviewed source versions may open the database. This first installer
supports refreshing **0.29.6**; future version changes need an explicit reviewed
upgrade path. Unsupported changes and downgrades are refused before package work.

An update stops both services, keeps a private recovery copy of database,
identities, certificates, configuration and binaries, uses SQLite's backup API,
and runs integrity/configuration checks. It then starts the pinned software and
checks local health. A failure attempts restoration and restart of the old state.
The manager also takes a Proxmox snapshot when supported. It uses that snapshot
only when local recovery cannot be verified. Early refusals and a verified local
recovery do not rewind the database again. A rollback can lose registrations or revocations made after its backup.
The last three successful update recovery points stay under
`/var/backups/iglo-headscale`. Journald is bounded to 64 MiB on disk.

Generated Headscale/Caddy configs, units and the guest firewall are owned by the
installer and replaced on update. The separate policy file stays user-owned.
Device keys and certificate storage are created only if missing. Container tags
and networking are preserved on updates.

Include the guest in a Proxmox backup job and keep an encrypted off-device copy.
Local recovery points are not off-device backups. **Backups contain private
keys and device identities.** Protect their access and restore them deliberately.

Removal requires typing the installed public hostname. The manager, agent and
host script verify it. Removal destroys the guest database, identities and
certificates; devices need to join again unless a backup restores the identities.
If the manager uses this controller, its remote access is affected too. Existing
Proxmox backups survive removal. Keep LAN access available before proceeding.

## Versions and verification

Checked on **28 October 2026** against Headscale **v0.29.6** and Caddy **v2.11.7**.
Both release binaries have fixed SHA256 checksums. Headscale uses BSD 3-Clause;
Caddy uses Apache 2.0. The first slice supports static IPv4 on `vmbr0` and one
private network. It stays outside the guided media setup.

`bash dev/check-headscale.sh` runs real pinned binaries in an isolated Debian
container limited to 512 MiB and one CPU. It checks config loading, local HTTPS
proxying, blocked private routes, user creation, SQLite backup and the guest
firewall. It uses a throwaway local CA for the test and never requests a public
certificate. Go tests cover answer validation, budget and bridge guards,
confirmed removal, packaging and restoration of old identities after a failure.
The dev mock simulates installation and removal. These checks do not prove
Proxmox installation, public ACME/NAT reachability, relay throughput or recovery
on the actual Mini PC. Those remain hardware acceptance checks.

Sources: [Headscale configuration](https://github.com/juanfont/headscale/blob/v0.29.6/config-example.yaml),
[router](https://github.com/juanfont/headscale/blob/v0.29.6/hscontrol/app.go),
[reverse proxy](https://github.com/juanfont/headscale/blob/v0.29.6/docs/ref/integration/reverse-proxy.md),
[DERP](https://github.com/juanfont/headscale/blob/v0.29.6/docs/ref/derp.md),
[Caddy ACME issuer](https://github.com/caddyserver/caddy/blob/v2.11.7/modules/caddytls/acmeissuer.go).
