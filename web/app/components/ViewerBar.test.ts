import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { Session } from '~/composables/useSessionTree'
import ViewerBar from './ViewerBar.vue'

const mockZoomPane = vi.fn()

vi.mock('~/composables/useSessionApi', () => ({
  useSessionApi: () => ({
    zoomPane: mockZoomPane,
    createSession: vi.fn(),
    renameSession: vi.fn(),
    killSession: vi.fn(),
    createWindow: vi.fn(),
    renameWindow: vi.fn(),
    killWindow: vi.fn(),
    splitWindow: vi.fn(),
    detectTestCommand: vi.fn(),
  }),
}))

const stubs = { SplitDropdown: true }

const multiPaneSessions: Session[] = [{
  id: '$1',
  name: 'dev',
  windows: [{
    id: '@0',
    name: 'editor',
    index: 0,
    active: true,
    panes: [
      { id: '%0', width: 80, height: 24, active: true },
      { id: '%1', width: 80, height: 24, active: false },
    ],
  }],
}]

const singlePaneSessions: Session[] = [{
  id: '$1',
  name: 'dev',
  windows: [{
    id: '@0',
    name: 'editor',
    index: 0,
    active: true,
    panes: [
      { id: '%0', width: 80, height: 24, active: true },
    ],
  }],
}]

describe('ViewerBar', () => {
  it('renders a Sessions button and emits back when clicked', async () => {
    const wrapper = mount(ViewerBar, {
      props: {
        sessionName: 'agent-monitoring',
        connected: true,
      },
      global: { stubs },
    })

    const backButton = wrapper.get('.back-button')
    expect(backButton.text()).toBe('Sessions')
    expect(wrapper.find('.session-info').text()).toContain('agent-monitoring')

    await backButton.trigger('click')
    expect(wrapper.emitted('back')).toEqual([[]])
  })

  it('renders SplitDropdown in default mode', () => {
    const wrapper = mount(ViewerBar, {
      props: {
        sessionName: 'dev',
        connected: true,
      },
      global: { stubs },
    })

    expect(wrapper.findComponent({ name: 'SplitDropdown' }).exists()).toBe(true)
  })

  it('hides SplitDropdown in compact mode', () => {
    const wrapper = mount(ViewerBar, {
      props: {
        sessionName: 'dev',
        connected: true,
        compact: true,
      },
      global: { stubs },
    })

    expect(wrapper.findComponent({ name: 'SplitDropdown' }).exists()).toBe(false)
  })

  it('applies compact class and truncates session name', () => {
    const wrapper = mount(ViewerBar, {
      props: {
        sessionName: 'a-very-long-session-name-that-should-truncate',
        connected: true,
        compact: true,
      },
      global: { stubs },
    })

    expect(wrapper.find('.viewer-bar').classes()).toContain('compact')
    expect(wrapper.find('.session-name--compact').exists()).toBe(true)
  })

  it('still emits back in compact mode', async () => {
    const wrapper = mount(ViewerBar, {
      props: {
        sessionName: 'dev',
        connected: true,
        compact: true,
      },
      global: { stubs },
    })

    await wrapper.get('.back-button').trigger('click')
    expect(wrapper.emitted('back')).toEqual([[]])
  })

  // --- Zoom button tests ---

  it('shows zoom button in compact mode with 2+ panes', () => {
    const wrapper = mount(ViewerBar, {
      props: {
        sessionName: 'dev',
        connected: true,
        compact: true,
        sessions: multiPaneSessions,
      },
      global: { stubs },
    })

    expect(wrapper.find('.zoom-button').exists()).toBe(true)
  })

  it('hides zoom button in compact mode with 1 pane', () => {
    const wrapper = mount(ViewerBar, {
      props: {
        sessionName: 'dev',
        connected: true,
        compact: true,
        sessions: singlePaneSessions,
      },
      global: { stubs },
    })

    expect(wrapper.find('.zoom-button').exists()).toBe(false)
  })

  it('hides zoom button in non-compact mode', () => {
    const wrapper = mount(ViewerBar, {
      props: {
        sessionName: 'dev',
        connected: true,
        compact: false,
        sessions: multiPaneSessions,
      },
      global: { stubs },
    })

    expect(wrapper.find('.zoom-button').exists()).toBe(false)
  })

  it('calls zoomPane API on click', async () => {
    mockZoomPane.mockClear()
    const wrapper = mount(ViewerBar, {
      props: {
        sessionName: 'dev',
        connected: true,
        compact: true,
        sessions: multiPaneSessions,
      },
      global: { stubs },
    })

    await wrapper.find('.zoom-button').trigger('click')
    expect(mockZoomPane).toHaveBeenCalledWith('dev')
  })

  it('emits toggleSidebar from the sidebar button', async () => {
    const wrapper = mount(ViewerBar, {
      props: { sessionName: 'dev', connected: true },
      global: { stubs },
    })

    const toggle = wrapper.find('.sidebar-toggle')
    expect(toggle.attributes('title')).toBe('Hide sidebar')
    await toggle.trigger('click')
    expect(wrapper.emitted('toggleSidebar')).toHaveLength(1)
  })

  it('labels the sidebar button for showing a collapsed sidebar', () => {
    const wrapper = mount(ViewerBar, {
      props: { sessionName: 'dev', connected: true, sidebarCollapsed: true },
      global: { stubs },
    })

    expect(wrapper.find('.sidebar-toggle').attributes('title')).toBe('Show sidebar')
  })

  it('hides the sidebar button in compact mode', () => {
    const wrapper = mount(ViewerBar, {
      props: { sessionName: 'dev', connected: true, compact: true },
      global: { stubs },
    })

    expect(wrapper.find('.sidebar-toggle').exists()).toBe(false)
  })
})
