import { useState, type ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { Apps } from '@/lib/api'
import { Field, Section } from './form-parts'
import { keepSaved } from './keep-saved'
import { noMediaSource, type MediaSource } from './no-media-source'

type Where = 'smb' | 'nfs' | 'folder'

const fieldsOf: Record<Where, (keyof MediaSource)[]> = {
    smb: ['smbServer', 'smbShare', 'smbUsername', 'smbPassword'],
    nfs: ['nasServer', 'nasExport'],
    folder: ['mediaFolder'],
}

function whereOf(value: MediaSource): Where {
    if (value.mediaFolder) {
        return 'folder'
    }
    return value.nasServer || value.nasExport ? 'nfs' : 'smb'
}

type MediaSourceFieldsProps = {
    id: string
    defaults: Apps['defaults']
    value: MediaSource
    // smbPasswordSaved is set when the host still has the SMB password of a failed install.
    smbPasswordSaved: boolean
    onChange: (value: Partial<MediaSource>) => void
    help: string
    // children are the movies and series fields. path is where the media is, for their help.
    children: (path: string) => ReactNode
}

export function MediaSourceFields({
    id,
    defaults,
    value,
    smbPasswordSaved,
    onChange,
    help,
    children,
}: MediaSourceFieldsProps) {
    const [where, setWhere] = useState<Where>(() => whereOf(value))

    // choose clears the answers of the other places.
    function choose(next: Where) {
        setWhere(next)
        const cleared: Partial<MediaSource> = { ...noMediaSource }
        for (const key of fieldsOf[next]) {
            delete cleared[key]
        }
        onChange(cleared)
    }

    if (defaults.mediaShare) {
        return (
            <Section title="Media" help={help}>
                <p className="text-sm">
                    Uses the media that is already mounted: <span className="font-mono">{defaults.mediaShare}</span>
                </p>
                {children(defaults.mediaShare)}
            </Section>
        )
    }

    const paths: Record<Where, string> = {
        smb: `//${value.smbServer || 'NAS'}/${value.smbShare || 'Videos'}`,
        nfs: `${value.nasServer || 'NAS'}:${value.nasExport || '/volume1/media'}`,
        folder: value.mediaFolder || '/mnt/pve/media',
    }
    const choices: [Where, string][] = [
        ['smb', 'SMB share'],
        ['nfs', 'NFS share'],
        ['folder', 'On this host'],
    ]

    return (
        <Section title="Media" help={help}>
            <div role="radiogroup" aria-label="Where your movies and series are" className="grid grid-cols-3 gap-2">
                {choices.map(([choice, label]) => (
                    <Button
                        key={choice}
                        type="button"
                        role="radio"
                        aria-checked={where === choice}
                        variant={where === choice ? 'secondary' : 'outline'}
                        onClick={() => choose(choice)}
                    >
                        {label}
                    </Button>
                ))}
            </div>

            {where === 'smb' && (
                <div className="grid gap-4 sm:grid-cols-2">
                    <Field
                        id={`${id}-smb-server`}
                        label="NAS address"
                        help="Use the IP address. The Proxmox host can't look up names like umbrel.local."
                    >
                        <Input
                            id={`${id}-smb-server`}
                            placeholder="192.168.1.20"
                            value={value.smbServer}
                            onChange={(event) => onChange({ smbServer: event.target.value.trim() })}
                            required
                        />
                    </Field>
                    <Field
                        id={`${id}-smb-share`}
                        label="Share"
                        help="One shared folder, without spaces. On an Umbrel: right-click the folder in Files, then Share over network."
                    >
                        <Input
                            id={`${id}-smb-share`}
                            placeholder="Videos"
                            value={value.smbShare}
                            onChange={(event) => onChange({ smbShare: event.target.value.trim() })}
                            required
                        />
                    </Field>
                    <Field
                        id={`${id}-smb-username`}
                        label="Username"
                        help="On an Umbrel, Settings > File sharing shows it. It is not your Umbrel login."
                    >
                        <Input
                            id={`${id}-smb-username`}
                            autoComplete="off"
                            value={value.smbUsername}
                            onChange={(event) => onChange({ smbUsername: event.target.value.trim() })}
                            required
                        />
                    </Field>
                    <Field
                        id={`${id}-smb-password`}
                        label="Password"
                        help="It stays on the Proxmox host, in a file that only root can read."
                    >
                        <Input
                            id={`${id}-smb-password`}
                            type="password"
                            autoComplete="new-password"
                            value={value.smbPassword}
                            onChange={(event) => onChange({ smbPassword: event.target.value })}
                            placeholder={smbPasswordSaved ? keepSaved : undefined}
                            required={!smbPasswordSaved}
                        />
                    </Field>
                </div>
            )}

            {where === 'nfs' && (
                <>
                    <Field id={`${id}-nas-server`} label="NAS address">
                        <Input
                            id={`${id}-nas-server`}
                            placeholder="192.168.1.5"
                            value={value.nasServer}
                            onChange={(event) => onChange({ nasServer: event.target.value.trim() })}
                            required
                        />
                    </Field>
                    <Field id={`${id}-nas-export`} label="NFS export path" help="UGOS shows it on the NFS page.">
                        <Input
                            id={`${id}-nas-export`}
                            placeholder="/volume1/media"
                            value={value.nasExport}
                            onChange={(event) => onChange({ nasExport: event.target.value.trim() })}
                            required
                        />
                    </Field>
                </>
            )}

            {where === 'folder' && (
                <Field
                    id={`${id}-media-folder`}
                    label="Folder on the Proxmox host"
                    help="A folder on a disk of the host. For a new disk, make one first in Proxmox: Disks > Directory."
                >
                    <Input
                        id={`${id}-media-folder`}
                        placeholder="/mnt/pve/media"
                        list={`${id}-media-folders`}
                        autoComplete="off"
                        value={value.mediaFolder}
                        onChange={(event) => onChange({ mediaFolder: event.target.value.trim() })}
                        required
                    />
                    <datalist id={`${id}-media-folders`}>
                        {defaults.mediaFolders.map((folder) => (
                            <option key={folder.path} value={folder.path}>
                                {folder.storage}
                            </option>
                        ))}
                    </datalist>
                </Field>
            )}

            {children(paths[where])}
        </Section>
    )
}
