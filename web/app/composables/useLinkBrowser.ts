import { ref } from 'vue'
import type { LinkBrowser } from '~/utils/linkBrowser'

// The browser terminal links open in, remembered per device. A Home Screen
// web app has its own storage, separate from the browser's, so it is chosen
// inside the app.
const STORAGE_KEY = 'juli:linkBrowser'
const VALID: LinkBrowser[] = ['default', 'duckduckgo', 'safari', 'chrome']

function load(): LinkBrowser {
  try {
    const v = window.localStorage.getItem(STORAGE_KEY) as LinkBrowser | null
    return v && VALID.includes(v) ? v : 'default'
  }
  catch {
    return 'default'
  }
}

const linkBrowser = ref<LinkBrowser>(typeof window === 'undefined' ? 'default' : load())

export function useLinkBrowser() {
  function setLinkBrowser(value: LinkBrowser) {
    linkBrowser.value = value
    try {
      window.localStorage.setItem(STORAGE_KEY, value)
    }
    catch {
      // Storage unavailable: the choice lasts for this page load.
    }
  }
  return { linkBrowser, setLinkBrowser }
}
