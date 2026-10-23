import { describe, expect, it } from 'vitest'
import { createGlyphSubstitution } from './glyphSubstitution'

const enc = new TextEncoder()
const dec = (b: Uint8Array) => new TextDecoder().decode(b)

describe('createGlyphSubstitution', () => {
  it('draws ⏺ as ●', () => {
    const { filter } = createGlyphSubstitution()
    expect(dec(filter(enc.encode('⏺ Done ⏺ again')))).toBe('● Done ● again')
  })

  it('passes other text through as the same buffer', () => {
    const { filter } = createGlyphSubstitution()
    const chunk = enc.encode('plain ─ │ ● ⚠ ⏸ text')
    expect(filter(chunk)).toBe(chunk)
  })

  it('does not modify the caller\'s buffer', () => {
    const { filter } = createGlyphSubstitution()
    const chunk = enc.encode('⏺')
    filter(chunk)
    expect(dec(chunk)).toBe('⏺')
  })

  it('handles ⏺ split at every position across two chunks', () => {
    const bytes = enc.encode('a⏺b')
    for (let split = 1; split < bytes.length; split++) {
      const { filter } = createGlyphSubstitution()
      const joined = new Uint8Array([...filter(bytes.slice(0, split)), ...filter(bytes.slice(split))])
      expect(dec(joined)).toBe('a●b')
    }
  })

  it('releases a held lead byte that starts another character', () => {
    const { filter } = createGlyphSubstitution()
    const bytes = enc.encode('x─y') // ─ is E2 94 80
    const joined = new Uint8Array([...filter(bytes.slice(0, 2)), ...filter(bytes.slice(2))])
    expect(dec(joined)).toBe('x─y')
  })

  it('drops held bytes on reset', () => {
    const { filter, reset } = createGlyphSubstitution()
    filter(Uint8Array.from([0x41, 0xe2]))
    reset()
    expect(dec(filter(enc.encode('ok')))).toBe('ok')
  })
})
