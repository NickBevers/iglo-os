import { Link } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import type { CatalogApp } from '@/lib/api'

export function HeadscaleDetails({ app }: { app: CatalogApp }) {
    const url = app.controllerHost ? `https://${app.controllerHost}` : ''
    return (
        <section className="flex flex-col gap-3 rounded-md border p-4 text-sm">
            <p className="font-medium">Controller: {url || 'Check the container description'}</p>
            <p>
                Installation and updates check local health. For current service and certificate status, open the guest
                console and run systemctl status headscale caddy and journalctl -u caddy. Internet reachability is not
                verified by Iglo.
            </p>
            <p>
                From mobile data, open {url}/health. Test the relay with your client’s netcheck command. UDP 3478
                provides STUN; relayed traffic uses HTTPS on TCP 443.
            </p>
            <p>
                Approve devices from the guest console. The installer creates an iglo user. Run{' '}
                <code>headscale users list</code> to find its ID, then follow the command on the device’s registration
                page. Device keys stay in the console.
            </p>
            <p>
                This controller serves one private network. Registered devices can reach each other by default; edit its
                policy to restrict access. If this Mini PC is off, registration and relayed connections stop.
            </p>
            <div className="flex flex-wrap gap-3">
                <Button
                    variant="outline"
                    onClick={() => {
                        void navigator.clipboard.writeText(url || '').then(
                            () => toast.success('Controller URL copied'),
                            () => toast.error('Could not copy the URL'),
                        )
                    }}
                    disabled={!url}
                >
                    Copy controller URL
                </Button>
                <Link to="/tailscale" className="self-center underline">
                    Configure remote access
                </Link>
            </div>
            <p className="text-muted-foreground">
                Connecting the manager to this controller is a separate action. Keep LAN access available before
                changing its current connection.
            </p>
        </section>
    )
}
