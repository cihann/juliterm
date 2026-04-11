<script setup lang="ts">
import { ref, onBeforeUnmount } from 'vue'
import { useViewportMode } from '~/composables/useViewportMode'

definePageMeta({
  middleware: 'auth',
})

const sessionTree = useSessionTree()
const socket = useTerminalSocket()
const notifications = useNotifications()
const { isMobile } = useViewportMode()
const errorMessage = ref<string | null>(null)

function showError(msg: string) {
  errorMessage.value = msg
  setTimeout(() => { errorMessage.value = null }, 4000)
}

// Register session tree callback immediately — no timer hack needed.
// The singleton buffers the last session tree and replays on subscribe,
// so ordering relative to WebSocket connect is irrelevant.
const unsubSessionTree = socket.onSessionTree((data: unknown[]) => {
  sessionTree.updateFromMessage(data)
  notifications.processSessionUpdate(data, sessionTree.selectedSessionName.value, handleSelectSession)
})

const unsubError = socket.onError((err) => {
  console.error('[juli] server error:', err.code, err.message)
  if (err.code === 'session_ended') {
    sessionTree.clearSelection()
    showError('Session terminated')
  }
})

async function fetchSessions() {
  // Only fetch via HTTP if WebSocket hasn't delivered data yet
  if (sessionTree.sessions.value.length > 0) return
  try {
    const res = await fetch('/api/sessions')
    if (res.ok) {
      const data = await res.json()
      sessionTree.updateFromMessage(data)
    }
  } catch {
    // Will retry via WebSocket once connected
  }
}

onMounted(() => {
  // Connect WebSocket AFTER auth middleware has validated the cookie.
  socket.connect()
  fetchSessions()
  notifications.requestPermission()
})

onBeforeUnmount(() => {
  unsubSessionTree()
  unsubError()
})

function handleSelectSession(sessionName: string) {
  sessionTree.selectSession(sessionName)
}

function handleSelectWindow(sessionName: string, windowIndex: number) {
  // If not attached to this session yet, attach first.
  if (sessionTree.selectedSessionName.value !== sessionName) {
    sessionTree.selectSession(sessionName)
  }
  socket.selectWindow(sessionName, windowIndex)
}

function handleReturnToGrid() {
  sessionTree.clearSelection()
}

const isConnected = computed(() => {
  return socket.connected.value
})
</script>

<template>
  <div class="layout">
    <div v-if="errorMessage" class="error-toast">{{ errorMessage }}</div>

    <!-- Grid mode: no session selected -->
    <SessionGrid
      v-if="!sessionTree.selectedSessionName.value"
      :sessions="sessionTree.sessions.value"
      @select="handleSelectSession"
      @error="showError"
    />

    <!-- Terminal mode: session attached -->
    <div v-else class="main-area" :class="{ 'main-area--mobile': isMobile }">
      <SessionTree
        v-if="!isMobile"
        :sessions="sessionTree.sessions.value"
        :selected-session-name="sessionTree.selectedSessionName.value"
        @select="handleSelectSession"
        @select-window="handleSelectWindow"
        @error="showError"
      />
      <div class="content-area">
        <ViewerBar
          :session-name="sessionTree.selectedSessionName.value"
          :connected="isConnected"
          :compact="isMobile"
          :sessions="sessionTree.sessions.value"
          @back="handleReturnToGrid"
          @error="showError"
        />
        <TerminalView
          v-if="sessionTree.selectedSessionName.value"
          :session-name="sessionTree.selectedSessionName.value"
          @error="showError"
        />
      </div>
    </div>
  </div>
</template>

<style scoped>
.layout {
  height: 100vh;
  height: 100dvh;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.main-area {
  flex: 1;
  display: flex;
  overflow: hidden;
}

.content-area {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.error-toast {
  position: fixed;
  top: 12px;
  left: 50%;
  transform: translateX(-50%);
  background: #da3633;
  color: #fff;
  font-size: 13px;
  padding: 8px 20px;
  border-radius: 6px;
  z-index: 1100;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.4);
}
</style>
