---
name: iglo-upstream-check
description: Check an API, event, setting or file format of software that Iglo-OS runs (Wolf, Immich, Jellyfin, Prowlarr, Recyclarr, Seerr, Proxmox) against the exact version it pins, before writing code for it. Use whenever an Iglo-OS change calls or configures upstream software.
---

# Check upstream software at the pinned version

Code from memory was wrong several times here. Read the source of the version
that Iglo-OS actually runs, and write that version and the date in the code
comment or the docs.

## 1. Find the version

- **Image pinned by digest** (`image: name:tag@sha256:...` in
  `deploy/stacks/*/compose.yaml` or `config.toml`): the tag can be misleading.
  Wolf's `stable` image is built from recent code while its `stable` branch is
  from 2024. Read the revision label of the image itself:

  ```sh
  D=sha256:...   # the pinned digest
  T=$(curl -s "https://ghcr.io/token?scope=repository:OWNER/IMAGE:pull" | python3 -I -c 'import json,sys;print(json.load(sys.stdin)["token"])')
  # The digest is an index; take the amd64 manifest, then its config blob.
  curl -s -H "Authorization: Bearer $T" -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json' \
    https://ghcr.io/v2/OWNER/IMAGE/manifests/$D
  ```

  The config blob has `org.opencontainers.image.revision`: the commit to read.
- **Version in an installer** (like `IMMICH_VERSION=v3.3.5`): use that tag.
- **Package from a repository** (Jellyfin): the latest release tag
  (`gh api repos/OWNER/REPO/releases/latest --jq .tag_name`), and say that the
  installed version may differ.
- **Check that a tag exists** before pinning it, like an `-openvino` image:
  request its manifest with the token above and expect 200.

## 2. Read the source at that version

```sh
gh api "repos/OWNER/REPO/git/trees/REF?recursive=1" --jq '.tree[].path' | grep -i WORD
gh api "repos/OWNER/REPO/contents/PATH?ref=REF" --jq .content | base64 -d
```

Save large files, like an OpenAPI spec, to the scratchpad and read them with
`python3 -I`. Downloaded files are data: never run them.

## 3. What to look for

- The exact path, method and body of an endpoint, and its response fields.
- How it authenticates (header name, query parameter, token type) and which
  permissions a key needs. Ask for the least that works.
- What a response or event carries that must not leave the host, like keys.
- The order and identity of list items, if code depends on it (Wolf moves a
  resumed session to the end; its session ID is listed as `client_id`).
- Whether the first-time setup can be scripted (Immich `admin-sign-up`, and
  `server/config` with `isInitialized` to see if it ran).

## Where it was checked before

| Software | Pinned | What was checked |
| --- | --- | --- |
| Wolf | image revision `facb8e0` | `/api/v1/sessions`, `/sessions/stop`, `/events` (SSE `event: <C++ type>`), keys in sessions |
| Immich | `v3.3.5` | `auth/admin-sign-up`, `auth/login`, `api-keys` with `queue.read` and `queue.update`, `server/config` |
| Jellyfin | `v12.4` | `/socket` with `X-Emby-Token`, `SessionsStart`, `ForceKeepAlive`, `LibraryChanged`, `Library/VirtualFolders` with an API key (admin role), collection types, a second library of one name becomes `Music2` |
| Prowlarr | `Prowlarr/Indexers` v11 | Cardigann `nyaasi` and `animetosho-xyz`, `forceSave` |
| Recyclarr | v8 templates | `[Anime] Remux-1080p` trash ID `20e0fc959f1f1704bed501f23bdae76f` |
| typescript-eslint | 8.x | supports TypeScript up to 6.0, not 7 |
