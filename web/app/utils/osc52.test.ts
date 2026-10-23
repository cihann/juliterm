import { describe, expect, it } from 'vitest'
import { OSC52_MAX_BYTES, parseOsc52 } from './osc52'

function b64(text: string) {
  const bytes = new TextEncoder().encode(text)
  let binary = ''
  for (const b of bytes) binary += String.fromCharCode(b)
  return btoa(binary)
}

describe('parseOsc52', () => {
  it('decodes the clipboard text', () => {
    expect(parseOsc52(`c;${b64('hello world')}`)).toBe('hello world')
  })

  it('accepts an empty or multi-selection parameter', () => {
    expect(parseOsc52(`;${b64('a')}`)).toBe('a')
    expect(parseOsc52(`cp;${b64('b')}`)).toBe('b')
  })

  it('decodes UTF-8', () => {
    expect(parseOsc52(`c;${b64('café ● 日本')}`)).toBe('café ● 日本')
  })

  it('ignores queries and clears', () => {
    expect(parseOsc52('c;?')).toBeNull()
    expect(parseOsc52('c;')).toBeNull()
  })

  it('rejects malformed payloads', () => {
    expect(parseOsc52('no-separator')).toBeNull()
    expect(parseOsc52('c;***not base64***')).toBeNull()
    // Valid base64 but not UTF-8 (a lone continuation byte).
    expect(parseOsc52(`c;${btoa('\x80')}`)).toBeNull()
  })

  it('rejects payloads over the size limit', () => {
    const big = 'A'.repeat(Math.ceil(OSC52_MAX_BYTES / 3) * 4 + 8)
    expect(parseOsc52(`c;${big}`)).toBeNull()
  })
})
