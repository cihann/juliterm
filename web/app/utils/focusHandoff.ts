// iOS/iPadOS Safari only lets a page focus a text field (and raise the
// on-screen keyboard) synchronously inside a user gesture. The terminal's
// textarea doesn't exist yet when a session is tapped, so the tap focuses
// this off-screen proxy instead; once the terminal is ready, moving focus from
// the proxy to it keeps the keyboard up. On other platforms it's harmless.

/**
 * Mark a text field as terminal input rather than a form field, so mobile
 * browsers don't offer autofill (Chrome on iPadOS shows a passwords / cards /
 * addresses bar for form fields) or autocorrect for it.
 */
export function markAsTerminalInput(el: HTMLTextAreaElement) {
  el.setAttribute('autocomplete', 'off')
  el.setAttribute('autocorrect', 'off')
  el.setAttribute('autocapitalize', 'off')
  el.setAttribute('spellcheck', 'false')
  // Hints password managers honor to skip a field.
  el.setAttribute('data-1p-ignore', '')
  el.setAttribute('data-lpignore', 'true')
  el.setAttribute('data-form-type', 'other')
}

let proxy: HTMLTextAreaElement | null = null

function getProxy(): HTMLTextAreaElement {
  if (proxy && proxy.isConnected) return proxy
  proxy = document.createElement('textarea')
  proxy.setAttribute('aria-hidden', 'true')
  proxy.tabIndex = -1
  markAsTerminalInput(proxy)
  // Off-screen but focusable; font-size 16px stops iOS from zooming the page.
  Object.assign(proxy.style, {
    position: 'fixed', top: '0', left: '-1000px', width: '1px', height: '1px',
    opacity: '0', fontSize: '16px', pointerEvents: 'none',
  })
  document.body.appendChild(proxy)
  return proxy
}

/** Call synchronously from the tap/click that opens a session. */
export function holdFocusForTerminal() {
  if (typeof document === 'undefined') return
  getProxy().focus({ preventScroll: true })
}

/** True while the proxy still holds focus waiting for the terminal. */
export function focusIsHeldForTerminal() {
  return proxy !== null && document.activeElement === proxy
}
