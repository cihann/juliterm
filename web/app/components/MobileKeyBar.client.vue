<script setup lang="ts">
import { useTerminalSocket } from '~/composables/useTerminalSocket'
import { ctrlActive, altActive } from '~/composables/useMobileKeyModifiers'
import { selectMode, requestPaste } from '~/composables/useTerminalTouch'

const socket = useTerminalSocket()

function enc(s: string): Uint8Array {
  return new TextEncoder().encode(s)
}

function send(bytes: Uint8Array) {
  if (bytes.length > 0) socket.sendInput(bytes)
}

function toggleCtrl() {
  ctrlActive.value = !ctrlActive.value
  altActive.value = false
}

function toggleAlt() {
  altActive.value = !altActive.value
  ctrlActive.value = false
}

function toggleSelect() {
  selectMode.value = !selectMode.value
}

type Key = {
  label: string
  action: () => void
  wide?: boolean
  icon?: boolean
  // Run on click instead of pointerdown: on touch screens only pointerup /
  // click count as a user gesture, which clipboard access needs.
  onClick?: boolean
}

const keys: Key[] = [
  { label: 'Esc',  action: () => send(new Uint8Array([0x1b])) },
  { label: 'Select', action: toggleSelect, wide: true },
  { label: 'Paste', action: requestPaste, wide: true, onClick: true },
  { label: 'Tab',  action: () => send(new Uint8Array([0x09])) },
  { label: 'Ctrl', action: toggleCtrl },
  { label: 'Alt',  action: toggleAlt },
  { label: '←',   action: () => send(enc('\x1b[D')), icon: true },
  { label: '↑',   action: () => send(enc('\x1b[A')), icon: true },
  { label: '↓',   action: () => send(enc('\x1b[B')), icon: true },
  { label: '→',   action: () => send(enc('\x1b[C')), icon: true },
  { label: 'PgUp', action: () => send(enc('\x1b[5~')), wide: true },
  { label: 'PgDn', action: () => send(enc('\x1b[6~')), wide: true },
  { label: 'Home', action: () => send(enc('\x1b[H')), wide: true },
  { label: 'End',  action: () => send(enc('\x1b[F')), wide: true },
  { label: '|',   action: () => send(enc('|')) },
  { label: '~',   action: () => send(enc('~')) },
  { label: '/',   action: () => send(enc('/')) },
  { label: '\\',  action: () => send(enc('\\')) },
  { label: '-',   action: () => send(enc('-')) },
  { label: '_',   action: () => send(enc('_')) },
]
</script>

<template>
  <div class="key-bar" role="toolbar" aria-label="Terminal key bar">
    <button
      v-for="key in keys"
      :key="key.label"
      class="key"
      :class="{
        'key--wide': key.wide,
        'key--active': (key.label === 'Ctrl' && ctrlActive) || (key.label === 'Alt' && altActive) || (key.label === 'Select' && selectMode),
        'key--modifier': key.label === 'Ctrl' || key.label === 'Alt' || key.label === 'Select',
      }"
      :aria-pressed="key.label === 'Select' ? selectMode : undefined"
      @pointerdown.prevent="!key.onClick && key.action()"
      @click="key.onClick && key.action()"
    >
      {{ key.label }}
    </button>
  </div>
</template>

<style scoped>
.key-bar {
  display: flex;
  flex-direction: row;
  overflow-x: auto;
  overflow-y: hidden;
  scrollbar-width: none;
  background: #161b22;
  border-top: 1px solid #30363d;
  padding: 4px 6px;
  gap: 5px;
  /* A plain bottom row of the terminal column: the page is sized to the area
     above the keyboard, so this already sits just above it. (Sticky
     positioning moved with Safari's page scroll and floated over the
     terminal.) */
  flex-shrink: 0;
}

.key-bar::-webkit-scrollbar {
  display: none;
}

.key {
  flex-shrink: 0;
  min-width: 36px;
  height: 32px;
  padding: 0 8px;
  background: #21262d;
  border: 1px solid #30363d;
  border-radius: 6px;
  color: #c9d1d9;
  font-family: 'SF Mono', 'Cascadia Code', 'Fira Code', 'Menlo', monospace;
  font-size: 13px;
  cursor: pointer;
  user-select: none;
  -webkit-user-select: none;
  touch-action: manipulation;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background 0.1s, border-color 0.1s;
  -webkit-tap-highlight-color: transparent;
}

.key:active {
  background: #30363d;
}

.key--wide {
  min-width: 46px;
}

.key--modifier {
  color: #8b949e;
}

.key--active {
  background: #1f6feb;
  border-color: #388bfd;
  color: #ffffff;
}
</style>
