# Development

You need Go and Node.js 22.

## Run it on your computer

```sh
make dev        # fake Proxmox, fake host agent, API and Vite
make dev-code   # the current login code, in a second terminal
```

Open http://localhost:5173 and log in with `admin` / `iglo-dev-password` and the code from `make dev-code`.

- The fake Proxmox has one node, the manager, Jellyfin, a Docker container and a VM, with disks, storage, backups and tasks.
- Background work can connect to the fake Immich queues at `http://127.0.0.1:18283`, with the sample key `iglo-dev-immich` and node `pve`. This tests the controls, not real Immich performance. The fixture resets its queue state when restarted.
- The fake host agent answers update checks, logs and backup jobs. App installs use the real installer of the agent, and only play the install script: it takes a few seconds. The password `failfailfail12` makes an install fail.
- The fake Proxmox has a Jellyfin container. To try the Jellyfin install, run `touch .dev/no-jellyfin` and reload the Apps page.
- Jellyfin and the media stack use a Videos share from the Storage page, like the other apps. Connect one there first; the fake host only simulates the mount.
- A fake `tailscale` CLI (`dev/bin/tailscale`) logs in 5 seconds after you click Connect.
- The console opens a shell on your own computer, not in a container.
- While the fake gaming container runs, a PC plays on Moonlight. To let a TV join and see the manager stop its session, run `touch .dev/gaming-second-session`.
- The fake host agent sends the same events as the real one, so live updates work in dev mode. It looks for changes once a second, where the real agent relays the events of Wolf and the Music worker as they happen.
- Everything you change stays in `.dev/`. Delete that folder to start again.
- A fake Jellyfin on `http://127.0.0.1:18096` has a library, two things playing and recent items. The manager is connected to it on the first start. Images are not faked.
- Music, Photos, Inbox and Gaming install like the other apps: the real installer of the agent plays a simulated script. After that, the fake host has their container with sample stats, and their cards show **Sample data**.

## A demo for a review

The demo build runs the web app without a server. Every answer of the API comes from sample data in `web/src/demo/fixtures.json`. Buttons that change something show a message instead, and a banner says it is a demo.

```sh
cd web
npm run build:demo    # the demo in web/dist
```

`web/vercel.json` builds the demo on Vercel. The Vercel project has `web` as its root directory, so the CLI is linked from the repo root. Deploy from there:

```sh
vercel pull --yes --environment=production
vercel build --prod
vercel deploy --prebuilt --prod
```

This builds on your machine and only uploads the result. `vercel deploy --prod` builds on Vercel instead. Only a push to `main` starts a build on Vercel, and only when something in `web/` changed. Other branches get no preview deploy. The normal build leaves the demo code and the sample data out.

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

GitHub runs the same checks on every pull request and on main, plus race checks, `gofmt` and Prettier. Build the web app first with `make web`, because the Go tests embed it.

## Test the real install

The installers only run on a Proxmox host. Test them in a nested Proxmox VM on your server:

1. Create a VM with CPU type **host**, 4 cores, 8 GB RAM, a 64 GB disk and the network on `vmbr0`.
2. Install Proxmox VE 9 from its ISO.
3. Build a bundle with `make bundle`, copy it to the VM, and run `./install.sh` as root.

## Deploy to your server

`make deploy` puts the tagged commit on your Proxmox host. It runs on your own
machine, and needs SSH access as root to the host (at home, or over
Tailscale).

One time:

1. Install [age](https://github.com/FiloSottile/age) (BSD-3-Clause) with your
   package manager.
2. Make a signing key. The private key goes straight into a file encrypted
   with a passphrase, and never touches the disk unencrypted. Commit the public
   key.

   ```sh
   mkdir -p ~/.config/iglo
   go run ./cmd/iglo-release keygen -public internal/release/signing.pub | age -p -o ~/.config/iglo/signing-key.age
   ```

3. Keep a copy of `signing-key.age` and its passphrase somewhere safe, like
   your password manager. Without them, the agent installs no new version, and
   you install the next one by hand once.

   To keep the key in KeePassXC instead, set `IGLO_SIGNING_KEY_CMD` to a
   command that prints it, like
   `keepassxc-cli show -s -a Password ~/vault.kdbx iglo-signing-key`.

Each time:

```sh
git tag -a v0.2.0 -m v0.2.0
make deploy HOST=root@192.168.1.10
git push origin v0.2.0
```

`make deploy`:

1. Refuses changes that aren't committed, and a commit without a version tag.
2. Runs every check of [AGENTS.md](../AGENTS.md): `make web`, `make test`,
   the race tests, `gofmt`, Prettier, `bash -n` and ShellCheck.
3. Builds the bundle and signs it with your key, which only that one command
   sees.
4. Copies the bundle to `/root/iglo-deploy` on the host. Then
   `iglo-agent upgrade` hands it to the agent over its root-only socket,
   `/run/iglo-agent/admin.sock`, and prints the install log. The agent checks
   the signature and that the version is newer, and rolls back when the new
   version doesn't work. No container can reach that socket.

On a host without Iglo-OS, the same command runs the first install, which
asks for the admin account. The bundle comes from your own machine over SSH,
so it needs no download from anywhere.

## New app image versions

The app stacks pin every image to a digest, and a new Iglo-OS version brings
the new pins. To move an app to its newest images, run
`go run ./dev/pinimages deploy/stacks/arr/compose.yaml`. It pins each tag to
the digest it points to now, with the version from the image's label. Test
the stack with them on a server before you deploy; `dev/pinimages` refuses
nothing, the test is the check. Every image in the stacks has a digest, and a
test fails on one without. After a deploy, press **Update** on the Apps page
to move an installed app to the new pins.


## Notes

The manager publishes Health, Updates and Backups topics while using the dev
mock too. Add or refresh a health check, run an update check, or start a backup
restore check to exercise them through `/api/events`. These topics come from
the real manager services; the mock supplies their normal API dependencies.

The shared shadcn stylesheet is kept in `web/src/styles/shadcn.css`, from
shadcn 4.21.0, with its MIT license alongside it. The app needs this CSS,
not the shadcn CLI and its dependency tree. Refresh the stylesheet deliberately
when updating the shared component styles.
