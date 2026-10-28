import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
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
                  cores: id === 'photos' || id === 'gaming' ? 4 : 2,
                  memoryMiB: id === 'photos' ? 8192 : id === 'music' ? 1024 : id === 'gaming' ? 6144 : 2048,
                  diskGB: id === 'gaming' ? 96 : 64,
                  privileged: false,
              },
    )
    // The limits that the host agent checks too.
    const limits = {
        cores: [id === 'gaming' ? 2 : 1, 4],
        memoryMiB:
            id === 'photos'
                ? [8192, 8192]
                : id === 'music'
                  ? [1024, 2048]
                  : id === 'gaming'
                    ? [4096, 8192]
                    : [512, 2048],
        diskGB: [id === 'gaming' ? 64 : 32, 2048],
    }
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
              : id === 'gaming'
                ? ([['libraryBinding', 'games', false]] as const)
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
                            min={limits[key][0]}
                            max={limits[key][1]}
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
                    database; snapshots alone do not. With a GPU, face detection and smart search use it by themselves;
                    for video, turn on Administration &gt; Settings &gt; Video transcoding &gt; Hardware acceleration
                    &gt; Quick Sync.
                </p>
            )}
            {id === 'inbox' && (
                <p className="text-sm text-muted-foreground">
                    Open Inbox work to review the profile and resume the paused queue. Sources stay read only. FFmpeg
                    stages on SSD and publishes to Videos/home-videos. The playback profile uses VAAPI when verified,
                    with a CPU fallback.
                </p>
            )}
            {id === 'gaming' && (
                <>
                    <p className="text-sm text-muted-foreground">
                        The Games share needs roms and bios folders. The host needs the Intel GPU, the uinput and uhid
                        devices, and the reviewed Wolf input rules. The installer checks all of them first.
                    </p>
                    <label className="flex items-start gap-3 text-sm">
                        <Checkbox
                            className="mt-0.5"
                            checked={answers.privileged === true}
                            onCheckedChange={(checked) => set({ ...answers, privileged: checked === true })}
                        />
                        <span>
                            I understand that Wolf needs a privileged container with access to the GPU, input devices
                            and Docker. It is far less isolated than the other apps, so treat it and every game image it
                            starts as trusted software on the host.
                        </span>
                    </label>
                </>
            )}
            <div className="flex justify-end gap-2">
                <Button type="button" variant="outline" onClick={onDone}>
                    Cancel
                </Button>
                <Button
                    disabled={
                        install.isPending ||
                        !bindings.data ||
                        roles.some(([key]) => !answers[key]) ||
                        (id === 'gaming' && !answers.privileged)
                    }
                >
                    {install.isPending ? 'Starting…' : 'Install'}
                </Button>
            </div>
        </form>
    )
}
