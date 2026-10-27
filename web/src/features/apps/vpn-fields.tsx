import { Input } from '@/components/ui/input'
import { MultiCombobox } from '@/components/ui/multi-combobox'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import type { VpnProvider, VpnSettings } from '@/lib/api'
import locations from '@/lib/vpn-locations.json'
import { Field } from './form-parts'

// dedicated is the location of a PIA dedicated IP. It goes alone.
export const dedicated = 'Dedicated IP'

const providers: Record<VpnProvider, string> = { protonvpn: 'ProtonVPN', pia: 'Private Internet Access (PIA)' }

// Keep says which secrets the host keeps sealed for the media stack, so their
// fields may stay empty.
export type Keep = { wireguardKey?: boolean; piaLogin?: boolean; dedicatedToken?: boolean }

// maxLocations is what Gluetun gets at most; the agent checks the same.
const maxLocations = 10

type VpnFieldsProps = {
    id: string
    value: VpnSettings
    onChange: (patch: Partial<VpnSettings>) => void
    keep?: Keep
}

// VpnFields choose the VPN of the media stack: the provider, its login and
// the locations with port forwarding that Gluetun knows.
export function VpnFields({ id, value, onChange, keep = {} }: VpnFieldsProps) {
    const pia = value.provider === 'pia'
    const hasToken = value.piaDedicatedToken !== '' || Boolean(keep.dedicatedToken)
    const chosen = value.locations ? value.locations.split(',').map((location) => location.trim()) : []
    const items = pia ? [...(hasToken ? [dedicated] : []), ...locations.pia] : locations.protonvpn

    const setProvider = (provider: VpnProvider) => {
        if (provider !== value.provider) {
            // Netherlands is a location of both, and has port forwarding. The
            // other provider's typed secrets go: the host keeps its sealed ones.
            onChange({
                provider,
                locations: 'Netherlands',
                wireguardPrivateKey: '',
                piaUsername: '',
                piaPassword: '',
                piaDedicatedToken: '',
            })
        }
    }

    const setLocations = (next: string[]) => {
        // The dedicated IP is one server, so it can't go with other locations.
        const added = next.find((location) => !chosen.includes(location))
        if (added === dedicated) next = [dedicated]
        else if (added) next = next.filter((location) => location !== dedicated)
        onChange({ locations: next.slice(0, maxLocations).join(',') })
    }

    const setToken = (token: string) => {
        // Without a token, the dedicated IP can't be chosen.
        const dropped = token === '' && !keep.dedicatedToken && chosen.includes(dedicated)
        onChange(dropped ? { piaDedicatedToken: token, locations: '' } : { piaDedicatedToken: token })
    }

    return (
        <div className="flex flex-col gap-4">
            <Field id={`${id}-provider`} label="VPN provider">
                <Select
                    items={providers}
                    value={value.provider}
                    onValueChange={(provider) => provider && setProvider(provider as VpnProvider)}
                >
                    <SelectTrigger id={`${id}-provider`} className="w-full">
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                        {Object.entries(providers).map(([provider, label]) => (
                            <SelectItem key={provider} value={provider}>
                                {label}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>
            </Field>

            {pia ? (
                <>
                    <div className="grid gap-4 sm:grid-cols-2">
                        <Field id={`${id}-pia-user`} label="PIA username" help="Like p1234567.">
                            <Input
                                id={`${id}-pia-user`}
                                autoComplete="off"
                                value={value.piaUsername}
                                onChange={(event) => onChange({ piaUsername: event.target.value.trim() })}
                                placeholder={keep.piaLogin ? 'Keep the current login' : undefined}
                                required={!keep.piaLogin}
                            />
                        </Field>
                        <Field id={`${id}-pia-password`} label="PIA password">
                            <Input
                                id={`${id}-pia-password`}
                                type="password"
                                autoComplete="off"
                                value={value.piaPassword}
                                onChange={(event) => onChange({ piaPassword: event.target.value })}
                                placeholder={keep.piaLogin ? 'Keep the current login' : undefined}
                                required={!keep.piaLogin}
                                maxLength={256}
                            />
                        </Field>
                    </div>
                    <Field
                        id={`${id}-pia-token`}
                        label="Dedicated IP token (optional)"
                        help={`For your static IP. It starts with DIP; then choose "${dedicated}" as the location.`}
                    >
                        <Input
                            id={`${id}-pia-token`}
                            type="password"
                            autoComplete="off"
                            value={value.piaDedicatedToken}
                            onChange={(event) => setToken(event.target.value.trim())}
                            placeholder={keep.dedicatedToken ? 'Keep the current token' : undefined}
                        />
                    </Field>
                </>
            ) : (
                <Field
                    id={`${id}-wireguard`}
                    label="ProtonVPN WireGuard private key"
                    help={
                        <>
                            Make one at{' '}
                            <a
                                className="underline"
                                href="https://account.proton.me/u/0/vpn/WireGuard"
                                target="_blank"
                                rel="noreferrer"
                            >
                                account.proton.me
                            </a>{' '}
                            with NAT-PMP (port forwarding) on.
                        </>
                    }
                >
                    <Input
                        id={`${id}-wireguard`}
                        type="password"
                        autoComplete="off"
                        value={value.wireguardPrivateKey}
                        onChange={(event) => onChange({ wireguardPrivateKey: event.target.value.trim() })}
                        placeholder={keep.wireguardKey ? 'Keep the current key' : undefined}
                        required={!keep.wireguardKey}
                    />
                </Field>
            )}

            <Field
                id={`${id}-locations`}
                label={pia ? 'Regions' : 'Countries'}
                help={`Up to ${maxLocations}. Only ${pia ? 'regions' : 'countries'} with port forwarding, which qBittorrent needs. Gluetun picks a server in one of them.`}
            >
                <MultiCombobox
                    id={`${id}-locations`}
                    items={items}
                    value={chosen}
                    onValueChange={setLocations}
                    placeholder={pia ? 'Search a region' : 'Search a country'}
                    empty={
                        pia
                            ? 'No PIA region with port forwarding has that name.'
                            : 'No ProtonVPN country has that name.'
                    }
                />
            </Field>
        </div>
    )
}
