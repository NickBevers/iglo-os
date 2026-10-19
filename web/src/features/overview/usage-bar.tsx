import { BlockMeter } from '@/components/block-meter'

type UsageBarProps = {
    label: string
    value: number
    detail: string
}

export function UsageBar({ label, value, detail }: UsageBarProps) {
    return (
        <div className="flex flex-col gap-1.5">
            <div className="flex items-baseline gap-3 text-sm">
                <span className="text-muted-foreground">{label}</span>
                <span className="ml-auto tabular-nums">{detail}</span>
            </div>
            <BlockMeter value={value} label={label} />
        </div>
    )
}
