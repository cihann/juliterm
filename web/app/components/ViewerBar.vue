<script setup lang="ts">
import { computed } from 'vue'
import type { Session } from '~/composables/useSessionTree'
import { useSessionApi } from '~/composables/useSessionApi'

const props = defineProps<{
  sessionName: string | null
  connected: boolean
  compact?: boolean
  sessions?: Session[]
  sidebarCollapsed?: boolean
}>()

const emit = defineEmits<{
  back: []
  error: [message: string]
  toggleSidebar: []
}>()

const api = useSessionApi()

const hasMultiplePanes = computed(() => {
  if (!props.sessionName || !props.sessions) return false
  const session = props.sessions.find(s => s.name === props.sessionName)
  const activeWindow = session?.windows.find(w => w.active)
  return (activeWindow?.panes.length ?? 0) >= 2
})

async function handleZoom() {
  if (!props.sessionName) return
  try {
    await api.zoomPane(props.sessionName)
  }
  catch (e: any) {
    emit('error', e.message || 'Zoom failed')
  }
}
</script>

<template>
  <header class="viewer-bar" :class="{ compact }">
    <div class="viewer-bar-left">
      <button
        v-if="!compact"
        class="sidebar-toggle"
        :class="{ 'sidebar-toggle--collapsed': sidebarCollapsed }"
        type="button"
        :title="sidebarCollapsed ? 'Show sidebar' : 'Hide sidebar'"
        :aria-pressed="!sidebarCollapsed"
        @click="emit('toggleSidebar')"
      >
        <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor">
          <path d="M2 2h12a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1H2a1 1 0 0 1-1-1V3a1 1 0 0 1 1-1zm0 1v10h3V3H2zm4 0v10h8V3H6z" />
        </svg>
      </button>
      <button class="back-button" type="button" @click="emit('back')">
        Sessions
      </button>
      <span v-if="sessionName" class="session-info">
        <span class="status-dot" :class="{ connected }" />
        <span class="session-name" :class="{ 'session-name--compact': compact }">{{ sessionName }}</span>
      </span>
    </div>
    <div class="viewer-bar-right">
      <SplitDropdown
        v-if="sessionName && !compact"
        :session-name="sessionName"
        @error="emit('error', $event)"
      />
      <button
        v-if="compact && hasMultiplePanes"
        class="zoom-button"
        type="button"
        title="Toggle pane zoom"
        @click="handleZoom"
      >
        <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor">
          <path d="M1 1h5v2H3.414L6.7 6.293l-1.414 1.414L2 4.414V6H0V1h1zm14 0h-5v2h2.586L9.3 6.293l1.414 1.414L14 4.414V6h2V1h-1zM1 15h5v-2H3.414l3.293-3.293-1.414-1.414L2 11.586V10H0v5h1zm14 0h-5v-2h2.586l-3.293-3.293 1.414-1.414L14 11.586V10h2v5h-1z" />
        </svg>
      </button>
    </div>
  </header>
</template>

<style scoped>
.viewer-bar {
  height: 36px;
  min-height: 36px;
  background: #161b22;
  border-bottom: 1px solid #21262d;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 16px;
  font-size: 12px;
}

.viewer-bar-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

.viewer-bar-left {
  display: flex;
  align-items: center;
  gap: 16px;
}

.sidebar-toggle {
  height: 24px;
  width: 24px;
  margin-right: -8px;
  border: 1px solid #30363d;
  border-radius: 4px;
  background: none;
  color: #e6edf3;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background 0.1s, color 0.1s;
}

.sidebar-toggle:hover {
  background: #21262d;
}

.sidebar-toggle:focus-visible {
  outline: 2px solid #1f6feb;
  outline-offset: 2px;
}

.sidebar-toggle--collapsed {
  color: #8b949e;
}

.back-button {
  height: 24px;
  padding: 0 10px;
  border: 1px solid #30363d;
  border-radius: 6px;
  background: #21262d;
  color: #e6edf3;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
}

.back-button:hover {
  background: #30363d;
}

.back-button:focus-visible {
  outline: 2px solid #1f6feb;
  outline-offset: 2px;
}

.session-info {
  display: flex;
  align-items: center;
  gap: 8px;
}

.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #484f58;
  flex-shrink: 0;
}

.status-dot.connected {
  background: #3fb950;
}

.session-name {
  font-weight: 600;
  color: #e6edf3;
}

.session-name--compact {
  max-width: 150px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.viewer-bar.compact {
  padding: 0 8px;
}

.compact .viewer-bar-left {
  gap: 8px;
}

.zoom-button {
  height: 28px;
  width: 28px;
  border: 1px solid #30363d;
  border-radius: 6px;
  background: #21262d;
  color: #e6edf3;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
}

.zoom-button:hover {
  background: #30363d;
}

.zoom-button:active {
  background: #1f6feb;
}
</style>
