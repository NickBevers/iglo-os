import { useState, type FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { api, type Tailscale } from '@/lib/api'
import { useTailscaleMutation } from './use-tailscale-mutation'

export function ControlServerCard({ tailscale }: { tailscale: Tailscale }) {
    const [headscale, setHeadscale] = useState(Boolean(tailscale.settings.loginServer))
    const [url, setURL] = useState(tailscale.settings.loginServer || '')
    const save = useTailscaleMutation(api.saveTailscaleSettings, () => 'Control server saved. Connect below.')
    const logout = useTailscaleMutation(api.logoutTailscale, () => 'Saved remote session disconnected')
    const locked = tailscale.connecting || !['NeedsLogin', 'NoState'].includes(tailscale.state)
    function submit(event: FormEvent) {
        event.preventDefault()
        save.mutate({ ...tailscale.settings, loginServer: headscale ? url : '' })
    }
    return (
        <Card>
            <CardHeader>
                <CardTitle>Control server</CardTitle>
            </CardHeader>
            <CardContent>
                <form onSubmit={submit} className="flex flex-col gap-3">
                    <Label htmlFor="control-server">Provider</Label>
                    <select
                        id="control-server"
                        className="rounded-md border bg-background p-2 text-sm"
                        value={headscale ? 'headscale' : 'tailscale'}
                        disabled={locked || save.isPending}
                        onChange={(e) => setHeadscale(e.target.value === 'headscale')}
                    >
                        <option value="headscale">Headscale, self-hosted and open source</option>
                        <option value="tailscale">Tailscale hosted service</option>
                    </select>
                    {headscale && (
                        <>
                            <Label htmlFor="headscale-url">Headscale HTTPS URL</Label>
                            <Input
                                id="headscale-url"
                                type="url"
                                required
                                placeholder="https://headscale.example.com"
                                value={url}
                                disabled={locked || save.isPending}
                                onChange={(e) => setURL(e.target.value)}
                            />
                        </>
                    )}
                    <p className="text-sm text-muted-foreground">
                        Headscale needs an existing reachable HTTPS controller. Devices must join the same controller.
                        Private IP access and subnet routing work; managed Tailscale Serve, Services and ts.net
                        certificates are unavailable. Configure your own DNS and TLS if needed.
                    </p>
                    {locked && (
                        <p className="text-sm text-muted-foreground">
                            Disconnect before changing provider. Keep a LAN connection available to reconnect.
                        </p>
                    )}
                    {locked && !tailscale.connecting && tailscale.state !== 'Running' && (
                        <Button
                            type="button"
                            variant="outline"
                            className="self-start"
                            disabled={logout.isPending}
                            onClick={() => logout.mutate(undefined)}
                        >
                            Disconnect saved session
                        </Button>
                    )}
                    <Button type="submit" variant="outline" className="self-start" disabled={locked || save.isPending}>
                        Save control server
                    </Button>
                </form>
            </CardContent>
        </Card>
    )
}
