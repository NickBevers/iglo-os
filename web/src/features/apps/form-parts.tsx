import type { ReactNode } from 'react'

import { Label } from '@/components/ui/label'

export function Section({ title, help, children }: { title: string; help?: string; children: ReactNode }) {
    return (
        // A hairline between groups. The first group of a form has none.
        <fieldset className="flex flex-col gap-4 border-t pt-5 first:border-t-0 first:pt-0">
            <div className="flex flex-col gap-0.5">
                <legend className="section-title">{title}</legend>
                {help && <p className="text-xs leading-snug text-muted-foreground">{help}</p>}
            </div>
            {children}
        </fieldset>
    )
}

type FieldProps = { id: string; label: string; help?: ReactNode; error?: string; children: ReactNode }

export function Field({ id, label, help, error, children }: FieldProps) {
    return (
        <div className="flex flex-col gap-1.5">
            <Label htmlFor={id}>{label}</Label>
            {children}
            {error ? (
                <p className="text-xs leading-snug text-destructive">{error}</p>
            ) : (
                help && <p className="text-xs leading-snug text-muted-foreground">{help}</p>
            )}
        </div>
    )
}
