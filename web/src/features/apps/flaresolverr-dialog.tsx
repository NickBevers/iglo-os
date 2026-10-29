import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2Icon } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog'
import { Switch } from '@/components/ui/switch'
import { api, type CatalogApp } from '@/lib/api'
import { appsQuery } from '@/lib/queries'

type FlareSolverrDialogProps = { app: CatalogApp; open: boolean; onOpenChange: (open: boolean) => void }

// FlareSolverrDialog turns FlareSolverr of the media stack on or off, without
// passwords and without a reinstall.
export function FlareSolverrDialog({ app, open, onOpenChange }: FlareSolverrDialogProps) {
    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-lg">
                <DialogHeader>
                    <DialogTitle>FlareSolverr</DialogTitle>
                    <DialogDescription>
                        FlareSolverr runs a browser for indexers behind Cloudflare. It can take up to 1 GB of memory, so
                        leave it off when your indexers don't need it.
                    </DialogDescription>
                </DialogHeader>
                {open && <FlareSolverrForm app={app} onDone={() => onOpenChange(false)} />}
            </DialogContent>
        </Dialog>
    )
}

function FlareSolverrForm({ app, onDone }: { app: CatalogApp; onDone: () => void }) {
    const queryClient = useQueryClient()
    const current = app.flareSolverr ?? false
    const [enabled, setEnabled] = useState(current)

    const change = useMutation({
        mutationFn: () => api.changeFlareSolverr(app.id, enabled),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: appsQuery.queryKey })
            onDone()
        },
        onError: (error) => toast.error('Could not change FlareSolverr', { description: error.message }),
    })

    return (
        <div className="flex flex-col gap-4">
            <label className="flex items-center justify-between gap-4 text-sm">
                <span>
                    Run FlareSolverr
                    <span className="block text-xs text-muted-foreground">
                        {enabled
                            ? 'Prowlarr sends the indexers with the flaresolverr tag through it.'
                            : 'Prowlarr gets no FlareSolverr proxy. Indexers keep the flaresolverr tag, so they use it again when you turn it back on.'}
                    </span>
                </span>
                <Switch checked={enabled} onCheckedChange={setEnabled} />
            </label>
            <DialogFooter>
                <Button disabled={change.isPending || enabled === current} onClick={() => change.mutate()}>
                    {change.isPending && <Loader2Icon className="animate-spin" />}
                    {enabled ? 'Turn on' : 'Turn off'}
                </Button>
            </DialogFooter>
        </div>
    )
}
