import assert from 'node:assert/strict'
import { test } from 'node:test'
import { backupPasswordProblem } from '../src/features/apps/backup-password-check.ts'

test('the Photos backup password is checked like the host agent checks it', () => {
    const good = 'correct horse battery'
    assert.equal(backupPasswordProblem(good, good), '')
    assert.match(backupPasswordProblem('elevenchars', 'elevenchars'), /12 to 128/)
    assert.match(backupPasswordProblem('x'.repeat(129), 'x'.repeat(129)), /12 to 128/)
    // 12 characters of 2 bytes each are 24 bytes: still fine.
    assert.equal(backupPasswordProblem('é'.repeat(12), 'é'.repeat(12)), '')
    assert.match(backupPasswordProblem('twelve chars\tok', 'twelve chars\tok'), /one line/)
    assert.match(backupPasswordProblem(good, good + ' '), /not the same/)
})
