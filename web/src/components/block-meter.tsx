import { meterTone } from '@/lib/meter'
import { cn } from '@/lib/utils'

type BlockMeterProps = {
    // value is a percentage, 0 to 100.
    value: number
    label: string
    blocks?: number
    size?: 'sm' | 'md'
    className?: string
}

const filled = { normal: 'bg-brand', warning: 'bg-warning', critical: 'bg-destructive' }

// BlockMeter shows a usage as a row of blocks, like the courses of an igloo.
export function BlockMeter({ value, label, blocks = 24, size = 'md', className }: BlockMeterProps) {
    const clamped = Math.min(100, Math.max(0, value))
    // Any use at all shows one block, so 1% does not look like nothing.
    const count = clamped > 0 ? Math.max(1, Math.round((clamped / 100) * blocks)) : 0
    const tone = meterTone(clamped)

    return (
        <div
            role="meter"
            aria-label={label}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={Math.round(clamped)}
            className={cn('flex w-full gap-[2px]', size === 'sm' ? 'h-1.5' : 'h-2.5', className)}
        >
            {Array.from({ length: blocks }, (_, index) => (
                <span
                    key={index}
                    className={cn('min-w-0 flex-1 rounded-[2px]', index < count ? filled[tone] : 'bg-foreground/8')}
                />
            ))}
        </div>
    )
}
