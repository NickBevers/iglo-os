import { TriangleAlertIcon } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'

// FormError keeps the error of a submit next to its button until the next
// try. A toast was gone after four seconds, before you could read it twice.
export function FormError({ error, title = 'That did not work' }: { error: Error | null; title?: string }) {
    if (!error) {
        return null
    }
    return (
        <Alert variant="destructive">
            <TriangleAlertIcon />
            <AlertTitle>{title}</AlertTitle>
            <AlertDescription>{error.message}</AlertDescription>
        </Alert>
    )
}
