import { describe, expect, it } from 'vitest'
import { controlByte } from './useMobileKeyModifiers'

describe('controlByte', () => {
  it('maps letters regardless of case', () => {
    expect(controlByte('c')).toBe(0x03)
    expect(controlByte('C')).toBe(0x03)
    expect(controlByte('a')).toBe(0x01)
    expect(controlByte('z')).toBe(0x1a)
  })

  it('maps the punctuation control codes', () => {
    expect(controlByte('@')).toBe(0x00)
    expect(controlByte('[')).toBe(0x1b)
    expect(controlByte('\\')).toBe(0x1c)
    expect(controlByte(']')).toBe(0x1d)
    expect(controlByte('^')).toBe(0x1e)
    expect(controlByte('_')).toBe(0x1f)
    expect(controlByte(' ')).toBe(0x00)
    expect(controlByte('?')).toBe(0x7f)
  })

  it('returns null for characters without a control code', () => {
    expect(controlByte('1')).toBeNull()
    expect(controlByte('/')).toBeNull()
    expect(controlByte('é')).toBeNull()
    expect(controlByte('ab')).toBeNull()
  })
})
