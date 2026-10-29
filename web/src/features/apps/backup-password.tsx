import { Input } from '@/components/ui/input'
import { Field } from './form-parts'

type Props = {
    id: string
    password: string
    confirm: string
    problem: string
    onPassword: (password: string) => void
    onConfirm: (confirm: string) => void
}

// BackupPasswordFields ask for the password of the Photos backups twice.
export function BackupPasswordFields({ id, password, confirm, problem, onPassword, onConfirm }: Props) {
    return (
        <div className="grid gap-4 sm:grid-cols-2">
            <Field
                id={id}
                label="Backup password"
                help="Iglo-OS seals it on the host and can't show it again. Save it in your password manager first. On a new PC, install Photos with the same password to open the old backups."
            >
                <Input
                    id={id}
                    type="password"
                    required
                    minLength={12}
                    maxLength={128}
                    autoComplete="new-password"
                    value={password}
                    onChange={(e) => onPassword(e.target.value)}
                />
            </Field>
            <Field id={`${id}-confirm`} label="Backup password again" error={problem || undefined}>
                <Input
                    id={`${id}-confirm`}
                    type="password"
                    required
                    minLength={12}
                    maxLength={128}
                    autoComplete="new-password"
                    value={confirm}
                    onChange={(e) => onConfirm(e.target.value)}
                />
            </Field>
        </div>
    )
}
