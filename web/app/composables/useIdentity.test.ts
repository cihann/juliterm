import { describe, expect, it, vi, afterEach } from 'vitest'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.resetModules()
  document.head.querySelectorAll('link[rel="apple-touch-icon"], meta[name="apple-mobile-web-app-title"]').forEach(e => e.remove())
})

async function flush() {
  for (let i = 0; i < 10; i++) await new Promise(r => setTimeout(r, 0))
}

describe('useIdentity', () => {
  it('names the page and inlines the icon fetched with the page cookies', async () => {
    const png = new Blob([new Uint8Array([0x89, 0x50, 0x4e, 0x47])], { type: 'image/png' })
    vi.stubGlobal('fetch', vi.fn(async (url: string) => url === '/api/identity'
      ? { ok: true, json: async () => ({ name: 'macstudio36' }) }
      : { ok: true, headers: new Headers({ 'content-type': 'image/png' }), blob: async () => png }))
    const { useIdentity } = await import('./useIdentity')

    const { name } = useIdentity()
    await flush()

    expect(name.value).toBe('macstudio36')
    expect(document.title).toBe('macstudio36 · Juli')
    expect(document.querySelector<HTMLMetaElement>('meta[name="apple-mobile-web-app-title"]')?.content).toBe('macstudio36')
    expect(document.querySelector<HTMLLinkElement>('link[rel="apple-touch-icon"]')?.href).toMatch(/^data:image\/png;base64,/)
  })

  it('keeps no inline icon when the icon request returns a login page', async () => {
    vi.stubGlobal('fetch', vi.fn(async (url: string) => url === '/api/identity'
      ? { ok: true, json: async () => ({ name: 'macstudio84' }) }
      : { ok: true, headers: new Headers({ 'content-type': 'text/html' }), blob: async () => new Blob(['<html>']) }))
    const { useIdentity } = await import('./useIdentity')

    useIdentity()
    await flush()

    expect(document.querySelector('link[rel="apple-touch-icon"]')).toBeNull()
  })
})
