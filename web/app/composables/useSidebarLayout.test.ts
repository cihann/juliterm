import { describe, expect, it, beforeEach } from 'vitest'
import { nextTick } from 'vue'
import {
  SIDEBAR_DEFAULT_WIDTH,
  SIDEBAR_MAX_WIDTH,
  SIDEBAR_MIN_WIDTH,
  useSidebarLayout,
} from './useSidebarLayout'

// In-memory stand-in for localStorage: newer Node versions ship their own
// global localStorage, which shadows happy-dom's and is unusable without a
// --localstorage-file.
function memoryStorage() {
  const items = new Map<string, string>()
  return {
    getItem: (key: string) => items.get(key) ?? null,
    setItem: (key: string, value: string) => { items.set(key, value) },
    removeItem: (key: string) => { items.delete(key) },
    clear: () => items.clear(),
  }
}

function useStorage(storage: object) {
  Object.defineProperty(window, 'localStorage', { value: storage, configurable: true })
}

describe('useSidebarLayout', () => {
  beforeEach(() => {
    useStorage(memoryStorage())
  })

  it('starts at the default width, expanded', () => {
    const { width, collapsed } = useSidebarLayout()
    expect(width.value).toBe(SIDEBAR_DEFAULT_WIDTH)
    expect(collapsed.value).toBe(false)
  })

  it('clamps dragged widths to the allowed range', () => {
    const layout = useSidebarLayout()
    layout.dragTo(130)
    expect(layout.width.value).toBe(SIDEBAR_MIN_WIDTH)
    layout.dragTo(2000)
    expect(layout.width.value).toBe(SIDEBAR_MAX_WIDTH)
    layout.dragTo(300.4)
    expect(layout.width.value).toBe(300)
  })

  it('collapses when dragged past the threshold and keeps its width', () => {
    const layout = useSidebarLayout()
    layout.dragTo(320)
    layout.dragTo(40)
    expect(layout.collapsed.value).toBe(true)
    expect(layout.width.value).toBe(320)
    layout.dragTo(200)
    expect(layout.collapsed.value).toBe(false)
    expect(layout.width.value).toBe(200)
  })

  it('toggles and resets', () => {
    const layout = useSidebarLayout()
    layout.toggle()
    expect(layout.collapsed.value).toBe(true)
    layout.toggle()
    expect(layout.collapsed.value).toBe(false)
    layout.dragTo(400)
    layout.resetWidth()
    expect(layout.width.value).toBe(SIDEBAR_DEFAULT_WIDTH)
  })

  it('remembers the layout across page loads', async () => {
    const first = useSidebarLayout()
    first.dragTo(300)
    first.toggle()
    await nextTick()

    const second = useSidebarLayout()
    expect(second.width.value).toBe(300)
    expect(second.collapsed.value).toBe(true)
  })

  it('ignores corrupt or out-of-range stored values', () => {
    window.localStorage.setItem('juli:sidebar', '{not json')
    expect(useSidebarLayout().width.value).toBe(SIDEBAR_DEFAULT_WIDTH)

    window.localStorage.setItem('juli:sidebar', JSON.stringify({ width: 9999, collapsed: 'yes' }))
    const layout = useSidebarLayout()
    expect(layout.width.value).toBe(SIDEBAR_MAX_WIDTH)
    expect(layout.collapsed.value).toBe(false)
  })

  it('still works when storage throws', async () => {
    const blocked = () => { throw new Error('blocked') }
    useStorage({ getItem: blocked, setItem: blocked })
    const layout = useSidebarLayout()
    expect(layout.width.value).toBe(SIDEBAR_DEFAULT_WIDTH)
    layout.toggle()
    await nextTick()
    expect(layout.collapsed.value).toBe(true)
  })
})
