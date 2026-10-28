import { useState, type SubmitEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
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
import { Skeleton } from '@/components/ui/skeleton'
import { api, type CatalogApp, type VpnSettings, type VpnView } from '@/lib/api'
import { appsQuery } from '@/lib/queries'
import { VpnFields } from './vpn-fields'

type VpnDialogProps = { app: CatalogApp; open: boolean; onOpenChange: (open: boolean) => void }

export function VpnDialog({ app, open, onOpenChange }: VpnDialogProps) {
    const { data, error, isFetching } = useQuery({
        queryKey: ['apps', app.id, 'vpn'],
        queryFn: () => api.vpnSettings(app.id),
        enabled: open,
        staleTime: 0,
    })

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-lg">
                <DialogHeader>
                    <DialogTitle>VPN settings</DialogTitle>
                    <DialogDescription>
                        qBittorrent and Prowlarr restart with the new settings, and FlareSolverr when it runs, so
                        downloads stop for a minute. If the VPN does not connect, the old settings come back.
                    </DialogDescription>
                </DialogHeader>
                {error ? (
                    <p className="text-sm text-destructive">{error.message}</p>
                ) : data && !isFetching ? (
                    <VpnForm app={app} current={data} onDone={() => onOpenChange(false)} />
                ) : (
                    <Skeleton className="h-40 w-full" />
                )}
            </DialogContent>
        </Dialog>
    )
}

function VpnForm({ app, current, onDone }: { app: CatalogApp; current: VpnView; onDone: () => void }) {
    const queryClient = useQueryClient()
    // The secrets stay in the container: empty fields keep them.
    const [settings, setSettings] = useState<VpnSettings>({
        provider: current.provider,
        locations: current.locations,
        wireguardPrivateKey: '',
        piaUsername: '',
        piaPassword: '',
        piaDedicatedToken: '',
    })
    const keep = {
        wireguardKey: current.hasWireguardKey,
        piaLogin: current.hasPiaLogin,
        dedicatedToken: current.hasDedicatedToken,
    }

    const change = useMutation({
        mutationFn: () => api.changeVpn(app.id, settings),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: appsQuery.queryKey })
            onDone()
        },
        onError: (error) => toast.error('Could not change the VPN settings', { description: error.message }),
    })

    const submit = (event: SubmitEvent) => {
        event.preventDefault()
        change.mutate()
    }

    return (
        <form onSubmit={submit} className="flex flex-col gap-4">
            <VpnFields
                id="vpn"
                value={settings}
                onChange={(patch) => setSettings((now) => ({ ...now, ...patch }))}
                keep={keep}
            />
            <DialogFooter>
                <Button type="submit" disabled={change.isPending || settings.locations === ''}>
                    {change.isPending && <Loader2Icon className="animate-spin" />}
                    Save and restart the VPN
                </Button>
            </DialogFooter>
        </form>
    )
}
