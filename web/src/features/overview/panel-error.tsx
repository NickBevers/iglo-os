import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { cn } from '@/lib/utils'

// PanelError takes the place of a panel's skeleton when its query failed, so
// the overview says what is missing instead of loading forever.
export function PanelError({
    title,
    error,
    retry,
    className,
}: {
    title: string
    error: Error
    retry: () => void
    className?: string
}) {
    return (
        <Card className={cn('flex flex-col items-start justify-center gap-2 p-4 text-sm', className)}>
            <span className="font-medium">{title}</span>
            <span className="text-muted-foreground">Could not load this: {error.message}</span>
            <Button size="sm" variant="outline" onClick={retry}>
                Try again
            </Button>
        </Card>
    )
}
