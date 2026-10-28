import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { api, type BindingInput } from '@/lib/api'
import { Field } from './form-parts'

const empty: BindingInput = {
    id: '',
    role: 'photos',
    server: '',
    share: '',
    username: '',
    password: '',
    readOnly: false,
}
// Shares change when you connect or disconnect one here, which refreshes the
// list. The poll only notices a mount that dropped, and each check reads every
// container config on the host.
const bindingsQuery = { queryKey: ['storage-bindings'], queryFn: api.storageBindings, refetchInterval: 60000 }

export function StoragePage() {
    const query = useQuery(bindingsQuery)
    const client = useQueryClient()
    const [input, setInput] = useState(empty)
    const [removeID, setRemoveID] = useState<string | null>(null)
    const refresh = () => client.invalidateQueries({ queryKey: bindingsQuery.queryKey })
    const create = useMutation({
        mutationFn: () => api.createBinding(input),
        onSuccess: () => {
            setInput(empty)
            refresh()
            toast.success('Share mounted')
        },
        onError: (e) => toast.error(e.message),
    })
    const remove = useMutation({
        mutationFn: api.deleteBinding,
        onSuccess: () => {
            setRemoveID(null)
            refresh()
        },
        onError: (e) => toast.error(e.message),
    })
    const backups = useMutation({
        mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => api.setBindingBackups(id, enabled),
        onSuccess: (_, { enabled }) => {
            refresh()
            client.invalidateQueries({ queryKey: ['backups'] })
            toast.success(
                enabled ? 'Proxmox can keep its backups on this share now' : 'Proxmox no longer uses this share',
            )
        },
        onError: (e) => toast.error(e.message),
    })
    function submit(e: FormEvent) {
        e.preventDefault()
        create.mutate()
    }
    return (
        <div className="flex flex-col gap-6">
            <header>
                <h1 className="page-title">Storage</h1>
                <p className="text-sm text-muted-foreground">
                    Give each app access to its own NAS folders. Databases, caches and temporary files stay on the local
                    disk.
                </p>
            </header>
            {query.error && <p role="alert">{query.error.message}</p>}
            {query.data?.sample && (
                <p role="status" className="text-sm text-muted-foreground">
                    Development simulation. No NAS is contacted and no host mount is created.
                </p>
            )}
            {query.isPending && <p>Loading shares…</p>}
            {query.data?.bindings.map((b) => (
                <Card key={b.id}>
                    <CardHeader>
                        <CardTitle>
                            {b.id} · {b.role}
                        </CardTitle>
                    </CardHeader>
                    <CardContent className="flex flex-col gap-2 text-sm">
                        <p>
                            {b.source} · {b.readOnly ? 'Read only' : 'Read and write'} ·{' '}
                            {b.mounted ? 'Mounted' : 'Unavailable'}
                        </p>
                        <p className="text-muted-foreground">{b.path}</p>
                        <p>Containers: {b.consumers.join(', ') || 'None'}</p>
                        {b.role === 'backups' && !b.readOnly && (
                            <div className="flex flex-col gap-2 rounded-md border p-3">
                                <p>
                                    {b.proxmoxStorage
                                        ? `Proxmox keeps backups here as the storage ${b.proxmoxStorage}. Choose it on the Backups page.`
                                        : 'Proxmox can keep its container backups on this share, off the disk of the host.'}
                                </p>
                                <Button
                                    variant="outline"
                                    className="self-start"
                                    disabled={backups.isPending || (!b.mounted && !b.proxmoxStorage)}
                                    onClick={() => backups.mutate({ id: b.id, enabled: !b.proxmoxStorage })}
                                >
                                    {b.proxmoxStorage ? 'Stop using for Proxmox backups' : 'Use for Proxmox backups'}
                                </Button>
                            </div>
                        )}
                        {removeID === b.id ? (
                            <div className="flex items-center gap-2">
                                <span>Disconnect this share? NAS files stay in place.</span>
                                <Button
                                    variant="destructive"
                                    disabled={remove.isPending}
                                    onClick={() => remove.mutate(b.id)}
                                >
                                    Disconnect
                                </Button>
                                <Button variant="outline" onClick={() => setRemoveID(null)}>
                                    Cancel
                                </Button>
                            </div>
                        ) : (
                            <Button
                                variant="outline"
                                className="self-start"
                                disabled={b.consumers.length > 0 || Boolean(b.proxmoxStorage)}
                                onClick={() => setRemoveID(b.id)}
                            >
                                Disconnect
                            </Button>
                        )}
                    </CardContent>
                </Card>
            ))}
            <Card>
                <CardHeader>
                    <CardTitle>Connect an SMB share</CardTitle>
                </CardHeader>
                <CardContent>
                    <form onSubmit={submit} className="grid gap-4 sm:grid-cols-2">
                        <Field
                            id="binding-id"
                            label="Short ID"
                            help="Lowercase letters and digits, starting with a letter. This ID stays fixed."
                        >
                            <Input
                                id="binding-id"
                                required
                                pattern="[a-z][a-z0-9]{0,31}"
                                value={input.id}
                                onChange={(e) => setInput({ ...input, id: e.target.value })}
                            />
                        </Field>
                        <Field id="binding-role" label="Folder role">
                            <select
                                id="binding-role"
                                className="h-9 rounded-md border bg-background px-3 text-sm"
                                value={input.role}
                                onChange={(e) => setInput({ ...input, role: e.target.value })}
                            >
                                {['videos', 'music', 'photos', 'games', 'inbox', 'cameras', 'backups'].map((r) => (
                                    <option key={r}>{r}</option>
                                ))}
                            </select>
                        </Field>
                        {(['server', 'share', 'username', 'password'] as const).map((key) => (
                            <Field
                                key={key}
                                id={`binding-${key}`}
                                label={
                                    {
                                        server: 'NAS hostname or IP',
                                        share: 'Share name',
                                        username: 'SMB username',
                                        password: 'SMB password',
                                    }[key]
                                }
                            >
                                <Input
                                    id={`binding-${key}`}
                                    required
                                    autoComplete="off"
                                    type={key === 'password' ? 'password' : 'text'}
                                    value={input[key]}
                                    onChange={(e) => setInput({ ...input, [key]: e.target.value })}
                                />
                            </Field>
                        ))}
                        <label className="flex items-center gap-3 text-sm">
                            <Switch
                                checked={input.readOnly}
                                onCheckedChange={(readOnly) => setInput({ ...input, readOnly })}
                            />
                            Read only
                        </label>
                        <p className="text-xs text-muted-foreground sm:col-span-2">
                            Use a NAS account restricted to this share. Credentials stay on the host. Existing shared
                            media mounts are left in place; apps must explicitly select a new binding.
                        </p>
                        <Button className="justify-self-start" disabled={create.isPending}>
                            {create.isPending ? 'Connecting…' : 'Connect share'}
                        </Button>
                    </form>
                </CardContent>
            </Card>
        </div>
    )
}
