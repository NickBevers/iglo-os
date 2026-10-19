// GuestId is the Proxmox ID of a container or VM, like "101 container".
export function GuestId({ vmid, type = 'lxc' }: { vmid: number; type?: 'lxc' | 'qemu' }) {
    return (
        <span className="shrink-0 text-xs font-normal whitespace-nowrap text-muted-foreground font-stretch-normal">
            <span className="font-mono text-foreground/75">{vmid}</span> {type === 'lxc' ? 'container' : 'VM'}
        </span>
    )
}
