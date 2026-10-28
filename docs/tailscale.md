# Remote access

The installer puts Tailscale in the manager container. Connect it on the Tailscale page, with your Tailscale account or an auth key.

- **Open Iglo-OS from anywhere**: Tailscale Serve gives Iglo-OS an HTTPS address like `https://iglo.<tailnet>.ts.net`, with a trusted certificate. Only devices in your tailnet can open it. Turn on HTTPS certificates for your tailnet first (admin console, DNS page).
- **Share your home network**: Iglo-OS becomes a subnet router, so your devices can reach Jellyfin and the NAS at their normal address. Approve the route in the admin console after you turn it on.

Tailscale keeps the container's own DNS (`--accept-dns=false`), so the LAN names keep working.

## Apps on your tailnet (Tailscale Services)

Tailscale Services give Seerr and Jellyfin their own address on your tailnet, like `https://seerr.<tailnet>.ts.net`, with a trusted certificate.

A Service needs a host with a tag, so do this once in the [Tailscale admin console](https://login.tailscale.com/admin):

1. In **Access controls**, add to your policy file:

   ```json
   "tagOwners": {
     "tag:iglo": ["autogroup:admin"]
   },
   "autoApprovers": {
     "services": {
       "svc:seerr": ["tag:iglo"],
       "svc:jellyfin": ["tag:iglo"]
     }
   }
   ```

2. In **Services**, choose Advertise > Define a Service, and add `svc:seerr` and `svc:jellyfin`, each with the endpoint `tcp:443`.
3. On the Tailscale page of Iglo-OS, click **Use tag:iglo**, and log in again with the link on the page.
4. Turn on Seerr and Jellyfin under **Apps on your tailnet**.

After step 3, the manager belongs to `tag:iglo` instead of your account, and its key no longer expires. If your access controls only allow your own devices, also allow `tag:iglo`.

Tailscale forwards each Service to a small local proxy in the manager (`127.0.0.1:18081` for Seerr, `127.0.0.1:18082` for Jellyfin). These proxies send the request to the app, like the LAN names in [app-names.md](app-names.md).

## Self-hosted Headscale

The **Remote access** page can register the manager with your own HTTPS
Headscale controller. Headscale is BSD 3-Clause FOSS. The Linux Tailscale client
is open source; the hosted control service is optional. Check the license of
client applications on each platform separately.

1. Run a reachable Headscale server with a trusted HTTPS certificate. Keep it
   available independently of the Iglo manager. Use the upstream server and
   reverse-proxy guides linked below. Iglo does not install a public controller
   or configure a domain for you.
2. Keep a LAN connection to Iglo available. Disconnect its current remote
   session, choose **Headscale** under **Control server**, enter the HTTPS base
   URL and save. Provider changes are refused while connected or connecting.
3. Register interactively through the controller, or paste a one-time Headscale
   pre-authentication key. Keys are held in a temporary 0600 file for the CLI
   and removed when login ends. They are not saved in settings or CLI arguments.
4. Join your other devices to that same controller. Reach the manager's private
   IP over HTTPS, or configure your own DNS and TLS. The LAN certificate is not
   automatically trusted for the private IP. Do not disable TLS verification
   as a permanent setup.
5. For subnet access, save the home subnet and approve the advertised route on
   Headscale, or configure its policy auto-approvers. The form preserves your
   controller. Existing `tag:iglo` settings are cleared when changing providers;
   manage Headscale tags on that controller.

Iglo hides and refuses its managed Tailscale Serve, Services and tag setup in
Headscale mode. It does not promise `.ts.net` names or certificates. Peer status
and subnet routing remain available. Keeping `--accept-dns=false` preserves
LAN DNS on the manager; configure DNS on other devices through your controller.

For a fully self-hosted network, also review Headscale's DERP configuration.
Its default relay map may use Tailscale-operated relays. Headscale can run an
embedded DERP server or use your own relays. Configure reachability and remove
external relay sources deliberately, then test direct and relayed connections
from outside your LAN. An external identity provider is optional; interactive
administrator approval and pre-authentication keys can be self-hosted too.

Checked on 28 October 2026 against Tailscale **v1.104.4**
`cmd/tailscale/cli/up.go` and Headscale **v0.29.6** features, registration and
license. The installer uses the stable package repository, so installed versions
can differ. The dev CLI simulates both controllers with isolated state. It does
not test real NAT traversal, relay reachability or a deployed Headscale server.

Sources: [Headscale features](https://github.com/juanfont/headscale/blob/v0.29.6/docs/about/features.md),
[registration](https://headscale.net/stable/ref/registration/),
[DERP configuration](https://headscale.net/stable/ref/derp/),
[server configuration](https://headscale.net/stable/setup/install/official/),
[Tailscale CLI source](https://github.com/tailscale/tailscale/blob/v1.104.4/cmd/tailscale/cli/up.go),
[Headscale license](https://github.com/juanfont/headscale/blob/v0.29.6/LICENSE).
