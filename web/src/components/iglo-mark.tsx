import { cn } from '@/lib/utils'

// IgloMark is the logo: an igloo of offset blocks, the same blocks as the meters.
export function IgloMark({ className }: { className?: string }) {
    return (
        <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" className={cn('size-4', className)}>
            <path d="M7.2 9.2a4.8 4.4 0 0 1 9.6 0Z" />
            <rect x="4" y="10.4" width="7.4" height="3.6" rx="0.8" />
            <rect x="12.6" y="10.4" width="7.4" height="3.6" rx="0.8" />
            <rect x="2" y="15.2" width="6.6" height="4.8" rx="0.8" />
            <rect x="15.4" y="15.2" width="6.6" height="4.8" rx="0.8" />
            <path d="M9.8 20v-2.4a2.2 2.2 0 0 1 4.4 0V20Z" opacity="0.45" />
        </svg>
    )
}
