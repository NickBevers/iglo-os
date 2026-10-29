// backupPasswordProblem is what is wrong with the Photos backup password, or
// ''. The host agent and the installer check the same length.
export function backupPasswordProblem(password: string, confirm: string) {
    if ([...password].length < 12 || new TextEncoder().encode(password).length > 128) {
        return 'Use 12 to 128 characters.'
    }
    if ([...password].some((c) => c.charCodeAt(0) < 32 || c.charCodeAt(0) === 127)) {
        return 'Use one line, without tabs.'
    }
    if (password !== confirm) {
        return 'The two passwords are not the same.'
    }
    return ''
}
