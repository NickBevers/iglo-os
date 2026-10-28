import { useQuery } from '@tanstack/react-query'
import { TriangleAlertIcon } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { tailscaleQuery } from '@/lib/queries'
import { ConnectCard } from './connect-card'
import { ControlServerCard } from './control-server-card'
import { ConnectedCard, DeviceList, ServeCard, SubnetCard } from './connected'
import { ServicesCard } from './services-card'

export function TailscalePage() {
    const { data, error, isPending } = useQuery(tailscaleQuery)

    if (isPending) {
        return (
            <div className="flex flex-col gap-6">
                <Skeleton className="h-10 w-64" />
                <Skeleton className="h-40 w-full rounded-xl" />
            </div>
        )
    }

    if (error) {
        return (
            <Alert variant="destructive">
                <TriangleAlertIcon />
                <AlertTitle>Could not load Tailscale</AlertTitle>
                <AlertDescription>{error.message}</AlertDescription>
            </Alert>
        )
    }

    const running = data.state === 'Running'
    const custom = Boolean(data.settings.loginServer)

    return (
        <div className="flex flex-col gap-8">
            <header>
                <h1 className="page-title">Remote access</h1>
                <p className="text-sm text-muted-foreground">
                    Open Iglo-OS and your home network from anywhere, without opening ports on your router.
                </p>
            </header>

            {data.installed && <ControlServerCard key={data.settings.loginServer || 'hosted'} tailscale={data} />}
            {!data.installed ? (
                <Alert>
                    <TriangleAlertIcon />
                    <AlertTitle>Tailscale is not installed</AlertTitle>
                    <AlertDescription>
                        Install the newest Iglo-OS version from the Updates page. The update installs Tailscale in the
                        manager container.
                    </AlertDescription>
                </Alert>
            ) : running ? (
                <>
                    <ConnectedCard tailscale={data} />
                    <div className="grid gap-4 lg:grid-cols-2">
                        {!custom && <ServeCard tailscale={data} />}
                        {custom && (
                            <p className="rounded-lg border p-4 text-sm">
                                Connected through Headscale. Reach Iglo-OS at its private IP over HTTPS, or use your own
                                DNS and certificate. The built-in LAN certificate is not automatically trusted for the
                                private IP.
                            </p>
                        )}
                        <SubnetCard key={data.settings.subnet} tailscale={data} />
                    </div>
                    {!custom && <ServicesCard tailscale={data} />}
                    <section className="flex flex-col gap-3">
                        <h2 className="section-title">Devices</h2>
                        <DeviceList peers={data.peers} />
                    </section>
                </>
            ) : (
                <ConnectCard tailscale={data} />
            )}
        </div>
    )
}
