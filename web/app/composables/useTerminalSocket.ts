// Binary WebSocket composable for the Juli terminal protocol (PTY-based).
// Singleton — module-scoped state shared across all components (safe with ssr: false).
//
// Message type bytes must match Go protocol/message.go exactly.

import { ref, readonly } from 'vue'
import { resetAuth } from '~/middleware/auth'

const MSG = {
  OUTPUT: 0x01,       // server→client: raw PTY output
  INPUT: 0x02,        // client→server: raw keyboard input
  RESIZE: 0x03,       // client→server: JSON {cols, rows}
  SESSION_TREE: 0x04,   // bidirectional: JSON session tree / session select
  SELECT_WINDOW: 0x05,  // client→server: JSON {sessionName, windowIndex}
  ERROR: 0x07,          // server→client: JSON error
  HEARTBEAT: 0x08,      // server→client: empty liveness frame
} as const

type Callback<T> = (data: T) => void

interface ErrorPayload {
  code: string
  message: string
}

interface TerminalSize {
  cols: number
  rows: number
}

// ---- Module-scoped singleton state ----

const connected = ref(false)
const reconnecting = ref(false)
const error = ref<string | null>(null)

// The server sends a heartbeat every 25s, so a connection that has delivered
// nothing for a minute is dead even if the browser still reports it open
// (laptop sleep, network switch).
const LIVENESS_CHECK_MS = 10_000
const STALE_AFTER_MS = 60_000
const MIN_RECONNECT_DELAY = 500

let ws: WebSocket | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
let reconnectDelay = MIN_RECONNECT_DELAY
let intentionallyClosed = false
let lastMessageAt = 0
let livenessTimer: ReturnType<typeof setInterval> | null = null
let lifecycleListening = false

// Use Set for O(1) add/delete, prevents duplicates
const onOutputCbs = new Set<Callback<Uint8Array>>()
const onSessionTreeCbs = new Set<Callback<unknown[]>>()
const onErrorCbs = new Set<Callback<ErrorPayload>>()

const onReconnectCbs = new Set<() => void>()

// Buffer last session tree so late subscribers get it immediately
let lastSessionTree: unknown[] | null = null

// The session this browser is showing and its terminal size. The server only
// attaches a PTY when it receives a session select, so both are replayed every
// time a connection opens: after a reconnect, and when the session was picked
// before the socket finished connecting.
let currentSession: string | null = null
let lastSize: TerminalSize | null = null

// Other control frames (window selection) sent while not connected, flushed
// once the session has been re-selected.
let pendingControl: Uint8Array[] = []

// ---- Internal helpers ----

function getWsUrl(): string {
  const loc = window.location
  const proto = loc.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${loc.host}/ws`
}

function connect() {
  if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
    return
  }

  intentionallyClosed = false
  error.value = null
  watchLiveness()

  let opened = false

  try {
    ws = new WebSocket(getWsUrl())
    ws.binaryType = 'arraybuffer'
  } catch (e) {
    error.value = `Failed to create WebSocket: ${e}`
    scheduleReconnect()
    return
  }

  ws.onopen = () => {
    opened = true
    connected.value = true
    reconnecting.value = false
    reconnectDelay = MIN_RECONNECT_DELAY
    error.value = null
    lastMessageAt = Date.now()

    if (currentSession) {
      reattach()
    }
    const queued = pendingControl
    pendingControl = []
    for (const frame of queued) send(frame)
  }

  ws.onclose = (event: CloseEvent) => {
    connected.value = false
    ws = null

    // Close code 4001 = session revoked by server (logout or expiry).
    // Stop reconnecting and redirect to login.
    if (event.code === 4001) {
      goToLogin()
      return
    }

    if (!intentionallyClosed) {
      scheduleReconnect()
      // A refused upgrade (expired login, or the server restarted and lost
      // its in-memory sessions) surfaces as a plain 1006 close, so check
      // whether we are still logged in rather than retry forever.
      if (!opened) void checkAuth()
    }
  }

  ws.onerror = () => {
    error.value = 'WebSocket error'
  }

  ws.onmessage = (event: MessageEvent) => {
    lastMessageAt = Date.now()
    if (!(event.data instanceof ArrayBuffer)) return
    const buf = new Uint8Array(event.data)
    if (buf.length === 0) return

    const msgType = buf[0]
    const payload = buf.slice(1)

    switch (msgType) {
      case MSG.OUTPUT:
        for (const cb of onOutputCbs) cb(payload)
        break
      case MSG.SESSION_TREE:
        try {
          const parsed = JSON.parse(new TextDecoder().decode(payload)) as unknown[]
          lastSessionTree = parsed
          for (const cb of onSessionTreeCbs) cb(parsed)
        } catch { /* ignore malformed JSON */ }
        break
      case MSG.ERROR:
        try {
          const parsed = JSON.parse(new TextDecoder().decode(payload)) as ErrorPayload
          // The server got input with no session attached: attach again
          // rather than keep typing into nothing.
          if (parsed.code === 'not_attached' && currentSession) {
            reattach()
          }
          for (const cb of onErrorCbs) cb(parsed)
        } catch { /* ignore malformed JSON */ }
        break
      case MSG.HEARTBEAT:
        break
    }
  }
}

function goToLogin() {
  intentionallyClosed = true
  if (reconnectTimer) {
    clearTimeout(reconnectTimer)
    reconnectTimer = null
  }
  resetAuth()
  window.location.href = '/login'
}

async function checkAuth() {
  try {
    const res = await fetch('/api/sessions', { credentials: 'same-origin' })
    if (res.status === 401 && !intentionallyClosed) goToLogin()
  } catch {
    // Server unreachable: keep retrying the WebSocket.
  }
}

// Drop a connection that stopped delivering frames. Its close handshake would
// wait on a dead network, so detach it and reconnect right away.
function abandonSocket() {
  if (!ws) return
  const dead = ws
  ws = null
  dead.onopen = null
  dead.onclose = null
  dead.onerror = null
  dead.onmessage = null
  try { dead.close() } catch { /* already closed */ }
  connected.value = false
  scheduleReconnect()
}

// A hidden tab must not reconnect: attaching makes it tmux's newest client,
// so the shared window would shrink to this device's size while nobody is
// looking at it. iPadOS wakes background tabs every so often, which used to
// resize tmux to the iPad's size with no one using juli. Reconnecting waits
// until the tab is visible again (see onVisibilityChange).
function pageHidden() {
  return typeof document !== 'undefined' && document.visibilityState === 'hidden'
}

function checkLiveness() {
  if (isOpen() && Date.now() - lastMessageAt > STALE_AFTER_MS) {
    abandonSocket()
  }
}

// The tab became visible or the network came back: verify the connection now
// and, if it's down, reconnect immediately instead of waiting out a backoff
// that may have grown to 30s.
function wake() {
  if (intentionallyClosed || pageHidden()) return
  checkLiveness()
  reconnectDelay = MIN_RECONNECT_DELAY
  if (!ws) {
    if (reconnectTimer) {
      clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
    connect()
  }
}

function onVisibilityChange() {
  if (document.visibilityState === 'visible') wake()
}

function watchLiveness() {
  if (!livenessTimer) {
    livenessTimer = setInterval(checkLiveness, LIVENESS_CHECK_MS)
  }
  if (!lifecycleListening) {
    lifecycleListening = true
    document.addEventListener('visibilitychange', onVisibilityChange)
    window.addEventListener('online', wake)
  }
}

function scheduleReconnect() {
  if (intentionallyClosed) return
  if (reconnectTimer) return

  reconnecting.value = true
  const jitter = Math.random() * reconnectDelay * 0.25
  const delay = reconnectDelay + jitter

  reconnectTimer = setTimeout(() => {
    reconnectTimer = null
    // Hidden: leave it to wake() when the tab is shown again.
    if (pageHidden()) return
    reconnectDelay = Math.min(reconnectDelay * 2, 30000)
    connect()
  }, delay)
}

function send(data: Uint8Array) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(data)
  }
}

function sendSelect() {
  if (!currentSession) return
  send(buildJsonFrame(MSG.SESSION_TREE, { sessionName: currentSession, ...lastSize }))
}

// Attach the current session again on this connection. tmux repaints the
// whole screen on attach, so subscribers reset their terminal first.
function reattach() {
  for (const cb of onReconnectCbs) cb()
  sendSelect()
  if (lastSize) send(buildJsonFrame(MSG.RESIZE, lastSize))
}

function isOpen(): boolean {
  return ws !== null && ws.readyState === WebSocket.OPEN
}

function buildJsonFrame(msgType: number, payload: object): Uint8Array {
  const json = new TextEncoder().encode(JSON.stringify(payload))
  const frame = new Uint8Array(1 + json.length)
  frame[0] = msgType
  frame.set(json, 1)
  return frame
}

function disconnect() {
  intentionallyClosed = true
  if (reconnectTimer) {
    clearTimeout(reconnectTimer)
    reconnectTimer = null
  }
  if (livenessTimer) {
    clearInterval(livenessTimer)
    livenessTimer = null
  }
  if (lifecycleListening) {
    lifecycleListening = false
    document.removeEventListener('visibilitychange', onVisibilityChange)
    window.removeEventListener('online', wake)
  }
  if (ws) {
    ws.close()
    ws = null
  }
  connected.value = false
  reconnecting.value = false
}

// ---- Public composable ----

export function useTerminalSocket() {
  // Don't auto-connect here — let the consumer call connect() after
  // auth is confirmed. The auth middleware validates the cookie first,
  // and only then should the WebSocket be opened.

  /**
   * Attach this connection to a session, optionally at a known terminal size
   * so the server attaches at the right size instead of 80x24. When the
   * socket isn't open yet, the select is sent as soon as it opens.
   */
  function selectSession(sessionName: string, size?: TerminalSize) {
    currentSession = sessionName
    if (size) lastSize = size
    if (isOpen()) sendSelect()
  }

  /** Forget the current session (e.g. when returning to the session grid). */
  function clearSession() {
    currentSession = null
  }

  function selectWindow(sessionName: string, windowIndex: number) {
    const frame = buildJsonFrame(MSG.SELECT_WINDOW, { sessionName, windowIndex })
    if (isOpen()) send(frame)
    else pendingControl.push(frame)
  }

  function sendInput(data: Uint8Array) {
    const frame = new Uint8Array(1 + data.length)
    frame[0] = MSG.INPUT
    frame.set(data, 1)
    send(frame)
  }

  function sendResize(cols: number, rows: number) {
    lastSize = { cols, rows }
    send(buildJsonFrame(MSG.RESIZE, lastSize))
  }

  /** Subscribe to PTY output. Returns unsubscribe function. */
  function onOutput(cb: Callback<Uint8Array>): () => void {
    onOutputCbs.add(cb)
    return () => { onOutputCbs.delete(cb) }
  }

  /** Subscribe to session tree updates. Replays last value immediately. Returns unsubscribe function. */
  function onSessionTree(cb: Callback<unknown[]>): () => void {
    onSessionTreeCbs.add(cb)
    if (lastSessionTree !== null) {
      cb(lastSessionTree)
    }
    return () => { onSessionTreeCbs.delete(cb) }
  }

  /**
   * Subscribe to re-attaches: after a reconnect, or when the server reported
   * that nothing was attached. Returns unsubscribe function.
   */
  function onReconnect(cb: () => void): () => void {
    onReconnectCbs.add(cb)
    return () => { onReconnectCbs.delete(cb) }
  }

  /** Subscribe to server errors. Returns unsubscribe function. */
  function onError(cb: Callback<ErrorPayload>): () => void {
    onErrorCbs.add(cb)
    return () => { onErrorCbs.delete(cb) }
  }

  return {
    connected: readonly(connected),
    reconnecting: readonly(reconnecting),
    error: readonly(error),
    selectSession,
    clearSession,
    selectWindow,
    sendInput,
    sendResize,
    disconnect,
    connect,
    onOutput,
    onSessionTree,
    onReconnect,
    onError,
  }
}

export type { ErrorPayload, TerminalSize }
