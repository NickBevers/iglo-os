import {
    ArchiveIcon,
    ArrowUpCircleIcon,
    FilmIcon,
    HeartPulseIcon,
    LayoutDashboardIcon,
    BlocksIcon,
    NetworkIcon,
    ScrollTextIcon,
    SlidersHorizontalIcon,
    HardDriveIcon,
    MusicIcon,
    type LucideIcon,
} from 'lucide-react'

export type NavItem = { to: string; label: string; icon: LucideIcon }

export const navigation: { label: string; items: NavItem[] }[] = [
    {
        label: 'Server',
        items: [
            { to: '/', label: 'Overview', icon: LayoutDashboardIcon },
            { to: '/health', label: 'Health', icon: HeartPulseIcon },
            { to: '/workloads', label: 'Background work', icon: SlidersHorizontalIcon },
            { to: '/storage', label: 'Storage', icon: HardDriveIcon },
            { to: '/updates', label: 'Updates', icon: ArrowUpCircleIcon },
            { to: '/backups', label: 'Backups', icon: ArchiveIcon },
            { to: '/logs', label: 'Logs', icon: ScrollTextIcon },
        ],
    },
    {
        label: 'Services',
        items: [
            { to: '/apps', label: 'Apps', icon: BlocksIcon },
            { to: '/music', label: 'Music work', icon: MusicIcon },
            { to: '/jellyfin', label: 'Jellyfin', icon: FilmIcon },
            { to: '/tailscale', label: 'Tailscale', icon: NetworkIcon },
        ],
    },
]
