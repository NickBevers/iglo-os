import { useState, type ReactNode } from 'react'

import {
    AlertDialog,
    AlertDialogAction,
    AlertDialogCancel,
    AlertDialogContent,
    AlertDialogDescription,
    AlertDialogFooter,
    AlertDialogHeader,
    AlertDialogTitle,
    AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'

type ConfirmButtonProps = {
    // The button that opens the question, and what the confirm button says.
    children: ReactNode
    title: string
    description: ReactNode
    confirm: string
    onConfirm: () => void
    disabled?: boolean
    variant?: 'default' | 'outline' | 'destructive'
}

// ConfirmButton asks before an action that loses something, like the answers
// of a setup or an unsaved game.
export function ConfirmButton({
    children,
    title,
    description,
    confirm,
    onConfirm,
    disabled,
    variant = 'outline',
}: ConfirmButtonProps) {
    const [open, setOpen] = useState(false)

    return (
        <AlertDialog open={open} onOpenChange={setOpen}>
            <AlertDialogTrigger render={<Button variant={variant} disabled={disabled} />}>
                {children}
            </AlertDialogTrigger>
            <AlertDialogContent>
                <AlertDialogHeader>
                    <AlertDialogTitle>{title}</AlertDialogTitle>
                    <AlertDialogDescription>{description}</AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                    <AlertDialogCancel>Cancel</AlertDialogCancel>
                    <AlertDialogAction
                        variant="destructive"
                        onClick={() => {
                            setOpen(false)
                            onConfirm()
                        }}
                    >
                        {confirm}
                    </AlertDialogAction>
                </AlertDialogFooter>
            </AlertDialogContent>
        </AlertDialog>
    )
}
