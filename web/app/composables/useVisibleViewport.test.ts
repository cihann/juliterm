import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { useVisibleViewport } from './useVisibleViewport'

class FakeVisualViewport extends EventTarget {
  height = 900
}

function mountWithViewport() {
  return mount(defineComponent({
    setup() { useVisibleViewport() },
    render: () => h('div'),
  }))
}

describe('useVisibleViewport', () => {
  let vv: FakeVisualViewport

  beforeEach(() => {
    vv = new FakeVisualViewport()
    vi.stubGlobal('visualViewport', vv)
    vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  const appHeight = () => document.documentElement.style.getPropertyValue('--app-height')

  it('sizes the app to the visible area and follows it', () => {
    const wrapper = mountWithViewport()
    expect(appHeight()).toBe('900px')

    vv.height = 612.4 // keyboard shown
    vv.dispatchEvent(new Event('resize'))
    expect(appHeight()).toBe('612px')

    wrapper.unmount()
    expect(appHeight()).toBe('')
  })

  it('catches the viewport growing back even when no resize is reported', () => {
    vi.useFakeTimers()
    const wrapper = mountWithViewport()
    vv.height = 520 // keyboard up
    vv.dispatchEvent(new Event('resize'))
    expect(appHeight()).toBe('520px')

    vv.height = 1000 // keyboard collapsed to the shortcut bar, no event
    vi.advanceTimersByTime(1000)
    expect(appHeight()).toBe('1000px')

    wrapper.unmount()
    vi.useRealTimers()
  })

  it('re-reads the height once focus leaves the terminal', () => {
    vi.useFakeTimers()
    const wrapper = mountWithViewport()
    vv.height = 520
    vv.dispatchEvent(new Event('resize'))

    document.dispatchEvent(new Event('focusout'))
    vv.height = 1000 // keyboard finishes closing a moment later
    vi.advanceTimersByTime(400)
    expect(appHeight()).toBe('1000px')

    wrapper.unmount()
    vi.useRealTimers()
  })

  it('undoes page scroll caused by the keyboard', () => {
    mountWithViewport()
    Object.defineProperty(window, 'scrollY', { value: 240, configurable: true })
    vv.dispatchEvent(new Event('scroll'))
    expect(window.scrollTo).toHaveBeenCalledWith(0, 0)
    Object.defineProperty(window, 'scrollY', { value: 0, configurable: true })
  })
})
