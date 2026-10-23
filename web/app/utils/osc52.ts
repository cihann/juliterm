// OSC 52 is how terminal apps set the clipboard: `ESC ] 52 ; Pc ; Pd BEL`,
// where Pc names the selection(s) (c, p, s, 0-7, or empty) and Pd is the text
// in base64. tmux (with set-clipboard on) and Claude Code use it when you copy
// a selection. A Pd of `?` asks the terminal to report the clipboard back;
// juli never answers that, so a page can't read the user's clipboard.

/** Largest clipboard text accepted from an app, in decoded bytes. */
export const OSC52_MAX_BYTES = 1 << 20

/**
 * Text an OSC 52 sequence asks to copy, or null for queries, clears,
 * malformed or oversized payloads. `data` is everything after `52;`.
 */
export function parseOsc52(data: string): string | null {
  const sep = data.indexOf(';')
  if (sep === -1) return null
  const payload = data.slice(sep + 1)
  if (payload === '' || payload === '?') return null
  // Base64 length is ~4/3 of the bytes; reject early before decoding.
  if (payload.length > Math.ceil(OSC52_MAX_BYTES / 3) * 4 + 4) return null

  let binary: string
  try {
    binary = atob(payload)
  } catch {
    return null
  }
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
  try {
    return new TextDecoder('utf-8', { fatal: true }).decode(bytes)
  } catch {
    return null
  }
}
