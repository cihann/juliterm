// Where terminal links open. A site added to the iPadOS Home Screen runs as a
// standalone web app, and its links open in an in-app browser sheet. The
// only way out to a real browser app is that app's URL scheme.

export type LinkBrowser = 'default' | 'duckduckgo' | 'safari' | 'chrome'

export const LINK_BROWSERS: Array<{ value: LinkBrowser, label: string }> = [
  { value: 'default', label: 'In-app' },
  { value: 'duckduckgo', label: 'DuckDuckGo' },
  { value: 'safari', label: 'Safari' },
  { value: 'chrome', label: 'Chrome' },
]

/**
 * The URL that opens `uri` in `browser`, or null to open it normally.
 * Only http(s) links are rewritten.
 */
export function browserUrl(uri: string, browser: LinkBrowser): string | null {
  const m = /^(https?):\/\/(.*)$/i.exec(uri)
  if (!m || browser === 'default') return null
  const [, scheme, rest] = m
  switch (browser) {
    // DuckDuckGo strips the `ddgQuickLink://` prefix and loads the rest.
    case 'duckduckgo': return `ddgQuickLink://${uri}`
    case 'safari': return `x-safari-${scheme!.toLowerCase()}://${rest}`
    case 'chrome': return `${scheme!.toLowerCase() === 'https' ? 'googlechromes' : 'googlechrome'}://${rest}`
  }
}

/** True when running as an installed (Home Screen) web app. */
export function isStandaloneApp(): boolean {
  if (typeof window === 'undefined') return false
  return window.matchMedia?.('(display-mode: standalone)').matches
    || (navigator as Navigator & { standalone?: boolean }).standalone === true
}
