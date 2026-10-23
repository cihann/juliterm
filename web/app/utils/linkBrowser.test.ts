import { describe, expect, it } from 'vitest'
import { browserUrl } from './linkBrowser'

describe('browserUrl', () => {
  const uri = 'https://gitlab.com/gprovencher/dotfiles/-/merge_requests/16?x=1#c'

  it('leaves links alone for the default browser', () => {
    expect(browserUrl(uri, 'default')).toBeNull()
  })

  it('builds a DuckDuckGo quick link', () => {
    expect(browserUrl(uri, 'duckduckgo')).toBe(`ddgQuickLink://${uri}`)
  })

  it('builds Safari and Chrome links for https and http', () => {
    expect(browserUrl(uri, 'safari')).toBe('x-safari-https://gitlab.com/gprovencher/dotfiles/-/merge_requests/16?x=1#c')
    expect(browserUrl('http://example.com/a', 'safari')).toBe('x-safari-http://example.com/a')
    expect(browserUrl(uri, 'chrome')).toBe('googlechromes://gitlab.com/gprovencher/dotfiles/-/merge_requests/16?x=1#c')
    expect(browserUrl('http://example.com/a', 'chrome')).toBe('googlechrome://example.com/a')
  })

  it('does not rewrite other schemes', () => {
    expect(browserUrl('mailto:a@b.c', 'duckduckgo')).toBeNull()
    expect(browserUrl('file:///etc/hosts', 'safari')).toBeNull()
  })
})
