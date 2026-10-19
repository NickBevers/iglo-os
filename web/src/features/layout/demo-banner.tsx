// DemoBanner tells a reviewer that the demo build runs on sample data. Other
// builds render nothing.
export function DemoBanner() {
    if (import.meta.env.VITE_DEMO !== '1') {
        return null
    }

    return (
        <p className="border-b bg-brand-soft px-4 py-1.5 text-center text-xs text-foreground">
            A demo of Iglo-OS with sample data from a fake server. Buttons that change something show a message instead.
        </p>
    )
}
