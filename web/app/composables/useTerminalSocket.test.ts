import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'

vi.mock('~/middleware/auth', () => ({ resetAuth: vi.fn() }))

// Minimal stand-in for the browser WebSocket: records sent frames and lets
// tests drive open/message/close.
class FakeWebSocket {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSING = 2
  static CLOSED = 3
  static instances: FakeWebSocket[] = []

  readyState = FakeWebSocket.CONNECTING
  binaryType = 'blob'
  sent: Uint8Array[] = []
  onopen: (() => void) | null = null
  onclose: ((e: { code: number }) => void) | null = null
  onerror: (() => void) | null = null
  onmessage: ((e: { data: ArrayBuffer }) => void) | null = null

  constructor(public url: string) {
    FakeWebSocket.instances.push(this)
  }

  send(data: Uint8Array) { this.sent.push(data) }
  close() {
    this.readyState = FakeWebSocket.CLOSED
    this.onclose?.({ code: 1000 })
  }

  open() {
    this.readyState = FakeWebSocket.OPEN
    this.onopen?.()
  }

  drop(code = 1006) {
    this.readyState = FakeWebSocket.CLOSED
    this.onclose?.({ code })
  }

  receive(type: number, payload: Uint8Array | string = new Uint8Array()) {
    const body = typeof payload === 'string' ? new TextEncoder().encode(payload) : payload
    const frame = new Uint8Array(1 + body.length)
    frame[0] = type
    frame.set(body, 1)
    this.onmessage?.({ data: frame.buffer })
  }

  /** Sent frames decoded as [type, JSON payload or bytes]. */
  frames(): Array<[number, unknown]> {
    return this.sent.map((f) => {
      const body = f.slice(1)
      try {
        return [f[0]!, JSON.parse(new TextDecoder().decode(body))]
      }
      catch {
        return [f[0]!, body]
      }
    })
  }
}

const SELECT = 0x04
const RESIZE = 0x03
const SELECT_WINDOW = 0x05
const ERROR = 0x07
const HEARTBEAT = 0x08

// The composable is a module-scoped singleton: load a fresh copy per test,
// and disconnect every copy afterwards so its window listeners go away.
const loaded: Array<{ disconnect: () => void }> = []

async function loadSocket() {
  vi.resetModules()
  const mod = await import('./useTerminalSocket')
  const socket = mod.useTerminalSocket()
  loaded.push(socket)
  return socket
}

afterEach(() => {
  for (const socket of loaded.splice(0)) socket.disconnect()
})

function latest(): FakeWebSocket {
  const ws = FakeWebSocket.instances.at(-1)
  if (!ws) throw new Error('no WebSocket created')
  return ws
}

describe('useTerminalSocket', () => {
  beforeEach(() => {
    FakeWebSocket.instances = []
    vi.useFakeTimers()
    vi.stubGlobal('WebSocket', FakeWebSocket)
    // Refused connections check the login over HTTP; never hit the network.
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ status: 200 }))
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('sends the select with its size when open', async () => {
    const socket = await loadSocket()
    socket.connect()
    latest().open()

    socket.selectSession('dev', { cols: 120, rows: 40 })
    expect(latest().frames()).toEqual([[SELECT, { sessionName: 'dev', cols: 120, rows: 40 }]])
  })

  it('sends a select made while connecting once the socket opens', async () => {
    const socket = await loadSocket()
    socket.connect()

    socket.selectSession('dev', { cols: 100, rows: 30 })
    expect(latest().sent).toHaveLength(0)

    latest().open()
    expect(latest().frames()).toEqual([
      [SELECT, { sessionName: 'dev', cols: 100, rows: 30 }],
      [RESIZE, { cols: 100, rows: 30 }],
    ])
  })

  it('re-attaches the session after a reconnect and notifies subscribers', async () => {
    const socket = await loadSocket()
    const reconnects = vi.fn()
    socket.onReconnect(reconnects)

    socket.connect()
    latest().open()
    socket.selectSession('dev')
    socket.sendResize(90, 25)

    latest().drop()
    expect(socket.connected.value).toBe(false)
    vi.advanceTimersByTime(1000)
    const second = latest()
    expect(FakeWebSocket.instances).toHaveLength(2)

    second.open()
    expect(reconnects).toHaveBeenCalledTimes(1)
    expect(second.frames()).toEqual([
      [SELECT, { sessionName: 'dev', cols: 90, rows: 25 }],
      [RESIZE, { cols: 90, rows: 25 }],
    ])
  })

  it('does not re-attach after the session was cleared', async () => {
    const socket = await loadSocket()
    socket.connect()
    latest().open()
    socket.selectSession('dev')
    socket.clearSession()

    latest().drop()
    vi.advanceTimersByTime(1000)
    latest().open()
    expect(latest().sent).toHaveLength(0)
  })

  it('queues window selection until the socket opens', async () => {
    const socket = await loadSocket()
    socket.connect()
    socket.selectWindow('dev', 2)
    latest().open()
    expect(latest().frames()).toEqual([[SELECT_WINDOW, { sessionName: 'dev', windowIndex: 2 }]])
  })

  it('re-selects the session when the server reports not_attached', async () => {
    const socket = await loadSocket()
    const reconnects = vi.fn()
    socket.onReconnect(reconnects)
    socket.connect()
    latest().open()
    socket.selectSession('dev')
    latest().sent = []

    latest().receive(ERROR, JSON.stringify({ code: 'not_attached', message: 'no session attached' }))
    expect(reconnects).toHaveBeenCalledTimes(1)
    expect(latest().frames()).toEqual([[SELECT, { sessionName: 'dev' }]])
  })

  it('treats a heartbeat as a no-op frame', async () => {
    const socket = await loadSocket()
    const output = vi.fn()
    socket.onOutput(output)
    socket.connect()
    latest().open()
    latest().receive(HEARTBEAT)
    expect(output).not.toHaveBeenCalled()
  })
})

describe('useTerminalSocket liveness', () => {
  beforeEach(() => {
    FakeWebSocket.instances = []
    vi.useFakeTimers()
    vi.stubGlobal('WebSocket', FakeWebSocket)
    // Refused connections check the login over HTTP; never hit the network.
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ status: 200 }))
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true })
  })

  it('keeps a connection that receives heartbeats', async () => {
    const socket = await loadSocket()
    socket.connect()
    latest().open()

    for (let i = 0; i < 6; i++) {
      vi.advanceTimersByTime(25_000)
      latest().receive(HEARTBEAT)
    }
    expect(FakeWebSocket.instances).toHaveLength(1)
    expect(socket.connected.value).toBe(true)
  })

  it('drops a silent connection and reconnects', async () => {
    const socket = await loadSocket()
    socket.connect()
    const dead = latest()
    dead.open()

    vi.advanceTimersByTime(70_000)
    expect(socket.connected.value).toBe(false)
    expect(dead.readyState).toBe(FakeWebSocket.CLOSED)

    vi.advanceTimersByTime(1000)
    expect(FakeWebSocket.instances).toHaveLength(2)
  })

  it('reconnects immediately when the network comes back', async () => {
    const socket = await loadSocket()
    socket.connect()
    latest().open()

    // Several failed attempts grow the backoff.
    for (let i = 0; i < 4; i++) {
      latest().drop()
      vi.advanceTimersByTime(30_000)
    }
    latest().drop()
    const attempts = FakeWebSocket.instances.length

    window.dispatchEvent(new Event('online'))
    expect(FakeWebSocket.instances).toHaveLength(attempts + 1)
  })

  it('does not reconnect while the tab is hidden, and does once it is shown', async () => {
    const socket = await loadSocket()
    socket.connect()
    latest().open()

    setVisibility('hidden')
    latest().drop()
    vi.advanceTimersByTime(120_000)
    window.dispatchEvent(new Event('online'))
    expect(FakeWebSocket.instances).toHaveLength(1)

    setVisibility('visible')
    expect(FakeWebSocket.instances).toHaveLength(2)
  })

  it('does not reconnect a hidden tab after a silent connection is dropped', async () => {
    const socket = await loadSocket()
    socket.connect()
    latest().open()

    setVisibility('hidden')
    vi.advanceTimersByTime(180_000)
    expect(socket.connected.value).toBe(false)
    expect(FakeWebSocket.instances).toHaveLength(1)

    setVisibility('visible')
    expect(FakeWebSocket.instances).toHaveLength(2)
  })
})

function setVisibility(state: 'hidden' | 'visible') {
  Object.defineProperty(document, 'visibilityState', { value: state, configurable: true })
  document.dispatchEvent(new Event('visibilitychange'))
}

describe('useTerminalSocket auth', () => {
  beforeEach(() => {
    FakeWebSocket.instances = []
    vi.useFakeTimers()
    vi.stubGlobal('WebSocket', FakeWebSocket)
    // Refused connections check the login over HTTP; never hit the network.
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ status: 200 }))
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('goes to the login page when a refused connection turns out to be logged out', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ status: 401 })
    vi.stubGlobal('fetch', fetchMock)
    // A plain object, so assigning href doesn't make happy-dom navigate.
    const realLocation = Object.getOwnPropertyDescriptor(window, 'location')!
    const location = { href: 'http://localhost/' }
    Object.defineProperty(window, 'location', { configurable: true, value: location })
    const { resetAuth } = await import('~/middleware/auth')
    const socket = await loadSocket()

    socket.connect()
    latest().drop()
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/sessions', expect.anything()))
    await Promise.resolve()

    expect(vi.mocked(resetAuth)).toHaveBeenCalled()
    expect(location.href).toBe('/login')
    vi.advanceTimersByTime(60_000)
    expect(FakeWebSocket.instances).toHaveLength(1)
    Object.defineProperty(window, 'location', realLocation)
  })

  it('keeps retrying when still logged in', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ status: 200 })
    vi.stubGlobal('fetch', fetchMock)
    const socket = await loadSocket()

    socket.connect()
    latest().drop()
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalled())
    vi.advanceTimersByTime(1000)
    expect(FakeWebSocket.instances).toHaveLength(2)
  })

  it('does not check auth when an open connection drops', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ status: 200 })
    vi.stubGlobal('fetch', fetchMock)
    const socket = await loadSocket()

    socket.connect()
    latest().open()
    latest().drop()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
