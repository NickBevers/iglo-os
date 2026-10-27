import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
    ChartLineIcon,
    EllipsisVerticalIcon,
    PlayIcon,
    PowerIcon,
    RotateCwIcon,
    SquareIcon,
    TerminalIcon,
} from 'lucide-react'
import { toast } from 'sonner'

import {
    AlertDialog,
    AlertDialogAction,
    AlertDialogCancel,
    AlertDialogContent,
    AlertDialogDescription,
    AlertDialogFooter,
    AlertDialogHeader,
    AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { BlockMeter } from '@/components/block-meter'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { api, type Guest, type GuestAction } from '@/lib/api'
import { formatBytes, formatUptime, percentage } from '@/lib/format'
import { overviewQuery } from '@/lib/queries'
import { cn } from '@/lib/utils'
import { LazyUsageCharts } from './lazy-usage-charts'

const actionLabels: Record<GuestAction, string> = {
    start: 'Start',
    shutdown: 'Shut down',
    reboot: 'Reboot',
    stop: 'Force stop',
}

export function GuestList({ guests }: { guests: Guest[] }) {
    if (guests.length === 0) {
        return (
            <Card className="p-6 text-center text-sm text-muted-foreground">
                No containers or VMs yet. Install an app on the Apps page to make one.
            </Card>
        )
    }

    return (
        <Card className="gap-0 py-0">
            <div className="overflow-x-auto">
                <table className="w-full text-sm">
                    <thead>
                        <tr className="border-b text-left text-xs text-muted-foreground">
                            <th scope="col" className="py-2 pr-3 pl-4 font-medium">
                                Name
                            </th>
                            <th scope="col" className="hidden px-3 py-2 font-medium sm:table-cell">
                                ID
                            </th>
                            <th scope="col" className="px-3 py-2 font-medium">
                                CPU
                            </th>
                            <th scope="col" className="hidden px-3 py-2 font-medium md:table-cell">
                                Memory
                            </th>
                            <th scope="col" className="hidden px-3 py-2 font-medium lg:table-cell">
                                Up for
                            </th>
                            <th scope="col" className="py-2 pr-2 pl-3">
                                <span className="sr-only">Actions</span>
                            </th>
                        </tr>
                    </thead>
                    <tbody>
                        {guests.map((guest) => (
                            <GuestRow key={guest.id} guest={guest} />
                        ))}
                    </tbody>
                </table>
            </div>
        </Card>
    )
}

function Usage({ value, label, text }: { value: number; label: string; text: string }) {
    return (
        <div className="flex items-center gap-2.5">
            <BlockMeter value={value} label={label} blocks={12} size="sm" className="w-20 shrink-0" />
            <span className="whitespace-nowrap tabular-nums">{text}</span>
        </div>
    )
}

function GuestRow({ guest }: { guest: Guest }) {
    const queryClient = useQueryClient()
    const [confirmStop, setConfirmStop] = useState(false)
    const [showHistory, setShowHistory] = useState(false)
    const running = guest.status === 'running'
    const cpu = Math.round(guest.cpu * 100)

    const action = useMutation({
        mutationFn: (guestAction: GuestAction) => api.guestAction(guest, guestAction),
        onSuccess(_, guestAction) {
            toast.success(`${actionLabels[guestAction]} sent to ${guest.name}`)
            // Proxmox runs the action as a task, so give it a moment before refreshing.
            setTimeout(() => queryClient.invalidateQueries({ queryKey: overviewQuery.queryKey }), 2000)
        },
        onError(error, guestAction) {
            toast.error(`${actionLabels[guestAction]} failed for ${guest.name}`, { description: error.message })
        },
    })

    return (
        <tr className="border-b last:border-b-0 hover:bg-accent/40">
            <td className="py-2 pr-3 pl-4">
                <div className="flex min-w-0 items-center gap-2.5">
                    <span
                        className={cn(
                            'size-2 shrink-0 rounded-full',
                            running ? 'bg-success' : 'bg-muted-foreground/40',
                        )}
                        aria-hidden="true"
                    />
                    <span className="truncate font-medium">{guest.name}</span>
                    <span className="sr-only">, {guest.status}</span>
                </div>
            </td>
            <td className="hidden px-3 py-2 whitespace-nowrap text-muted-foreground sm:table-cell">
                <span className="font-mono text-xs text-foreground/80">{guest.vmid}</span>{' '}
                {guest.type === 'lxc' ? 'container' : 'VM'}
            </td>
            <td className="px-3 py-2">
                {running ? (
                    <Usage value={cpu} label={`CPU of ${guest.name}`} text={`${cpu}%`} />
                ) : (
                    <span className="text-muted-foreground">{guest.status}</span>
                )}
            </td>
            <td className="hidden px-3 py-2 md:table-cell">
                {running && (
                    <Usage
                        value={percentage(guest.mem, guest.maxMem)}
                        label={`Memory of ${guest.name}`}
                        text={`${formatBytes(guest.mem)} / ${formatBytes(guest.maxMem)}`}
                    />
                )}
            </td>
            <td className="hidden px-3 py-2 whitespace-nowrap text-muted-foreground tabular-nums lg:table-cell">
                {running ? formatUptime(guest.uptime) : null}
            </td>
            <td className="py-1 pr-2 pl-3 text-right">
                <DropdownMenu>
                    <DropdownMenuTrigger
                        render={<Button variant="ghost" size="icon" disabled={action.isPending} />}
                        aria-label={`Actions for ${guest.name}`}
                    >
                        <EllipsisVerticalIcon />
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" className="w-40">
                        <DropdownMenuItem onClick={() => setShowHistory(true)}>
                            <ChartLineIcon /> History
                        </DropdownMenuItem>
                        <DropdownMenuSeparator />
                        {running ? (
                            <>
                                {guest.type === 'lxc' && (
                                    <>
                                        <DropdownMenuItem
                                            render={<Link to="/console/$vmid" params={{ vmid: String(guest.vmid) }} />}
                                        >
                                            <TerminalIcon /> Console
                                        </DropdownMenuItem>
                                        <DropdownMenuSeparator />
                                    </>
                                )}
                                <DropdownMenuItem onClick={() => action.mutate('reboot')}>
                                    <RotateCwIcon /> Reboot
                                </DropdownMenuItem>
                                <DropdownMenuItem onClick={() => action.mutate('shutdown')}>
                                    <PowerIcon /> Shut down
                                </DropdownMenuItem>
                                <DropdownMenuSeparator />
                                <DropdownMenuItem variant="destructive" onClick={() => setConfirmStop(true)}>
                                    <SquareIcon /> Force stop
                                </DropdownMenuItem>
                            </>
                        ) : (
                            <DropdownMenuItem onClick={() => action.mutate('start')}>
                                <PlayIcon /> Start
                            </DropdownMenuItem>
                        )}
                    </DropdownMenuContent>
                </DropdownMenu>

                <Dialog open={showHistory} onOpenChange={setShowHistory}>
                    <DialogContent className="sm:max-w-2xl">
                        <DialogHeader>
                            <DialogTitle>{guest.name}</DialogTitle>
                            <DialogDescription>
                                {guest.type === 'lxc' ? 'Container' : 'VM'} {guest.vmid}. CPU is the share of its{' '}
                                {guest.maxCpu} cores.
                            </DialogDescription>
                        </DialogHeader>
                        {showHistory && <LazyUsageCharts target={guest} />}
                    </DialogContent>
                </Dialog>

                <AlertDialog open={confirmStop} onOpenChange={setConfirmStop}>
                    <AlertDialogContent>
                        <AlertDialogHeader>
                            <AlertDialogTitle>Force stop {guest.name}?</AlertDialogTitle>
                            <AlertDialogDescription>
                                This is like pulling the power cable. Unsaved data can be lost. Use "Shut down" when you
                                can.
                            </AlertDialogDescription>
                        </AlertDialogHeader>
                        <AlertDialogFooter>
                            <AlertDialogCancel>Cancel</AlertDialogCancel>
                            <AlertDialogAction
                                variant="destructive"
                                onClick={() => {
                                    setConfirmStop(false)
                                    action.mutate('stop')
                                }}
                            >
                                Force stop
                            </AlertDialogAction>
                        </AlertDialogFooter>
                    </AlertDialogContent>
                </AlertDialog>
            </td>
        </tr>
    )
}
