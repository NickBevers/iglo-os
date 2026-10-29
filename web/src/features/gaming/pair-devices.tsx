import { useState, type SubmitEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { api, type GamingPairing } from '@/lib/api'
import { isLive } from '@/lib/live'

// PairDevices pairs a Moonlight device with the PIN that it shows. Wolf says
// when a device asks, so the list refreshes by itself; the poll is a fallback,
// and each check asks Wolf in its container.
export function PairDevices() {
    const query = useQuery({
        queryKey: ['gaming', 'pairing'],
        queryFn: api.gamingPairings,
        refetchInterval: () => (isLive() ? 60000 : 10000),
    })

    return (
        <Card>
            <CardHeader>
                <CardTitle>Pair a device</CardTitle>
                <CardDescription>
                    Add this server as a host in Moonlight. Moonlight then shows a PIN and waits here. Pair only devices
                    you know.
                </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
                {query.error && <p className="text-sm text-destructive">{query.error.message}</p>}
                {query.data?.length === 0 && (
                    <p className="text-sm text-muted-foreground">No device waits to pair right now.</p>
                )}
                {query.data?.map((pairing) => (
                    <PairForm key={pairing.id} pairing={pairing} />
                ))}
            </CardContent>
        </Card>
    )
}

function PairForm({ pairing }: { pairing: GamingPairing }) {
    const client = useQueryClient()
    const [pin, setPin] = useState('')
    const pair = useMutation({
        mutationFn: () => api.pairGamingDevice(pairing.id, pin),
        onSuccess: () => {
            client.invalidateQueries({ queryKey: ['gaming'] })
            toast.success(`Paired ${pairing.clientIp}`)
        },
    })
    function submit(event: SubmitEvent) {
        event.preventDefault()
        pair.mutate()
    }
    const id = `pair-${pairing.id}`

    return (
        <form onSubmit={submit} className="flex flex-col gap-2">
            <Label htmlFor={id}>PIN of the device at {pairing.clientIp}</Label>
            <div className="flex gap-2">
                <Input
                    id={id}
                    required
                    inputMode="numeric"
                    autoComplete="off"
                    pattern="[0-9]{4}"
                    maxLength={4}
                    className="w-28"
                    value={pin}
                    onChange={(event) => setPin(event.target.value.replace(/\D/g, ''))}
                />
                <Button type="submit" disabled={pair.isPending || pin.length !== 4}>
                    Pair
                </Button>
            </div>
            {pair.error && (
                <p role="alert" className="text-sm text-destructive">
                    The device did not pair: {pair.error.message}
                </p>
            )}
        </form>
    )
}
