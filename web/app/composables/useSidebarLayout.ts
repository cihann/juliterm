import { ref, readonly, watch } from 'vue'

export const SIDEBAR_DEFAULT_WIDTH = 240
export const SIDEBAR_MIN_WIDTH = 160
export const SIDEBAR_MAX_WIDTH = 480
// Dragging the handle this far left hides the sidebar instead of shrinking it.
export const SIDEBAR_COLLAPSE_WIDTH = 100

const STORAGE_KEY = 'juli:sidebar'

interface StoredLayout {
  width: number
  collapsed: boolean
}

function clampWidth(width: number) {
  return Math.min(SIDEBAR_MAX_WIDTH, Math.max(SIDEBAR_MIN_WIDTH, Math.round(width)))
}

function load(): StoredLayout {
  const fallback = { width: SIDEBAR_DEFAULT_WIDTH, collapsed: false }
  if (typeof window === 'undefined') return fallback
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY)
    if (!raw) return fallback
    const parsed = JSON.parse(raw)
    return {
      width: typeof parsed.width === 'number' ? clampWidth(parsed.width) : fallback.width,
      collapsed: parsed.collapsed === true,
    }
  }
  catch {
    return fallback
  }
}

/**
 * Sidebar width and collapsed state, remembered per browser in localStorage.
 * Storage is best-effort: a private window or blocked storage just falls back
 * to the defaults.
 */
export function useSidebarLayout() {
  const initial = load()
  const width = ref(initial.width)
  const collapsed = ref(initial.collapsed)

  watch([width, collapsed], () => {
    try {
      window.localStorage.setItem(STORAGE_KEY, JSON.stringify({ width: width.value, collapsed: collapsed.value }))
    }
    catch {
      // Storage unavailable; the layout still works for this page load.
    }
  })

  // Width the pointer asks for while dragging the handle. Below the collapse
  // threshold the sidebar hides and keeps its last width for when it reopens.
  function dragTo(requested: number) {
    if (requested < SIDEBAR_COLLAPSE_WIDTH) {
      collapsed.value = true
      return
    }
    collapsed.value = false
    width.value = clampWidth(requested)
  }

  function toggle() {
    collapsed.value = !collapsed.value
  }

  function resetWidth() {
    width.value = SIDEBAR_DEFAULT_WIDTH
  }

  return {
    width: readonly(width),
    collapsed: readonly(collapsed),
    dragTo,
    toggle,
    resetWidth,
  }
}
