# External GPU (eGPU)

The question: does an external GPU, like an Intel Arc A380 in an enclosure over Thunderbolt, help this plan?

**Answer: not now.** The UHD 770 in the CPU does what this plan needs. An eGPU costs money and power every hour, and on Proxmox it is fragile. Measure first, and buy only when the iGPU turns out too slow.

Researched on 19 October 2026. Sources are at the bottom.

## What an A380 would add

The A380 has more video engines than the UHD 770, and it can encode AV1. The UHD 770 can't.

Here is what that means for each job in the plan:

| Job | Does the UHD 770 do it? | Does the A380 help? |
| --- | --- | --- |
| Live transcoding in Jellyfin | Yes. Most files play without a transcode on the TV box. A transcode happens for AV1 on the Raspberry Pi 5, for burned-in subtitles, or on a slow connection away from home. | Only when several 4K HDR streams need tone mapping at the same time. Reports say the iGPU manages about 2 to 4 of those. One household rarely needs more. |
| Converting your own media (FileFlows) | Yes. It is a batch job, so it does not have to be fast. | Faster, but nobody waits for it. |
| AV1 encoding | No. | Yes. But AV1 is a bad target here: a Raspberry Pi 5 can't decode AV1, so every AV1 file would need a transcode. |
| Immich machine learning | Yes, through OpenVINO. Immich calls this experimental. | Faster. That matters mostly for the first import of a big library, which happens once. |

## What it costs

- **Parts:** the card, a Thunderbolt enclosure, and a power supply. Many enclosures have one built in.
- **Power:** an Arc card draws a lot at idle unless ASPM works. ASPM is a power saving mode of PCIe. Intel says ASPM L1 must be on for the card to save power. ASRock lists about 15 W idle with power saving, and less with no monitor. Homelab users report 20 to 30 W extra at idle. Over a Thunderbolt enclosure, it is unclear whether ASPM works at all.
- **Running cost:** 20 W extra, all day, is about 175 kWh a year. At about €0.30 per kWh, that is about €50 a year. This is our own estimate.

## Risks on Proxmox

- **Thunderbolt on Proxmox breaks with kernel updates.** A Proxmox forum thread shows eGPUs working on kernel 6.8.4 to 6.8.7 and broken from 6.8.8 on. A boot option (`thunderbolt.host_reset=false`) fixed it for some users and not for others.
- **Plugging it in while the host runs can hang the host.** One user's whole host froze, and only a restart helped.
- **Iglo-OS updates the host.** Host updates include new kernels, so every kernel update can break the eGPU again.
- **Not every mini PC has Thunderbolt.** A USB-C port without Thunderbolt or USB4 can't carry a GPU. Check the spec sheet of the exact model.

One thing is simpler than the forum threads suggest. Iglo-OS uses containers, not VMs. The host loads the driver and gives each container the `/dev/dri` device, so the VM passthrough problems of the A380 do not apply.

## When to look at this again

Measure on the real mini PC first:

1. Watch the GPU with `intel_gpu_top` on the host while Jellyfin transcodes.
2. Look at the transcode speed in the Jellyfin dashboard. Below 1× means the stream stutters.
3. Watch how long the first Immich import takes.

Look at an eGPU again when:

- the video engine stays at 100% and streams stutter in normal use;
- you want AV1 files, for example because the playback devices all decode AV1;
- you move to a PC with a real PCIe slot. Then a card inside the case avoids the Thunderbolt risks.

## Sources

- [Jellyfin forum: transcodes on a UHD 770 with tone mapping](https://forum.jellyfin.org/printthread.php?tid=10630)
- [ComputingForGeeks: mini PCs for Plex and Jellyfin](https://computingforgeeks.com/best-mini-pc-plex-jellyfin/)
- [Jellyfin forum: A380 power consumption](https://forum.jellyfin.org/t-a380-power-consumption?pid=35987)
- [ComputerBase: Intel on the idle power of Arc and ASPM](https://www.computerbase.de/2022-10/intel-arc-a770-750-380-verbrauch-leerlauf/)
- [ASRock FAQ: idle power of the A380](https://www.asrock.com/support/faq.de.asp?id=528)
- [Proxmox forum: improving power efficiency and ASPM](https://forum.proxmox.com/threads/improving-power-efficiency-and-configuring-aspm.133493/)
- [Proxmox forum: Thunderbolt eGPU regression in kernel 6.8.8](https://forum.proxmox.com/threads/regression-in-thunderbolt-connected-egpu-functionality-between-6-8-4-2-pve-and-6-8-12-4-pve.157303/post-719806)
- [Proxmox forum: eGPU over Thunderbolt on a Minisforum UM790 Pro](https://forum.proxmox.com/posts/854234/)
- [Proxmox forum: A380 PCIe passthrough](https://forum.proxmox.com/threads/issue-with-intel-arc-a380-pcie-passthrough.133290/latest)
- [Evetech: A380 as an eGPU over Thunderbolt](https://evezone.evetech.co.za/deep-dives/arc-a380-egpu-thunderbolt-laptops)
- [Immich: machine learning hardware acceleration](https://docs.immich.app/features/ml-hardware-acceleration)
