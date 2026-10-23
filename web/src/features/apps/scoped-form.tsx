import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { api, type Apps, type SavedAnswers, type ScopedAnswers } from '@/lib/api'
import { Field } from './form-parts'

export function ScopedForm({
    id,
    defaults,
    saved,
    onDone,
}: {
    id: string
    defaults: Apps['defaults']
    saved: SavedAnswers | null
    onDone: () => void
}) {
    const bindings = useQuery({ queryKey: ['storage-bindings'], queryFn: api.storageBindings })
    const client = useQueryClient()
    const [answers, set] = useState<ScopedAnswers>(
        saved?.scoped?.libraryBinding
            ? saved.scoped
            : {
                  storage: defaults.storage,
                  libraryBinding: '',
                  outputBinding: '',
                  backupBinding: '',
                  cores: id === 'photos' ? 4 : 2,
                  memoryMiB: id === 'photos' ? 8192 : id === 'music' ? 1024 : 2048,
                  diskGB: 64,
              },
    )
    const install = useMutation({
        mutationFn: () => api.installScopedApp(id, answers),
        onSuccess: () => {
            client.invalidateQueries({ queryKey: ['apps'] })
            toast.success('Installation started')
            onDone()
        },
        onError: (e) => toast.error(e.message),
    })
    function submit(e: FormEvent) {
        e.preventDefault()
        install.mutate()
    }
    const roles =
        id === 'photos'
            ? ([
                  ['libraryBinding', 'photos', true],
                  ['backupBinding', 'backups', true],
              ] as const)
            : id === 'music'
              ? ([['libraryBinding', 'music', true]] as const)
              : ([
                    ['libraryBinding', 'inbox', false],
                    ['outputBinding', 'videos', true],
                ] as const)
    return (
        <form onSubmit={submit} className="flex flex-col gap-4">
            <p className="text-sm text-muted-foreground">
                Connect the required shares in Server → Storage first. Working files use the local container disk.
            </p>
            {bindings.error && <p role="alert">{bindings.error.message}</p>}
            {bindings.data?.sample && (
                <p className="text-sm text-muted-foreground">
                    Development simulation: installation does not create a real container.
                </p>
            )}
            {roles.map(([key, role, write]) => (
                <Field
                    key={key}
                    id={key}
                    label={`${role[0].toUpperCase() + role.slice(1)} share${write ? ' (write access)' : ' (read only)'}`}
                >
                    <select
                        id={key}
                        className="h-9 rounded-md border bg-background px-3 text-sm"
                        required
                        value={answers[key]}
                        onChange={(e) => set({ ...answers, [key]: e.target.value })}
                    >
                        <option value="">Select a connected share</option>
                        {bindings.data?.bindings
                            .filter((b) => b.role === role && b.mounted && (!write || !b.readOnly))
                            .map((b) => (
                                <option key={b.id} value={b.id}>
                                    {b.id} · {b.source}
                                </option>
                            ))}
                    </select>
                </Field>
            ))}
            <Field id="scoped-storage" label="Local container storage">
                <select
                    id="scoped-storage"
                    className="h-9 rounded-md border bg-background px-3 text-sm"
                    required
                    value={answers.storage}
                    onChange={(e) => set({ ...answers, storage: e.target.value })}
                >
                    {defaults.storages.map((s) => (
                        <option key={s}>{s}</option>
                    ))}
                </select>
            </Field>
            <div className="grid gap-4 sm:grid-cols-3">
                {(['cores', 'memoryMiB', 'diskGB'] as const).map((key) => (
                    <Field
                        key={key}
                        id={key}
                        label={{ cores: 'CPU cores', memoryMiB: 'RAM (MiB)', diskGB: 'Local disk (GB)' }[key]}
                    >
                        <Input
                            id={key}
                            type="number"
                            required
                            min={
                                key === 'cores'
                                    ? 1
                                    : key === 'diskGB'
                                      ? 32
                                      : id === 'photos'
                                        ? 8192
                                        : id === 'music'
                                          ? 1024
                                          : 512
                            }
                            max={key === 'cores' ? 4 : key === 'diskGB' ? 2048 : id === 'photos' ? 8192 : 2048}
                            value={answers[key]}
                            onChange={(e) => set({ ...answers, [key]: Number(e.target.value) })}
                        />
                    </Field>
                ))}
            </div>
            {id === 'photos' && (
                <p className="text-sm text-muted-foreground">
                    After installation, open Immich to create the first admin account. Create a key with queue
                    read/update permissions and connect it in Background work. Backups include originals and the
                    database; snapshots alone do not.
                </p>
            )}
            {id === 'inbox' && (
                <p className="text-sm text-muted-foreground">
                    Open FileFlows to set up a flow, one runner and its schedule. Source files are read only. Configure
                    output under /media/output; temporary files use /temp.
                </p>
            )}
            <div className="flex justify-end gap-2">
                <Button type="button" variant="outline" onClick={onDone}>
                    Cancel
                </Button>
                <Button disabled={install.isPending || !bindings.data || roles.some(([key]) => !answers[key])}>
                    {install.isPending ? 'Starting…' : 'Install'}
                </Button>
            </div>
        </form>
    )
}
