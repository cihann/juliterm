import { ref } from 'vue'

// This machine's name (e.g. "macstudio36"), so several juli instances can
// be told apart: in the tab title, the Home Screen shortcut's name and the
// sidebar. The server also serves a matching manifest and icon.
const name = ref<string | null>(null)
let loading: Promise<void> | null = null

function apply(n: string) {
  name.value = n
  document.title = `${n} · Juli`
  // iPadOS names a Home Screen shortcut from this when adding it.
  let meta = document.querySelector<HTMLMetaElement>('meta[name="apple-mobile-web-app-title"]')
  if (!meta) {
    meta = document.createElement('meta')
    meta.name = 'apple-mobile-web-app-title'
    document.head.appendChild(meta)
  }
  meta.content = n
}

// Point the Home Screen icon at an inline copy of this machine's icon.
// iPadOS downloads a linked icon on its own, without the page's cookies, so
// behind an auth proxy (Cloudflare Access) it got the login page instead
// and fell back to a letter. The page's own fetch carries the cookies, and
// an inline data: URL needs no download at all.
async function inlineTouchIcon() {
  try {
    const res = await fetch('/apple-touch-icon.png')
    if (!res.ok || !res.headers.get('content-type')?.startsWith('image/')) return
    const blob = await res.blob()
    const dataUrl = await new Promise<string>((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => resolve(reader.result as string)
      reader.onerror = () => reject(reader.error)
      reader.readAsDataURL(blob)
    })
    let link = document.querySelector<HTMLLinkElement>('link[rel="apple-touch-icon"]')
    if (!link) {
      link = document.createElement('link')
      link.rel = 'apple-touch-icon'
      document.head.appendChild(link)
    }
    link.href = dataUrl
  }
  catch {
    // Keep the plain link; it still works without an auth proxy.
  }
}

export function useIdentity() {
  if (!loading && typeof window !== 'undefined') {
    loading = fetch('/api/identity')
      .then(r => (r.ok ? r.json() : null))
      .then((id: { name?: string } | null) => { if (id?.name) apply(id.name) })
      .catch(() => {})
      .then(inlineTouchIcon)
  }
  return { name }
}
