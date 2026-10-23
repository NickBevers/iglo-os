import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Gamepad2Icon, SearchIcon } from 'lucide-react'
import { cn } from 'cn'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { api, coverURL, type GameSystem } from '@/lib/api'

type Game = GameSystem['games'][number] & { system: string; systemLabel: string }

// GameLibrary shows the games on the Games share with their covers. A game
// without a cover gets a placeholder with its title.
export function GameLibrary() {
    const query = useQuery({ queryKey: ['gaming-games'], queryFn: api.gamingGames, staleTime: 60_000 })
    const [system, setSystem] = useState('all')
    const [search, setSearch] = useState('')

    const games = useMemo(() => {
        const all: Game[] = (query.data?.systems ?? []).flatMap((s) =>
            s.games.map((game) => ({ ...game, system: s.id, systemLabel: s.label })),
        )
        const words = search.trim().toLowerCase()
        return all.filter(
            (game) =>
                (system === 'all' || game.system === system) && (!words || game.title.toLowerCase().includes(words)),
        )
    }, [query.data, system, search])

    const systems = query.data?.systems ?? []
    const total = systems.reduce((sum, s) => sum + s.games.length, 0)

    return (
        <Card>
            <CardHeader>
                <CardTitle>Library</CardTitle>
                <CardDescription>
                    Your own covers come from covers/&lt;system&gt;/ on the Games share, named like the game file. Other
                    box art comes from libretro, matched by file name.
                </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
                {query.error && (
                    <p role="alert" className="text-sm text-destructive">
                        {query.error.message}
                    </p>
                )}
                {query.isPending && <p className="text-sm text-muted-foreground">Loading the games…</p>}
                {query.data && (
                    <>
                        <div className="flex flex-wrap gap-2" role="group" aria-label="System">
                            <Button
                                size="sm"
                                variant={system === 'all' ? 'default' : 'outline'}
                                aria-pressed={system === 'all'}
                                onClick={() => setSystem('all')}
                            >
                                All <span className="tabular-nums">{total}</span>
                            </Button>
                            {systems.map((s) => (
                                <Button
                                    key={s.id}
                                    size="sm"
                                    variant={system === s.id ? 'default' : 'outline'}
                                    aria-pressed={system === s.id}
                                    onClick={() => setSystem(s.id)}
                                >
                                    {s.label} <span className="tabular-nums">{s.games.length}</span>
                                </Button>
                            ))}
                        </div>
                        <label className="relative block max-w-sm">
                            <span className="sr-only">Search games</span>
                            <SearchIcon
                                className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground"
                                aria-hidden="true"
                            />
                            <Input
                                type="search"
                                className="pl-9"
                                placeholder="Search games"
                                value={search}
                                onChange={(e) => setSearch(e.target.value)}
                            />
                        </label>
                        {games.length === 0 ? (
                            <p className="text-sm text-muted-foreground">
                                {total === 0
                                    ? 'No games yet. Put them in roms/<system>/ on the Games share.'
                                    : 'No games match.'}
                            </p>
                        ) : (
                            <ul className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5 xl:grid-cols-6">
                                {games.map((game) => (
                                    <GameTile key={`${game.system}/${game.name}`} game={game} />
                                ))}
                            </ul>
                        )}
                    </>
                )}
            </CardContent>
        </Card>
    )
}

function GameTile({ game }: { game: Game }) {
    const [missing, setMissing] = useState(false)
    return (
        <li className="flex min-w-0 flex-col gap-2">
            <div
                className={cn(
                    'flex aspect-[3/4] items-center justify-center overflow-hidden rounded-md border bg-muted',
                    missing && 'p-3',
                )}
            >
                {missing ? (
                    <div className="flex flex-col items-center gap-2 text-center text-xs text-muted-foreground">
                        <Gamepad2Icon className="size-6" aria-hidden="true" />
                        <span className="line-clamp-3">{game.title}</span>
                    </div>
                ) : (
                    <img
                        src={coverURL(game.system, game)}
                        alt=""
                        loading="lazy"
                        decoding="async"
                        className="size-full object-contain"
                        onError={() => setMissing(true)}
                    />
                )}
            </div>
            <div className="flex min-w-0 flex-col">
                <span className="truncate text-sm font-medium" title={game.name}>
                    {game.title}
                </span>
                <span className="text-xs text-muted-foreground">{game.systemLabel}</span>
            </div>
        </li>
    )
}
