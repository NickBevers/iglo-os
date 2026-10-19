import type { Health } from './api'

// healthProblems lists what needs attention: services that are down, and the
// problems of disks, pools, shares and storage.
export function healthProblems(health: Health): string[] {
    return [
        ...health.services
            .filter((service) => service.status === 'down')
            .map((service) => `${service.name} is down: ${service.error}`),
        ...[...health.disks, ...health.pools, ...health.shares, ...health.storage].flatMap((item) =>
            item.problem ? [item.problem] : [],
        ),
        ...health.errors,
    ]
}
