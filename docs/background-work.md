# Background work

The Background work page controls optional Immich queues through its queue API and admission leases for the Music/Inbox workers. It keeps the API server and database running while deferring new analysis and video-conversion jobs. Connect an existing compatible Immich server or install [Photos](photos.md) from Apps. The independent Music/Inbox setting needs no Immich key; see [worker setup](workers.md) for native controls and the legacy FileFlows pre-execute script.

## Connect and enable

1. Open **Server → Background work**.
2. Enter the Immich server URL and an admin API key with queue read and update access. Both the server root and an `/api` base URL work. The manager must be able to reach it and trust its HTTPS certificate.
3. Select the Proxmox node that runs Immich. The memory checks read that node. On the node of the host agent, they use what the kernel can give to new work (`MemAvailable`, page cache included) and how long tasks waited for memory over the last 10 seconds (PSI, `/proc/pressure/memory`). New heavy work waits when either runs low or high: less free memory than the headroom, or more than 10% of the time spent waiting. The headroom is at least 4 GiB. When the agent doesn't answer, or Immich runs on another node, new heavy work waits: Proxmox's own figure counts page cache as used and knows nothing about pressure, so it is no stand-in. When Immich runs in a container, Iglo-OS finds it by the address in the URL, every 30 minutes and after the URL changes. It fills in an empty node, for example after a Photos install, and a server with one node needs no choice. It never changes a node you chose. When Immich runs on another node, the page says which one. For Immich outside Proxmox, like on a NAS, choose the node by hand.
4. Choose the heavy queues, host headroom and maximum queues accepting work. Enable control and save.

Control is disabled by default. Upload metadata, thumbnails, database backups and other essential queues are outside the allowlist. Existing operator-paused queues stay paused; Iglo only resumes pauses it owns. While enabled, use Iglo for the selected queues: an operator pause on a queue already paused by Iglo cannot be distinguished from Iglo's existing pause.

The queue API (`GET /api/queues`, `PUT /api/queues/{name}` with `isPaused`) is available in newer Immich versions, introduced in the v2.4 API. Older servers without it display an error and receive no guessed legacy commands. Validate the integration against the version you deploy. Sources: [queue controller](https://github.com/immich-app/immich/blob/main/server/src/controllers/queue.controller.ts), [queue schema](https://github.com/immich-app/immich/blob/main/server/src/dtos/queue.dto.ts).

## Admission policy

Every 15 seconds while work is admitted, once a minute while nothing may run, and right after settings changes or gaming protection, the controller:

- Defers selected queues during timed gaming protection or outside the configured daily processing window.
- Checks host headroom from the host agent, never less than 4 GiB. Unknown host memory defers new heavy work. After low headroom, another 512 MiB above the configured threshold is required to resume, reducing repeated pause/resume cycles.
- Limits how many selected queues accept new work. Queues with active work count toward the budget; excess existing queues are paused until enough jobs finish. Selection follows the configured queue order, so a continuously busy earlier queue can delay later ones.
- Pauses before resuming other queues. New jobs remain queued; the controller never deletes work or force-stops active jobs.

This limits **queues accepting work**, not individual worker processes. Set each queue's worker concurrency in Immich itself. Already-running tasks may continue consuming CPU, RAM or GPU until they finish. Proxmox memory accounting is a conservative admission signal, not Linux `MemAvailable`, a RAM reservation or an out-of-memory guarantee.

The daily window uses its explicit IANA timezone, supports crossing midnight and follows local daylight-saving changes. Its start is inclusive and its end exclusive. Turn the window off for all-day processing.

**Protect gaming for 2 hours** creates an expiring hold. **End protection** removes the hold; memory and time-window checks still apply. **Play** on the [Gaming page](gaming.md#the-gaming-page) sets an 8-hour hold, and **Stop** ends it. A running Moonlight session keeps a hold at least 15 minutes ahead by itself, also without Play.

## Recovery and secrets

`workloads.json` in the manager data directory contains the settings, secret key, gaming deadline and pause ownership. Writes are atomic and the file has mode `0600`. Pause intent is saved before sending a pause so a restart can recover it. The API never returns the key, and does not expose upstream response bodies. Credentialed redirects are rejected.

Disable control and wait for owned queues to resume before changing the server URL. The controller retains ownership on connection failures and retries. It cannot resume queues while the manager or Immich is offline; the original queues may stay paused until control reconnects. Do not delete its state file to disconnect it. If the manager is permanently lost, resume the affected queues in Immich's own administration interface.

The encrypted manager data backup includes this file. The `iglo.yaml` [settings file](settings-file.md) has the settings, without the API key, the gaming deadline or pause ownership. Restoring a manager backup also restores its endpoint, credentials and recorded queue ownership; verify the destination before starting a restored manager against a different installation.

## Other optimization paths

| Workload | Method | Current Iglo control |
| --- | --- | --- |
| Immich analysis and video conversion | Selected queue admission, low concurrency, processing windows; test Intel OpenVINO/Quick Sync where supported | Queue admission implemented here. With a host GPU, machine learning uses OpenVINO; Quick Sync for video is one switch in Immich. |
| Jellyfin playback | Prefer direct play; Intel Quick Sync for required transcodes | Existing Jellyfin integration; this controller does not change playback settings. |
| Inbox | One native FFmpeg worker, manual pause and a short admission lease | Review the profile and resume in Inbox work. Legacy FileFlows needs its pre-execute script until explicitly migrated. |
| Emulation and streaming | Test native graphics backend, resolution and hardware stream encoding; defer background work | Timed protection from Play, and a hold while Wolf reports a Moonlight session; no GPU reservation. |
| Music imports | One native worker, local staging, short admission lease and manual pause | Music installer and Music work page; independent worker control follows gaming/window/headroom policy. |
| Backups | Separate ordinary backups from expensive verification/pruning and conversion windows | Existing backup scheduler; this controller does not pause backups. |
| LXC resource controls | CPU weights/quotas and measured memory limits | Native Proxmox configuration; no automatic live memory shrinking. |

WebGPU exposes GPU compute and rendering to web applications. It does not automatically accelerate these native server apps or provide a server GPU scheduler. Native app acceleration is the relevant path; different Intel GPU engines can work concurrently, but still share bandwidth, memory and power. More acceleration is not automatically better when gaming shares the same device.

References: [WebGPU](https://developer.mozilla.org/en-US/docs/Web/API/WebGPU_API), [Jellyfin Intel acceleration](https://jellyfin.org/docs/general/post-install/transcoding/hardware-acceleration/intel/), [Immich ML acceleration](https://docs.immich.app/features/ml-hardware-acceleration/), [Immich worker separation](https://docs.immich.app/administration/jobs-workers/), [FileFlows agents and runners](https://fileflows.com/docs/webconsole/agents/), [FileFlows API](https://upload.fileflows.com/docs/api/).
