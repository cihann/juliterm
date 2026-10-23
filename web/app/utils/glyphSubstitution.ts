// Claude Code marks its messages with ⏺ (U+23FA). No common monospace font
// has it, so each browser falls back differently: desktop browsers find a
// text glyph, but Safari on iPadOS uses Apple Color Emoji and draws a blue
// square. Drawing ● (U+25CF), which every terminal font has, gives the same
// clean bullet in the text color everywhere. Display only: tmux and the
// session's bytes are untouched. Both are 3 bytes in UTF-8, so the swap is
// in place; a character split across output frames is held back until the
// next frame shows how it ends.

const FROM = [0xe2, 0x8f, 0xba] // ⏺
const TO = [0xe2, 0x97, 0x8f] // ●

/**
 * Returns a stateful filter for one output stream. Call `reset()` when the
 * stream restarts (session switch or reconnect) to drop any held bytes.
 */
export function createGlyphSubstitution() {
  // Trailing bytes that may begin ⏺, held back until the next chunk.
  let carry: number[] = []

  function filter(chunk: Uint8Array): Uint8Array {
    let input = chunk
    if (carry.length) {
      input = new Uint8Array(carry.length + chunk.length)
      input.set(carry)
      input.set(chunk, carry.length)
      carry = []
    }

    let out: Uint8Array | null = null
    for (let i = input.indexOf(FROM[0]!); i !== -1 && i < input.length; i = input.indexOf(FROM[0]!, i + 1)) {
      // A possible start of ⏺ cut off by the end of the chunk: hold it back.
      if (i + 1 === input.length || (i + 2 === input.length && input[i + 1] === FROM[1])) {
        carry = Array.from(input.subarray(i))
        const head = out ?? input
        return head.subarray(0, i)
      }
      if (input[i + 1] === FROM[1] && input[i + 2] === FROM[2]) {
        out ??= input === chunk ? chunk.slice() : input
        out[i + 1] = TO[1]!
        out[i + 2] = TO[2]!
        i += 2
      }
    }
    return out ?? input
  }

  function reset() {
    carry = []
  }

  return { filter, reset }
}
