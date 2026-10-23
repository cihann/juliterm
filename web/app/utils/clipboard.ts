// Clipboard writes. Safari (including every browser on iPadOS) only allows
// them during a user gesture, so callers outside one must expect `false` and
// offer a tap that calls this again.

/** Copy text to the clipboard; resolves false when the browser refuses. */
export async function copyText(text: string): Promise<boolean> {
  if (navigator.clipboard?.writeText && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      // Fall through: no gesture, permission denied, or unfocused document.
    }
  }
  return copyWithExecCommand(text)
}

// Legacy path for insecure contexts (plain http on a LAN address), where the
// async clipboard API doesn't exist. Only works inside a user gesture.
function copyWithExecCommand(text: string): boolean {
  const active = document.activeElement as HTMLElement | null
  const ta = document.createElement('textarea')
  ta.value = text
  ta.setAttribute('readonly', '')
  Object.assign(ta.style, { position: 'fixed', top: '0', left: '-1000px', opacity: '0', fontSize: '16px' })
  document.body.appendChild(ta)
  ta.select()
  ta.setSelectionRange(0, text.length)
  let ok = false
  try {
    ok = document.execCommand('copy')
  } catch {
    ok = false
  }
  ta.remove()
  active?.focus({ preventScroll: true })
  return ok
}

/** Read clipboard text; must be called inside a user gesture on Safari. */
export async function readText(): Promise<string | null> {
  if (!navigator.clipboard?.readText || !window.isSecureContext) return null
  try {
    return await navigator.clipboard.readText()
  } catch {
    return null
  }
}
