'use client'

import * as React from 'react'
import { Combobox } from '@base-ui/react/combobox'
import { cn } from 'cn'
import { CheckIcon, XIcon } from 'lucide-react'

type MultiComboboxProps = {
    id?: string
    items: readonly string[]
    value: string[]
    onValueChange: (value: string[]) => void
    placeholder?: string
    empty?: string
    'aria-label'?: string
    'aria-describedby'?: string
}

// MultiCombobox picks several items from a long list, with search. The chosen
// items show as chips that a click or Backspace removes.
function MultiCombobox({
    id,
    items,
    value,
    onValueChange,
    placeholder,
    empty = 'Nothing found.',
    ...aria
}: MultiComboboxProps) {
    const anchor = React.useRef<HTMLDivElement | null>(null)

    return (
        <Combobox.Root items={items} multiple value={value} onValueChange={onValueChange}>
            <Combobox.Chips
                ref={anchor}
                data-slot="multi-combobox"
                className="flex min-h-8 w-full flex-wrap items-center gap-1 rounded-lg border border-input bg-transparent px-1.5 py-1 text-sm transition-colors focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50 dark:bg-input/30"
            >
                <Combobox.Value>
                    {(selected: string[]) => (
                        <>
                            {selected.map((item) => (
                                <Combobox.Chip
                                    key={item}
                                    aria-label={item}
                                    className="flex items-center gap-1 rounded-md bg-secondary py-0.5 pr-0.5 pl-2 text-xs text-secondary-foreground outline-none data-highlighted:ring-2 data-highlighted:ring-ring/50"
                                >
                                    {item}
                                    <Combobox.ChipRemove
                                        aria-label={`Remove ${item}`}
                                        className="rounded-sm p-0.5 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
                                    >
                                        <XIcon className="size-3" />
                                    </Combobox.ChipRemove>
                                </Combobox.Chip>
                            ))}
                            <Combobox.Input
                                id={id}
                                placeholder={selected.length === 0 ? placeholder : undefined}
                                className="h-6 min-w-24 flex-1 bg-transparent px-1 text-base outline-none placeholder:text-muted-foreground md:text-sm"
                                {...aria}
                            />
                        </>
                    )}
                </Combobox.Value>
            </Combobox.Chips>
            <Combobox.Portal>
                <Combobox.Positioner anchor={anchor} sideOffset={4} className="isolate z-50">
                    <Combobox.Popup
                        className={cn(
                            'max-h-[min(var(--available-height),18rem)] w-(--anchor-width) overflow-y-auto rounded-lg bg-popover p-1 text-popover-foreground shadow-md ring-1 ring-foreground/10',
                            'origin-(--transform-origin) duration-100 data-open:animate-in data-open:fade-in-0 data-closed:animate-out data-closed:fade-out-0',
                        )}
                    >
                        <Combobox.Empty className="px-2 py-1.5 text-sm text-muted-foreground empty:hidden">
                            {empty}
                        </Combobox.Empty>
                        <Combobox.List>
                            {(item: string) => (
                                <Combobox.Item
                                    key={item}
                                    value={item}
                                    className="relative flex cursor-default items-center rounded-md py-1 pr-8 pl-1.5 text-sm outline-hidden select-none data-highlighted:bg-accent data-highlighted:text-accent-foreground"
                                >
                                    {item}
                                    <Combobox.ItemIndicator className="absolute right-2 flex size-4 items-center justify-center">
                                        <CheckIcon className="size-4" />
                                    </Combobox.ItemIndicator>
                                </Combobox.Item>
                            )}
                        </Combobox.List>
                    </Combobox.Popup>
                </Combobox.Positioner>
            </Combobox.Portal>
        </Combobox.Root>
    )
}

export { MultiCombobox }
