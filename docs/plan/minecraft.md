# Minecraft

Minecraft comes later. We plan for it now so the CPU and the memory budget leave room for it.

Iglo-OS can already install a Paper server. See [minecraft.md](../minecraft.md). It gives the container 4 cores that it shares with all other containers.

## One P-core of its own

The main loop of a Minecraft server (the "tick") runs on one thread. A fast core that nothing else uses keeps it smooth. Paper also uses other threads, for garbage collection, chunk loading and the network.

The plan:

- Minecraft gets **one P-core with both its threads**, for example logical CPUs 14 and 15. No other container may use them.
- Minecraft can also use the other cores, shared with the rest. Its side threads then don't wait.
- Check the CPU numbers on the mini PC with `lscpu -e`. See [hardware.md](hardware.md#cpu).

## How Proxmox does it

Proxmox pins a container to CPUs with a line in `/etc/pve/lxc/<id>.conf`:

```
lxc.cgroup2.cpuset.cpus: 14-15
```

"Of its own" means that every other container gets a line too, without those CPUs, like `0-13,16-19`. Otherwise they still run on them.

The Proxmox API does not allow `lxc.*` lines. Only root on the host may write them. So the host agent of Iglo-OS writes them, like it does other root work today.

## In Iglo-OS

- The Minecraft form gets a "Dedicated core" switch.
- The agent writes the `cpuset` line for Minecraft, and the opposite line for all other containers. A new container gets the opposite line too.
- The Health page or the Minecraft app shows which core Minecraft has.
- Remove gives the core back to the other containers.

## Memory

The current form offers 2 to 8 GB. 4 GB is enough for about 10 players. With 32 GB in the mini PC that fits. See [hardware.md](hardware.md#memory).
