# Development

You need Go and Node.js 22.

## Run it on your computer

```sh
make dev        # fake Proxmox, fake host agent, API and Vite
make dev-code   # the current login code, in a second terminal
```

Open http://localhost:5173 and log in with `admin` / `iglo-dev-password` and the code from `make dev-code`.

- The fake Proxmox has one node, the manager, Jellyfin, a Docker container and a VM, with disks, storage, backups and tasks.
- The fake host agent answers update checks, logs and backup jobs. App installs use the real installer of the agent, and only play the install script: it takes a few seconds. The password `failfailfail12` makes an install fail.
- The fake Proxmox has a Jellyfin container. To try the Jellyfin install, run `touch .dev/no-jellyfin` and reload the Apps page.
- The fake host agent has a NAS share mounted for the apps. To choose the media folder in the install forms, run `touch .dev/no-media-mount`. A working install mounts its answer again.
- A fake `tailscale` CLI (`dev/bin/tailscale`) logs in 5 seconds after you click Connect.
- The console opens a shell on your own computer, not in a container.
- Everything you change stays in `.dev/`. Delete that folder to start again.
- A fake Jellyfin on `http://127.0.0.1:18096` has a library, two things playing and recent items. The manager is connected to it on the first start. Images are not faked.
- The self-update check needs the real GitHub.
- The apps that Iglo-OS can't install yet (Music, Photos and Inbox) have fake containers with fake stats, so the dashboard shows them. To hide them, run `touch .dev/no-planned-apps`.

## A demo for a review

The demo build runs the web app without a server. Every answer of the API comes from sample data in `web/src/demo/fixtures.json`. Buttons that change something show a message instead, and a banner says it is a demo.

```sh
cd web
npm run build:demo    # the demo in web/dist
```

`web/vercel.json` builds the demo on Vercel: run `vercel deploy --prod` in the `web` folder. The normal build leaves the demo code and the sample data out.

To record the sample data again, run `node scripts/record-demo.mjs` in the `web` folder while `make dev` runs. It only takes data from the dev mock.

## Against a real Proxmox

The API only reads from Proxmox, except for the actions you click. Use a token with the role from [architecture.md](architecture.md).

```sh
IGLO_DATA_DIR=.data go run ./cmd/iglo admin   # once: create an admin

export PROXMOX_URL=https://<proxmox-ip>:8006
export PROXMOX_TOKEN_ID='iglo@pve!manager'
export PROXMOX_TOKEN_SECRET=<secret>
make dev-api    # terminal 1: API on 127.0.0.1:8080
make dev-web    # terminal 2: Vite, proxies /api to the API
```

Without a host agent, the pages that need it show "the host agent did not answer".

## Tests

```sh
make test       # Go vet and tests, ESLint and the TypeScript check
```

## Test the real install

The installers only run on a Proxmox host. Test them in a nested Proxmox VM on your server:

1. Create a VM with CPU type **host**, 4 cores, 8 GB RAM, a 64 GB disk and the network on `vmbr0`.
2. Install Proxmox VE 9 from its ISO.
3. Build a bundle with `make bundle`, copy it to the VM, and run `./install.sh` as root.

## Making a release

1. One time: create the signing key. The private key goes straight into a GitHub secret. Commit the public key.

   ```sh
   go run ./cmd/iglo-release keygen -public internal/release/signing.pub | gh secret set IGLO_SIGNING_KEY
   ```

2. Set the new version in the install command of `README.md`, and merge that to `main`.
3. Tag the version and push the tag. GitHub Actions builds, tests, signs and publishes the release.

   ```sh
   git tag -a v0.6.0 -m v0.6.0 && git push origin v0.6.0
   ```

A build without the key in `signing.pub` can't install updates. If you lose the private key, make a new one and install the next version by hand once.
