import { MonitorIcon, MoonIcon, SunIcon, type LucideIcon } from 'lucide-react'

import { useTheme } from '@/components/theme-provider'
import { cn } from '@/lib/utils'

const choices: { theme: 'light' | 'dark' | 'system'; label: string; icon: LucideIcon }[] = [
    { theme: 'light', label: 'Light', icon: SunIcon },
    { theme: 'dark', label: 'Dark', icon: MoonIcon },
    { theme: 'system', label: 'Like this device', icon: MonitorIcon },
]

export function ThemeSwitch({ className }: { className?: string }) {
    const { theme, setTheme } = useTheme()

    return (
        <div
            role="radiogroup"
            aria-label="Theme"
            className={cn('grid grid-cols-3 gap-0.5 rounded-md bg-sidebar-accent p-0.5', className)}
        >
            {choices.map((choice) => (
                <button
                    key={choice.theme}
                    type="button"
                    role="radio"
                    aria-checked={theme === choice.theme}
                    aria-label={choice.label}
                    title={choice.label}
                    onClick={() => setTheme(choice.theme)}
                    className="flex h-6 items-center justify-center rounded-[calc(var(--radius)-2px)] text-sidebar-foreground/60 transition-colors outline-none hover:text-sidebar-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring aria-checked:bg-background aria-checked:text-sidebar-foreground aria-checked:shadow-xs"
                >
                    <choice.icon className="size-3.5" />
                </button>
            ))}
        </div>
    )
}
