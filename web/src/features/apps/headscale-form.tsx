import { useState, type SubmitEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { api, type Apps, type HeadscaleAnswers, type SavedAnswers } from '@/lib/api'
import { Field } from './form-parts'

export function HeadscaleForm({
    defaults,
    saved,
    onDone,
}: {
    defaults: Apps['defaults']
    saved: SavedAnswers | null
    onDone: () => void
}) {
    const [answers, set] = useState<HeadscaleAnswers>(
        saved?.headscale?.hostname
            ? { ...saved.headscale, prerequisites: false }
            : {
                  hostname: '',
                  address: '',
                  // The host's gateway on vmbr0 is the guest's too.
                  gateway: defaults.lanGateway ?? '',
                  storage: defaults.storage,
                  rateMB: 5,
                  prerequisites: false,
              },
    )
    const client = useQueryClient()
    const install = useMutation({
        mutationFn: () => api.installHeadscale(answers),
        onSuccess: () => {
            void client.invalidateQueries({ queryKey: ['apps'] })
            toast.success('Controller installation started')
            onDone()
        },
        onError: (e) => toast.error(e.message),
    })
    function submit(event: SubmitEvent) {
        event.preventDefault()
        install.mutate()
    }
    return (
        <form onSubmit={submit} className="flex flex-col gap-4">
            <p className="text-sm">
                A dedicated container uses one core, a fixed 512 MiB RAM cap and 8 GiB local disk. Installation checks
                all guest caps, including stopped guests, and keeps 4 GiB for the host. No NAS share is needed.
            </p>
            <div className="rounded-md border p-3 text-sm">
                <p className="font-medium">Before installing</p>
                <ul className="mt-2 list-disc space-y-1 pl-5">
                    <li>
                        Point a public DNS A record at your public IPv4 address. Leave the AAAA record unset for this
                        IPv4 setup.
                    </li>
                    <li>
                        Forward TCP 80 and 443, and UDP 3478, to the guest’s static LAN address. Allow these ports in
                        Proxmox too if its firewall is enabled.
                    </li>
                    <li>
                        Your connection must accept inbound traffic. Carrier-grade NAT prevents ordinary port
                        forwarding.
                    </li>
                    <li>
                        Choose an unused address outside the DHCP pool, in the vmbr0 subnet. Local devices need NAT
                        loopback or a local DNS override for the public hostname.
                    </li>
                </ul>
            </div>
            <Field id="controller-host" label="Public hostname">
                <Input
                    id="controller-host"
                    required
                    autoCapitalize="none"
                    value={answers.hostname}
                    placeholder="vpn.yourdomain.com"
                    onChange={(e) => set({ ...answers, hostname: e.target.value.toLowerCase() })}
                />
            </Field>
            <Field
                id="controller-ip"
                label="Static guest IPv4 address and prefix"
                help={
                    defaults.lanAddress
                        ? `The host has ${defaults.lanAddress} on vmbr0. Use another free address with the same /${defaults.lanAddress.split('/')[1]}.`
                        : undefined
                }
            >
                <Input
                    id="controller-ip"
                    required
                    value={answers.address}
                    placeholder="192.168.1.210/24"
                    onChange={(e) => set({ ...answers, address: e.target.value })}
                />
            </Field>
            <Field id="controller-gateway" label="LAN gateway">
                <Input
                    id="controller-gateway"
                    required
                    value={answers.gateway}
                    placeholder="192.168.1.1"
                    onChange={(e) => set({ ...answers, gateway: e.target.value })}
                />
            </Field>
            <Field id="controller-storage" label="Local container storage">
                <select
                    id="controller-storage"
                    className="rounded-md border bg-background p-2 text-sm"
                    value={answers.storage}
                    onChange={(e) => set({ ...answers, storage: e.target.value })}
                >
                    {defaults.storages.map((s) => (
                        <option key={s}>{s}</option>
                    ))}
                </select>
            </Field>
            <Field id="controller-rate" label="Network limit (MiB/s)">
                <Input
                    id="controller-rate"
                    type="number"
                    min={1}
                    max={25}
                    required
                    value={answers.rateMB}
                    onChange={(e) => set({ ...answers, rateMB: Number(e.target.value) })}
                />
            </Field>
            <p className="text-sm text-muted-foreground">
                The embedded relay uses this guest’s network limit. VPN traffic normally connects devices directly.
                Certificates use Let’s Encrypt. Installation checks local health; public reachability must be tested
                from outside your home network. This controller goes offline when the Mini PC is off.
            </p>
            <label className="flex items-start gap-3 text-sm">
                <input
                    type="checkbox"
                    className="mt-1"
                    required
                    checked={answers.prerequisites}
                    onChange={(e) => set({ ...answers, prerequisites: e.target.checked })}
                />
                I have checked public DNS, port forwarding and this static address.
            </label>
            <Button type="submit" disabled={install.isPending || !answers.prerequisites}>
                Install Headscale
            </Button>
        </form>
    )
}
