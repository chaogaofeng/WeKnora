import assert from 'node:assert/strict'
import { test } from 'node:test'
import { baseCompile } from '@intlify/message-compiler'
import { opsMessages } from './index'
test('knowledge operations messages have identical keys and compile for every supported locale', () => {
  const reference = Object.keys(opsMessages['en-US']).sort()
  for (const [locale, messages] of Object.entries(opsMessages)) {
    assert.deepEqual(Object.keys(messages).sort(), reference, locale)
    for (const [key, value] of Object.entries(messages)) {
      const errors: string[] = []
      baseCompile(value, { onError: error => errors.push(error.message) })
      assert.deepEqual(errors, [], `${locale}.${key}`)
    }
  }
})
