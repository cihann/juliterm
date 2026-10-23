<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { WebglAddon } from '@xterm/addon-webgl'
import '@xterm/xterm/css/xterm.css'

import { useTerminalSocket, type TerminalSize } from '~/composables/useTerminalSocket'
import { useSessionApi } from '~/composables/useSessionApi'
import { useViewportMode } from '~/composables/useViewportMode'
import { ctrlActive, altActive, controlByte } from '~/composables/useMobileKeyModifiers'
import { createGlyphSubstitution } from '~/utils/glyphSubstitution'
import { browserUrl } from '~/utils/linkBrowser'
import { useLinkBrowser } from '~/composables/useLinkBrowser'
import { focusIsHeldForTerminal, markAsTerminalInput } from '~/utils/focusHandoff'
import { parseOsc52 } from '~/utils/osc52'
import { copyText, readText } from '~/utils/clipboard'
import { cellAt, linearSelection, type Cell, type ScreenBox } from '~/utils/touchCells'
import { selectMode, setPasteHandler } from '~/composables/useTerminalTouch'

const props = defineProps<{
  sessionName: string | null
}>()

const emit = defineEmits<{
  error: [message: string]
}>()

const terminalRef = ref<HTMLDivElement | null>(null)
const socket = useTerminalSocket()
const glyphs = createGlyphSubstitution()
const { linkBrowser } = useLinkBrowser()
const sessionTree = useSessionTree()
const api = useSessionApi()
const { isMobile } = useViewportMode(1024)
// Touch devices get the key bar at any width: an iPad in landscape is wider
// than 1024px, and its hardware keyboards often have no Esc key.
const hasTouchScreen = typeof window !== 'undefined' && window.matchMedia('(any-pointer: coarse)').matches
const showKeyBar = computed(() => isMobile.value || hasTouchScreen)

// Context menu state
const ctxVisible = ref(false)
const ctxX = ref(0)
const ctxY = ref(0)

const isZoomed = computed(() => {
  if (!props.sessionName) return false
  const session = sessionTree.findSession(props.sessionName)
  if (!session) return false
  const activeWindow = session.windows.find(w => w.active)
  return activeWindow?.zoomed ?? false
})

function onContextMenu(e: MouseEvent) {
  e.preventDefault()
  if (!props.sessionName) return
  ctxX.value = e.clientX
  ctxY.value = e.clientY
  ctxVisible.value = true
}

async function onCtxAction(action: string) {
  if (!props.sessionName) return
  try {
    switch (action) {
      case 'horizontal-split':
        await api.splitWindow(props.sessionName, 'custom', undefined, 'horizontal')
        break
      case 'vertical-split':
        await api.splitWindow(props.sessionName, 'custom', undefined, 'vertical')
        break
      case 'kill-pane':
        await api.killPane(props.sessionName)
        break
      case 'zoom':
        await api.zoomPane(props.sessionName)
        break
    }
  } catch (e: unknown) {
    const message = e instanceof Error ? e.message : 'Operation failed'
    emit('error', message)
  }
}

// Clipboard notice in the corner of the terminal: a short confirmation, or a
// copy waiting for a tap when the browser refused to write it directly.
interface ClipNotice {
  message: string
  pending: string | null
}
const clipNotice = ref<ClipNotice | null>(null)
let clipNoticeTimer: ReturnType<typeof setTimeout> | null = null

function showClipNotice(message: string, pending: string | null = null, ms = 1500) {
  if (clipNoticeTimer) clearTimeout(clipNoticeTimer)
  clipNotice.value = { message, pending }
  clipNoticeTimer = setTimeout(() => {
    clipNotice.value = null
    clipNoticeTimer = null
  }, ms)
}

function dismissClipNotice() {
  if (clipNoticeTimer) clearTimeout(clipNoticeTimer)
  clipNoticeTimer = null
  clipNotice.value = null
}

// An app copied text (tmux copy mode, Claude Code's selection) via OSC 52.
// Write it straight away where the browser allows; Safari and every iPadOS
// browser refuse outside a user gesture, so offer a tap that copies it.
async function copyFromApp(text: string) {
  if (await copyText(text)) {
    showClipNotice('Copied')
    return
  }
  showClipNotice(`Copied ${text.length} characters — tap to copy to clipboard`, text, 10000)
}

// Runs inside the tap on the notice, which Safari accepts as a user gesture.
async function copyPending() {
  const text = clipNotice.value?.pending
  if (!text) return
  showClipNotice(await copyText(text) ? 'Copied' : 'Copy failed')
}

// The key bar's Paste key. terminal.paste() goes through xterm's paste path,
// which adds bracketed-paste markers when the app asked for them.
async function pasteFromClipboard() {
  const text = await readText()
  if (text === null) {
    showClipNotice('Paste failed: clipboard not available', null, 2500)
    return
  }
  if (text) terminal?.paste(text)
  terminal?.focus()
}

function dismissClipNoticeOnEscape(e: KeyboardEvent) {
  if (e.key === 'Escape' && clipNotice.value?.pending) dismissClipNotice()
}

let terminal: Terminal | null = null
let fitAddon: FitAddon | null = null
let webglAddon: WebglAddon | null = null
let resizeObserver: ResizeObserver | null = null
let unsubOutput: (() => void) | null = null
let unsubReconnect: (() => void) | null = null

// Container resizes (window, sidebar drag, mobile keyboard) fire many times a
// second; wait for them to settle, and only tell the server when the cell
// size actually changed. Every resize reflows the shared tmux window.
const RESIZE_DEBOUNCE_MS = 75
let resizeTimer: ReturnType<typeof setTimeout> | null = null
let lastSentSize: TerminalSize | null = null

// Fit the terminal to its container and return the resulting size, or null
// when the container is collapsed (hidden, mid-transition): fitting then
// would squash the terminal, and sending that size would shrink the shared
// tmux window for every client.
function fitTerminal(): TerminalSize | null {
  if (!terminal || !fitAddon || !terminalRef.value) return null
  if (terminalRef.value.clientWidth === 0) return null
  const dims = fitAddon.proposeDimensions()
  if (!dims || dims.cols < 10) return null
  fitAddon.fit()
  return { cols: terminal.cols, rows: terminal.rows }
}

function syncSize() {
  const size = fitTerminal()
  if (!size) return
  if (lastSentSize && lastSentSize.cols === size.cols && lastSentSize.rows === size.rows) return
  lastSentSize = size
  socket.sendResize(size.cols, size.rows)
}

function scheduleSyncSize() {
  if (resizeTimer) clearTimeout(resizeTimer)
  resizeTimer = setTimeout(() => {
    resizeTimer = null
    syncSize()
  }, RESIZE_DEBOUNCE_MS)
}

// Draw with WebGL: the default DOM renderer struggles with heavy output and
// full-screen repaints. Browsers without WebGL2 (some mobile or remote
// browsers) keep the DOM renderer, and so does a lost GPU context.
// The WebGL renderer is opt-in with `?renderer=webgl`: in testing it stopped
// repainting after resizes and session switches while output kept arriving,
// whereas xterm's default DOM renderer stayed reliable.
function webglRequested() {
  return new URLSearchParams(window.location.search).get('renderer') === 'webgl'
}

function loadWebglRenderer() {
  if (!terminal || !webglRequested()) return
  try {
    const addon = new WebglAddon()
    addon.onContextLoss(() => {
      addon.dispose()
      if (webglAddon === addon) webglAddon = null
    })
    terminal.loadAddon(addon)
    webglAddon = addon
  } catch {
    webglAddon = null
  }
}

// Touch gestures. Browsers don't turn a finger swipe into wheel input, and
// xterm has no touch handling, so with tmux mouse mode on a swipe did nothing
// (xterm's own scrollback is empty inside tmux). Vertical swipes become
// synthetic wheel events, one per few cells of movement: xterm reports them
// to tmux as mouse-wheel input, or scrolls its own buffer when no app has
// mouse mode on. Taps are left alone so they still focus the terminal.
//
// In Select mode (the key bar's Select key) a drag instead makes xterm's own
// selection over the visible screen and lifting the finger copies it; the
// touchend is a user gesture, so Safari allows the clipboard write. Select
// mode turns itself off after a copy, so the next swipe scrolls again.
const TAP_SLOP_PX = 8
// Finger travel, in cells, per wheel step. Line-mode events give exactly one
// wheel report each (xterm damps small pixel deltas to 0.3 lines), and tmux
// scrolls 5 lines per report, so this keeps the content near the finger.
const CELLS_PER_WHEEL_STEP = 3

interface TouchState {
  startY: number
  lastY: number
  accum: number
  scrolling: boolean
  anchor: Cell | null
}
let touch: TouchState | null = null

function screenBox(): ScreenBox | null {
  const el = terminal?.element?.querySelector('.xterm-screen')
  if (!el) return null
  const r = el.getBoundingClientRect()
  if (r.width === 0 || r.height === 0) return null
  return { left: r.left, top: r.top, width: r.width, height: r.height }
}

// Dispatch where a real wheel event would land, so it reaches whichever
// xterm element listens (mouse reporting or the scrollable viewport).
function sendWheel(lines: number, clientX: number, clientY: number) {
  const root = terminal?.element
  if (!root) return
  const hit = document.elementFromPoint(clientX, clientY)
  const target = hit && root.contains(hit) ? hit : (root.querySelector('.xterm-screen') ?? root)
  target.dispatchEvent(new WheelEvent('wheel', {
    deltaY: lines, deltaMode: WheelEvent.DOM_DELTA_LINE, clientX, clientY, bubbles: true, cancelable: true,
  }))
}

function onTouchStart(e: TouchEvent) {
  if (!terminal || e.touches.length !== 1) {
    touch = null
    return
  }
  const t = e.touches[0]!
  touch = { startY: t.clientY, lastY: t.clientY, accum: 0, scrolling: false, anchor: null }
  if (selectMode.value) {
    const box = screenBox()
    if (!box) return
    touch.anchor = cellAt(t.clientX, t.clientY, box, terminal.cols, terminal.rows)
    terminal.clearSelection()
  }
}

function selectTo(x: number, y: number) {
  if (!terminal || !touch?.anchor) return
  const box = screenBox()
  if (!box) return
  const current = cellAt(x, y, box, terminal.cols, terminal.rows)
  const sel = linearSelection(touch.anchor, current, terminal.cols)
  terminal.select(sel.col, sel.row + terminal.buffer.active.viewportY, sel.length)
}

function onTouchMove(e: TouchEvent) {
  if (!touch || !terminal || e.touches.length !== 1) return
  const t = e.touches[0]!
  if (touch.anchor) {
    e.preventDefault()
    selectTo(t.clientX, t.clientY)
    return
  }
  if (!touch.scrolling && Math.abs(t.clientY - touch.startY) < TAP_SLOP_PX) return
  touch.scrolling = true
  e.preventDefault() // no page rubber-banding while scrolling the terminal
  const box = screenBox()
  if (!box) return
  const step = (box.height / terminal.rows) * CELLS_PER_WHEEL_STEP
  touch.accum += t.clientY - touch.lastY
  touch.lastY = t.clientY
  // Finger down drags the content down: scroll back (negative wheel delta).
  while (Math.abs(touch.accum) >= step) {
    const back = touch.accum > 0
    touch.accum += back ? -step : step
    sendWheel(back ? -1 : 1, t.clientX, t.clientY)
  }
}

function onTouchEnd() {
  const selecting = touch?.anchor
  touch = null
  if (!selecting || !terminal) return
  const text = terminal.getSelection()
  if (!text) return
  // Called synchronously from touchend, so it counts as a user gesture.
  void copyText(text).then((ok) => {
    showClipNotice(ok ? 'Copied' : 'Copy failed')
    if (ok) selectMode.value = false
  })
}

// Links open in the browser chosen in the sidebar when it's set (see
// utils/linkBrowser); otherwise in a new tab.
function openLink(uri: string) {
  const target = browserUrl(uri, linkBrowser.value)
  if (target) {
    window.location.href = target
    return
  }
  const win = window.open(uri, '_blank', 'noopener,noreferrer')
  if (win) win.opener = null
}

function stopRightClickPropagation(e: MouseEvent) {
  if (e.button === 2) e.stopPropagation()
}

onMounted(() => {
  terminal = new Terminal({
    cursorBlink: true,
    // OSC 8 hyperlinks (Claude Code's blue links): open directly through
    // openLink. xterm's default asks with window.confirm, which a Home
    // Screen web app may never show, so tapping them did nothing.
    linkHandler: {
      activate: (_event, uri) => openLink(uri),
      allowNonHttpProtocols: false,
    },
    fontSize: 14,
    fontFamily: "'SF Mono', 'Cascadia Code', 'Fira Code', 'Menlo', monospace",
    theme: {
      background: '#0d1117',
      foreground: '#c9d1d9',
      cursor: '#58a6ff',
      selectionBackground: '#1f6feb66',
      black: '#484f58',
      red: '#ff7b72',
      green: '#3fb950',
      yellow: '#d29922',
      blue: '#58a6ff',
      magenta: '#bc8cff',
      cyan: '#39d353',
      white: '#c9d1d9',
      brightBlack: '#6e7681',
      brightRed: '#ffa198',
      brightGreen: '#56d364',
      brightYellow: '#e3b341',
      brightBlue: '#79c0ff',
      brightMagenta: '#d2a8ff',
      brightCyan: '#56d364',
      brightWhite: '#f0f6fc',
    },
    allowProposedApi: true,
    // With tmux mouse mode on, drags go to tmux. On macOS and iPadOS, xterm
    // only makes its own selection (copied with Cmd+C) when Option is held
    // and this is set; elsewhere Shift-drag already does.
    macOptionClickForcesSelection: true,
  })

  fitAddon = new FitAddon()
  terminal.loadAddon(fitAddon)
  // Plain URLs in the output (WebLinksAddon); OSC 8 links use the same
  // opener through the terminal's linkHandler option.
  terminal.loadAddon(new WebLinksAddon((_event, uri) => openLink(uri)))

  // OSC 52 clipboard writes from apps. Queries (`?`) are ignored, never
  // answered, so nothing in the session can read the user's clipboard.
  terminal.parser.registerOscHandler(52, (data) => {
    const text = parseOsc52(data)
    if (text !== null) void copyFromApp(text)
    return true
  })
  window.addEventListener('keydown', dismissClipNoticeOnEscape, true)
  setPasteHandler(() => void pasteFromClipboard())

  if (terminalRef.value) {
    terminal.open(terminalRef.value)
    if (terminal.textarea) markAsTerminalInput(terminal.textarea)
    loadWebglRenderer()
    requestAnimationFrame(() => {
      fitTerminal()
    })
    // Cell metrics depend on the font; measure again once fonts are loaded.
    document.fonts?.ready.then(() => scheduleSyncSize())

    // Intercept right-click before xterm.js can forward it to tmux as mouse button-3.
    // Without this, tmux's native menu appears underneath our HTML context menu.
    terminalRef.value.addEventListener('mousedown', stopRightClickPropagation, true)

    // Capture phase, so these run before xterm's own gesture handling.
    terminalRef.value.addEventListener('touchstart', onTouchStart, { passive: true, capture: true })
    terminalRef.value.addEventListener('touchmove', onTouchMove, { passive: false, capture: true })
    terminalRef.value.addEventListener('touchend', onTouchEnd, { capture: true })
    terminalRef.value.addEventListener('touchcancel', onTouchEnd, { capture: true })
  }

  // Handle user input — send raw bytes directly to PTY.
  // Apply sticky Ctrl/Alt modifiers set by MobileKeyBar.
  terminal.onData((data: string) => {
    if (ctrlActive.value && data.length === 1) {
      ctrlActive.value = false
      // e.g. 'c' → ^C (0x03); characters without a control code go as typed.
      const ctrl = controlByte(data)
      socket.sendInput(ctrl === null ? new TextEncoder().encode(data) : new Uint8Array([ctrl]))
      return
    }
    if (altActive.value && data.length === 1) {
      altActive.value = false
      socket.sendInput(new Uint8Array([0x1b, ...new TextEncoder().encode(data)]))
      return
    }
    socket.sendInput(new TextEncoder().encode(data))
  })

  // Handle binary input (for paste, etc.)
  terminal.onBinary((data: string) => {
    const bytes = new Uint8Array(data.length)
    for (let i = 0; i < data.length; i++) {
      bytes[i] = data.charCodeAt(i)
    }
    socket.sendInput(bytes)
  })

  // Receive PTY output — write raw bytes to xterm.js
  unsubOutput = socket.onOutput((data: Uint8Array) => {
    if (terminal) {
      terminal.write(glyphs.filter(data))
    }
  })

  // The session is attached again (after a reconnect): tmux repaints the
  // whole screen, so start from a clean terminal rather than draw over the
  // stale one.
  unsubReconnect = socket.onReconnect(() => {
    glyphs.reset()
    terminal?.reset()
  })

  // Resize observer — fit terminal to container, then tell server
  resizeObserver = new ResizeObserver(() => {
    scheduleSyncSize()
  })
  if (terminalRef.value) {
    resizeObserver.observe(terminalRef.value)
  }
})

// Watch sessionName changes to select/switch session
watch(
  () => props.sessionName,
  (newSession, oldSession) => {
    if (newSession) {
      // Select once laid out, with the fitted size, so the server attaches
      // at the right size instead of 80x24 and resizing a moment later.
      // Reset in the same frame, so output still arriving from the old
      // session isn't drawn after the reset.
      nextTick(() => {
        requestAnimationFrame(() => {
          if (props.sessionName !== newSession) return
          if (terminal && oldSession) {
            glyphs.reset()
            terminal.clear()
            terminal.reset()
          }
          const size = fitTerminal()
          if (size) lastSentSize = size
          socket.selectSession(newSession, size ?? undefined)
          // Type straight into the new session without clicking it first.
          // Take focus on desktop, or from the proxy that held it through the
          // tap on iPadOS; don't steal it from anything else (e.g. a rename
          // field in the sidebar).
          const active = document.activeElement
          if (!active || active === document.body || focusIsHeldForTerminal()) {
            terminal?.focus()
          }
        })
      })
    }
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  unsubOutput?.()
  unsubOutput = null
  unsubReconnect?.()
  unsubReconnect = null
  socket.clearSession()
  terminalRef.value?.removeEventListener('mousedown', stopRightClickPropagation, true)
  terminalRef.value?.removeEventListener('touchstart', onTouchStart, { capture: true })
  terminalRef.value?.removeEventListener('touchmove', onTouchMove, { capture: true })
  terminalRef.value?.removeEventListener('touchend', onTouchEnd, { capture: true })
  terminalRef.value?.removeEventListener('touchcancel', onTouchEnd, { capture: true })
  window.removeEventListener('keydown', dismissClipNoticeOnEscape, true)
  setPasteHandler(null)
  dismissClipNotice()
  if (resizeObserver) {
    resizeObserver.disconnect()
    resizeObserver = null
  }
  if (resizeTimer) {
    clearTimeout(resizeTimer)
    resizeTimer = null
  }
  webglAddon?.dispose()
  webglAddon = null
  if (terminal) {
    terminal.dispose()
    terminal = null
  }
  fitAddon = null
})
</script>

<template>
  <div class="terminal-container">
    <!-- Connection status banner -->
    <div v-if="!socket.connected.value" class="status-banner">
      <template v-if="socket.reconnecting.value">
        <span class="status-dot reconnecting" />
        Reconnecting...
      </template>
      <template v-else>
        <span class="status-dot disconnected" />
        Disconnected
      </template>
    </div>

    <!-- Error banner -->
    <div v-if="socket.error.value" class="error-banner">
      {{ socket.error.value }}
    </div>

    <!-- Terminal -->
    <div ref="terminalRef" class="terminal-element" @contextmenu="onContextMenu" />

    <!-- Clipboard notice; pointerdown.prevent keeps focus in the terminal -->
    <div v-if="clipNotice" class="clip-notice" role="status">
      <button
        v-if="clipNotice.pending"
        class="clip-notice-action"
        type="button"
        @pointerdown.prevent
        @click="copyPending"
      >
        {{ clipNotice.message }}
      </button>
      <span v-else>{{ clipNotice.message }}</span>
      <button
        v-if="clipNotice.pending"
        class="clip-notice-close"
        type="button"
        aria-label="Dismiss"
        @pointerdown.prevent
        @click="dismissClipNotice"
      >
        &times;
      </button>
    </div>

    <!-- Mobile key bar: Tab, Esc, arrows, sticky Ctrl/Alt, special chars -->
    <MobileKeyBar v-if="showKeyBar" />

    <TerminalContextMenu
      :visible="ctxVisible"
      :x="ctxX"
      :y="ctxY"
      :zoomed="isZoomed"
      @action="onCtxAction"
      @close="ctxVisible = false"
    />
  </div>
</template>

<style scoped>
.terminal-container {
  flex: 1;
  display: flex;
  flex-direction: column;
  background: #0d1117;
  position: relative;
  overflow: hidden;
}

.terminal-element {
  flex: 1;
  /* A flex item can't shrink below its content by default, so without these
     the terminal never gets shorter: xterm's rows hold the element open, the
     ResizeObserver sees no change, and tmux keeps the old row count. */
  min-height: 0;
  overflow: hidden;
  padding: 4px;
  /* juli handles finger gestures here (swipe to scroll, Select-mode drags).
     Without this, iPadOS claims the swipe for native scrolling of xterm's
     viewport and cancels the touch before juli sees it moving. */
  touch-action: none;
}

/* Ensure xterm fills the container */
.terminal-element :deep(.xterm) {
  height: 100%;
}
.terminal-element :deep(.xterm-viewport) {
  overflow-y: auto;
}

.clip-notice {
  position: absolute;
  top: 10px;
  right: 14px;
  z-index: 200;
  display: flex;
  align-items: center;
  gap: 6px;
  max-width: calc(100% - 28px);
  padding: 6px 10px;
  background: #1c2129;
  border: 1px solid #30363d;
  border-radius: 6px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.4);
  font-size: 12px;
  color: #c9d1d9;
}

.clip-notice-action {
  background: #1f6feb;
  border: 1px solid #388bfd;
  border-radius: 4px;
  color: #ffffff;
  font-size: 12px;
  padding: 4px 8px;
  cursor: pointer;
  text-align: left;
}

.clip-notice-close {
  background: none;
  border: none;
  color: #8b949e;
  font-size: 16px;
  line-height: 1;
  padding: 0 2px;
  cursor: pointer;
}

.status-banner {
  padding: 6px 16px;
  background: #1c2129;
  border-bottom: 1px solid #21262d;
  font-size: 12px;
  color: #d29922;
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.error-banner {
  padding: 6px 16px;
  background: #3d1117;
  border-bottom: 1px solid #ff7b7233;
  font-size: 12px;
  color: #ff7b72;
  flex-shrink: 0;
}

.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

.status-dot.reconnecting {
  background: #d29922;
  animation: pulse 1.5s ease-in-out infinite;
}

.status-dot.disconnected {
  background: #ff7b72;
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}
</style>
