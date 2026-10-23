import { onMounted, onBeforeUnmount } from 'vue'

// On iPadOS, the on-screen keyboard (or the shortcut bar shown with a
// hardware keyboard) covers the bottom of the page without resizing it, and
// Safari scrolls the whole page up to keep the focused field in view. That
// dragged the key bar up over the terminal. Instead, size the app to the
// visible area above the keyboard (the visual viewport) through
// --app-height, and undo any page scroll so the layout stays put.
//
// iPadOS doesn't reliably report the viewport growing back when the keyboard
// collapses to its small shortcut bar, notably in Home Screen web apps, which
// left the app at keyboard height above an empty band. So the height is also
// re-read after focus changes, rotation and returning to the app (again once
// the keyboard animation has settled), and once a second while visible.

const SETTLE_DELAYS_MS = [100, 350, 800]
const POLL_MS = 1000

let lastHeight = ''

function sync() {
  const vv = window.visualViewport
  if (vv) {
    const height = `${Math.round(vv.height)}px`
    if (height !== lastHeight) {
      lastHeight = height
      document.documentElement.style.setProperty('--app-height', height)
    }
  }
  if (window.scrollX !== 0 || window.scrollY !== 0) window.scrollTo(0, 0)
}

let settleTimers: Array<ReturnType<typeof setTimeout>> = []

// Sync now and again as the keyboard animation finishes.
function syncAndSettle() {
  sync()
  for (const t of settleTimers) clearTimeout(t)
  settleTimers = SETTLE_DELAYS_MS.map(ms => setTimeout(sync, ms))
}

export function useVisibleViewport() {
  let poll: ReturnType<typeof setInterval> | null = null

  function onVisibilityChange() {
    if (document.visibilityState === 'visible') syncAndSettle()
  }

  onMounted(() => {
    lastHeight = ''
    sync()
    window.visualViewport?.addEventListener('resize', syncAndSettle)
    window.visualViewport?.addEventListener('scroll', sync)
    window.addEventListener('scroll', sync, { passive: true })
    window.addEventListener('resize', syncAndSettle)
    window.addEventListener('orientationchange', syncAndSettle)
    window.addEventListener('pageshow', syncAndSettle)
    document.addEventListener('focusin', syncAndSettle)
    document.addEventListener('focusout', syncAndSettle)
    document.addEventListener('visibilitychange', onVisibilityChange)
    poll = setInterval(() => {
      if (document.visibilityState === 'visible') sync()
    }, POLL_MS)
  })

  onBeforeUnmount(() => {
    window.visualViewport?.removeEventListener('resize', syncAndSettle)
    window.visualViewport?.removeEventListener('scroll', sync)
    window.removeEventListener('scroll', sync)
    window.removeEventListener('resize', syncAndSettle)
    window.removeEventListener('orientationchange', syncAndSettle)
    window.removeEventListener('pageshow', syncAndSettle)
    document.removeEventListener('focusin', syncAndSettle)
    document.removeEventListener('focusout', syncAndSettle)
    document.removeEventListener('visibilitychange', onVisibilityChange)
    if (poll) clearInterval(poll)
    for (const t of settleTimers) clearTimeout(t)
    settleTimers = []
    lastHeight = ''
    document.documentElement.style.removeProperty('--app-height')
  })
}
