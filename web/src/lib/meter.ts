// meterTone is the state of a usage: amber from 75%, red from 90%.
export function meterTone(value: number): 'normal' | 'warning' | 'critical' {
    if (value >= 90) {
        return 'critical'
    }
    return value >= 75 ? 'warning' : 'normal'
}
