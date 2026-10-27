import type { ReactNode } from 'react'
import { Link } from '@tanstack/react-router'

import { Field, Section } from './form-parts'
import type { useVideosShares } from './use-videos-shares'

type VideosShareFieldsProps = {
    id: string
    shares: ReturnType<typeof useVideosShares>
    value: string
    onChange: (id: string) => void
    write: boolean
    help: string
    // children are the movies and series fields. path is the share, for their help.
    children: (path: string) => ReactNode
}

export function VideosShareFields({ id, shares, value, onChange, write, help, children }: VideosShareFieldsProps) {
    const share = shares.shares.find((share) => share.id === value)

    return (
        <Section title="Media" help={help}>
            {shares.error && <p role="alert">{shares.error.message}</p>}
            {!shares.loading && shares.shares.length === 0 ? (
                <p className="text-sm">
                    Connect a Videos share{write ? ' with write access' : ''} in{' '}
                    <Link to="/storage" className="underline">
                        Storage
                    </Link>{' '}
                    first.
                </p>
            ) : (
                <Field id={`${id}-videos`} label={`Videos share${write ? ' (write access)' : ''}`}>
                    <select
                        id={`${id}-videos`}
                        className="h-9 rounded-md border bg-background px-3 text-sm"
                        required
                        value={value}
                        onChange={(event) => onChange(event.target.value)}
                    >
                        <option value="">Select a connected share</option>
                        {shares.shares.map((share) => (
                            <option key={share.id} value={share.id}>
                                {share.id} · {share.source}
                            </option>
                        ))}
                    </select>
                </Field>
            )}
            {children(share?.source ?? '//NAS/Videos')}
        </Section>
    )
}
