import { ref } from 'vue'

// Module-scoped so MobileKeyBar and TerminalView share touch state.

/** While on, a one-finger drag selects text instead of scrolling. */
export const selectMode = ref(false)

// TerminalView registers how to paste from the clipboard into the terminal.
let pasteHandler: (() => void) | null = null

export function setPasteHandler(fn: (() => void) | null) {
  pasteHandler = fn
}

/**
 * Paste the clipboard into the terminal. Call it synchronously from a click:
 * Safari only lets a page read the clipboard inside a user gesture.
 */
export function requestPaste() {
  pasteHandler?.()
}
