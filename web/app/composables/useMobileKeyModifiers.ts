import { ref } from 'vue'

// Module-scoped so MobileKeyBar and TerminalView share sticky modifier state.
export const ctrlActive = ref(false)
export const altActive = ref(false)

/**
 * The byte a terminal sends for Ctrl + the given character, or null when the
 * combination has no control code (the character is then sent unchanged).
 * Ctrl+Space is NUL, Ctrl+? is DEL, and '@'..'_' / '`'..'~' map onto
 * 0x00..0x1f (so Ctrl+c and Ctrl+C are both 0x03).
 */
export function controlByte(ch: string): number | null {
  if (ch.length !== 1) return null
  if (ch === ' ') return 0x00
  if (ch === '?') return 0x7f
  const code = ch.charCodeAt(0)
  if (code >= 0x40 && code <= 0x7e) return code & 0x1f
  return null
}
