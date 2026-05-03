<script setup lang="ts">
import { ref, watch, onMounted, onBeforeUnmount } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import '@xterm/xterm/css/xterm.css'

import { useTerminalSocket } from '~/composables/useTerminalSocket'
import { useSessionApi } from '~/composables/useSessionApi'
import { useViewportMode } from '~/composables/useViewportMode'
import { ctrlActive, altActive } from '~/composables/useMobileKeyModifiers'

const props = defineProps<{
  sessionName: string | null
}>()

const emit = defineEmits<{
  error: [message: string]
}>()

const terminalRef = ref<HTMLDivElement | null>(null)
const socket = useTerminalSocket()
const sessionTree = useSessionTree()
const api = useSessionApi()
const { isMobile } = useViewportMode(1024)

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

let terminal: Terminal | null = null
let fitAddon: FitAddon | null = null
let resizeObserver: ResizeObserver | null = null
let unsubOutput: (() => void) | null = null

function stopRightClickPropagation(e: MouseEvent) {
  if (e.button === 2) e.stopPropagation()
}

onMounted(() => {
  terminal = new Terminal({
    cursorBlink: true,
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
  })

  fitAddon = new FitAddon()
  terminal.loadAddon(fitAddon)
  terminal.loadAddon(new WebLinksAddon())

  if (terminalRef.value) {
    terminal.open(terminalRef.value)
    requestAnimationFrame(() => {
      fitAddon?.fit()
    })

    // Intercept right-click before xterm.js can forward it to tmux as mouse button-3.
    // Without this, tmux's native menu appears underneath our HTML context menu.
    terminalRef.value.addEventListener('mousedown', stopRightClickPropagation, true)
  }

  // Handle user input — send raw bytes directly to PTY.
  // Apply sticky Ctrl/Alt modifiers set by MobileKeyBar.
  terminal.onData((data: string) => {
    if (ctrlActive.value && data.length === 1) {
      ctrlActive.value = false
      const code = data.charCodeAt(0)
      // Map printable/uppercase to control code (e.g. 'c' → ^C = 0x03)
      const ctrl = code >= 64 ? code & 0x1f : code
      socket.sendInput(new Uint8Array([ctrl]))
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
      terminal.write(data)
    }
  })

  // Resize observer — fit terminal to container, then tell server
  resizeObserver = new ResizeObserver(() => {
    requestAnimationFrame(() => {
      if (fitAddon && terminal) {
        fitAddon.fit()
        socket.sendResize(terminal.cols, terminal.rows)
      }
    })
  })
  if (terminalRef.value) {
    resizeObserver.observe(terminalRef.value)
  }
})

// Watch sessionName changes to select/switch session
watch(
  () => props.sessionName,
  (newSession, oldSession) => {
    if (terminal && oldSession) {
      terminal.clear()
      terminal.reset()
    }

    if (newSession) {
      socket.selectSession(newSession)
      // Send resize after selecting so the PTY gets correct dimensions
      nextTick(() => {
        requestAnimationFrame(() => {
          if (fitAddon && terminal) {
            fitAddon.fit()
            socket.sendResize(terminal.cols, terminal.rows)
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
  terminalRef.value?.removeEventListener('mousedown', stopRightClickPropagation, true)
  if (resizeObserver) {
    resizeObserver.disconnect()
    resizeObserver = null
  }
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

    <!-- Mobile key bar: Tab, Esc, arrows, sticky Ctrl/Alt, special chars -->
    <MobileKeyBar v-if="isMobile" />

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
  padding: 4px;
}

/* Ensure xterm fills the container */
.terminal-element :deep(.xterm) {
  height: 100%;
}
.terminal-element :deep(.xterm-viewport) {
  overflow-y: auto;
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
