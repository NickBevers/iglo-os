---
name: iglo-live-and-cost
description: Add a background loop, a poll, a relay of an upstream stream, or a live update topic to Iglo-OS without making the server slower. Use when a change in Iglo-OS checks something on a timer, calls pct or Proxmox repeatedly, or should show up on a page without a reload.
---

# Loops, polls and live updates

The manager runs on one core in 512 MiB, and every `pct` call starts a Perl
program on the host. A performance sweep found most of the waste in loops and
polls that nobody counted. Count first, then build.

## 1. Count the cost

For each loop or poll, write down per minute:

- Proxmox API calls (shared for 2 seconds by `proxmox.Client.Resources`),
- `pct` processes on the host,
- calls to an app (Jellyfin, Immich, a worker),
- file writes in the data folder,

and say whether it runs while nothing happens, only while a page is open, or
only while something runs. An open page multiplies by its tabs.

## 2. Make it cheap

- **Prefer a stream to a poll.** If the upstream pushes (Wolf `/api/v1/events`,
  Jellyfin's `/socket`, an SSE endpoint of our own worker), follow it. In the
  agent, relay it with `relayGuestStream`: one long-lived `pct exec curl -N`,
  capped by `guestStreamLimit` so a stray curl can't outlive it, passing only
  the event names that matter. Never pass event data that may hold secrets.
- **In the manager, follow a container's stream with `follower`**
  (`internal/apps/follow.go`): it opens the stream while the container runs,
  reconnects after a drop, and its `events` channel wakes the owner. Use a
  long fallback interval while `live` is true and a short one when it isn't.
- **Poll fast only while something runs**, and slow down while the live stream
  is up (`isLive()` in `web/src/lib/live.ts`).
- **Cache what rarely changes** with a TTL and forget it when an operation
  changes it. The agent's `containerConfig` keeps `pct config` for 30 seconds.
- **Write files only on change**, and do independent calls in parallel.
- **Wake the watcher when the user acts**, like `gamingWake` after Play, then
  check quickly for a short while, because a container takes seconds to start.

## 3. Tell the open pages

An event names a topic; the page asks for the data the normal way.

1. Add the topic to `internal/events/hub.go`.
2. Publish it where the state changes: the service's `Changed` option, called
   only when what a page shows changed (compare a fingerprint that leaves out
   values that change every check, like free memory or the check time).
3. Map it to its query keys in `topicKeys` in `web/src/lib/live.ts`.
4. Make the query poll slower while `isLive()`.
5. Fake it in `dev/mock` (`streamChanges` turns a fingerprint into events).

## 4. Prove it

- Unit test the parsing, the filter (no secrets pass), the follower and the
  publish conditions.
- In a stream test, take the first snapshot before the stream opens, then
  write; the other order raced on CI.
- Time it end to end with the `iglo-dev-run` skill, and say that the mock
  checks once a second where the real system pushes.
