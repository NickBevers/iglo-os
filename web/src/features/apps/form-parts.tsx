import { cloneElement, isValidElement, type ReactNode } from 'react'

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
    // The input names its help or error, so a screen reader reads it with the
    // field, and an error marks the input invalid (WCAG 1.3.1 and 3.3.1).
    const noteID = error ? `${id}-error` : help ? `${id}-help` : undefined
    const input =
        noteID && isValidElement<{ 'aria-describedby'?: string; 'aria-invalid'?: boolean }>(children)
            ? cloneElement(children, {
                  'aria-describedby': [children.props['aria-describedby'], noteID].filter(Boolean).join(' '),
                  'aria-invalid': error ? true : children.props['aria-invalid'],
              })
            : children

    return (
        <div className="flex flex-col gap-1.5">
            <Label htmlFor={id}>{label}</Label>
            {input}
            {error ? (
                <p id={noteID} className="text-xs leading-snug text-destructive">
                    {error}
                </p>
            ) : (
                help && (
                    <p id={noteID} className="text-xs leading-snug text-muted-foreground">
                        {help}
                    </p>
                )
            )}
        </div>
    )
}
