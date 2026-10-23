<script setup lang="ts">
import { ref, watch, nextTick } from 'vue'
import type { Session } from '~/composables/useSessionTree'
import { useTimeAgo } from '~/composables/useTimeAgo'
import { useSessionApi } from '~/composables/useSessionApi'
import { useLinkBrowser } from '~/composables/useLinkBrowser'
import { useIdentity } from '~/composables/useIdentity'
import { LINK_BROWSERS, isStandaloneApp, type LinkBrowser } from '~/utils/linkBrowser'

const props = defineProps<{
  sessions: Session[]
  selectedSessionName: string | null
}>()

const emit = defineEmits<{
  select: [sessionName: string]
  selectWindow: [sessionName: string, windowIndex: number]
  error: [message: string]
}>()

const { tick, formatTimeAgo } = useTimeAgo()
const api = useSessionApi()
const { name: machineName } = useIdentity()

// Only an installed (Home Screen) web app opens links in an in-app browser,
// so that's the only place the choice is offered.
const showLinkSetting = isStandaloneApp()
const { linkBrowser, setLinkBrowser } = useLinkBrowser()
function onLinkBrowserChange(e: Event) {
  setLinkBrowser((e.target as HTMLSelectElement).value as LinkBrowser)
}
const { logout } = useLogout()

// --- State ---

const expandedSessions = ref<Set<string>>(new Set()) // keyed by session.id
const renamingSession = ref<string | null>(null) // session name being renamed
const renamingWindow = ref<string | null>(null) // "session:index" key
const renameValue = ref('')
const creatingSession = ref(false)
const newSessionName = ref('')

// Confirm dialog
const sessionRenameRef = ref<HTMLInputElement | null>(null)
const windowRenameRef = ref<HTMLInputElement | null>(null)
const confirmVisible = ref(false)
const confirmTitle = ref('')
const confirmMessage = ref('')
const confirmAction = ref<(() => Promise<void>) | null>(null)

// --- Helpers ---

const statusColors: Record<string, string> = {
  running: '#3fb950',
  idle: '#d29922',
  error: '#f85149',
  detached: '#484f58',
}

function statusColor(status?: string): string {
  return statusColors[status ?? ''] ?? '#484f58'
}

function toolLabel(toolType?: string): string {
  if (!toolType || toolType === 'unknown' || toolType === 'shell') return ''
  return toolType
}

function gitDisplay(session: Session): string {
  if (!session.gitRepo && !session.gitBranch) return ''
  const repo = session.gitRepo ?? ''
  const branch = session.gitBranch ?? ''
  if (repo && branch) return `${repo} @ ${branch}`
  if (branch) return branch
  return repo
}

function truncate(str: string, max: number): string {
  if (str.length <= max) return str
  return str.slice(0, max - 1) + '\u2026'
}

function isExpanded(sessionId: string): boolean {
  return expandedSessions.value.has(sessionId)
}

function toggleExpand(sessionId: string) {
  if (expandedSessions.value.has(sessionId)) {
    expandedSessions.value.delete(sessionId)
  } else {
    expandedSessions.value.add(sessionId)
  }
}

// --- Error handling ---

async function safeCall(fn: () => Promise<void>) {
  try {
    await fn()
  } catch (e: any) {
    emit('error', e.message ?? 'Operation failed')
  }
}

// --- Create session ---

function startCreateSession() {
  creatingSession.value = true
  newSessionName.value = ''
  nextTick(() => {
    const input = document.querySelector('.new-session-input') as HTMLInputElement
    input?.focus()
  })
}

async function commitCreateSession() {
  if (!creatingSession.value) return
  const name = newSessionName.value.trim()
  creatingSession.value = false
  if (!name) return
  await safeCall(async () => {
    await api.createSession(name)
    // Session may already be in the tree (WebSocket update arrived before API response).
    if (props.sessions.find(s => s.name === name)) {
      emit('select', name)
      return
    }
    const stop = watch(
      () => props.sessions.find(s => s.name === name),
      (s) => { if (s) { emit('select', name); stop() } },
    )
    setTimeout(stop, 5000)
  })
}

function cancelCreateSession() {
  creatingSession.value = false
}

// --- Rename session ---

function startRenameSession(name: string, e: Event) {
  e.stopPropagation()
  renamingSession.value = name
  renameValue.value = name
  nextTick(() => {
    sessionRenameRef.value?.focus()
    sessionRenameRef.value?.select()
  })
}

async function commitRenameSession(oldName: string) {
  const newName = renameValue.value.trim()
  renamingSession.value = null
  if (!newName || newName === oldName) return
  await safeCall(() => api.renameSession(oldName, newName))
}

function cancelRenameSession() {
  renamingSession.value = null
}

// --- Kill session ---

function promptKillSession(name: string, e: Event) {
  e.stopPropagation()
  confirmTitle.value = 'Kill session'
  confirmMessage.value = `Kill session "${name}"? All windows and processes will be terminated.`
  confirmAction.value = async () => {
    await safeCall(() => api.killSession(name))
  }
  confirmVisible.value = true
}

// --- Create window ---

async function handleCreateWindow(sessionName: string, e: Event) {
  e.stopPropagation()
  await safeCall(() => api.createWindow(sessionName))
}

// --- Rename window ---

function startRenameWindow(sessionName: string, windowIndex: number, windowName: string, e: Event) {
  e.stopPropagation()
  renamingWindow.value = `${sessionName}:${windowIndex}`
  renameValue.value = windowName
  nextTick(() => {
    windowRenameRef.value?.focus()
    windowRenameRef.value?.select()
  })
}

async function commitRenameWindow(sessionName: string, windowIndex: number, oldName: string) {
  const newName = renameValue.value.trim()
  renamingWindow.value = null
  if (!newName || newName === oldName) return
  await safeCall(() => api.renameWindow(sessionName, windowIndex, newName))
}

function cancelRenameWindow() {
  renamingWindow.value = null
}

// --- Kill window ---

function promptKillWindow(sessionName: string, windowIndex: number, windowName: string, e: Event) {
  e.stopPropagation()
  confirmTitle.value = 'Close window'
  confirmMessage.value = `Close window "${windowName}" in session "${sessionName}"?`
  confirmAction.value = async () => {
    await safeCall(() => api.killWindow(sessionName, windowIndex))
  }
  confirmVisible.value = true
}

// --- Confirm dialog ---

async function handleConfirm() {
  confirmVisible.value = false
  if (confirmAction.value) {
    await confirmAction.value()
    confirmAction.value = null
  }
}

function handleCancel() {
  confirmVisible.value = false
  confirmAction.value = null
}
</script>

<template>
  <aside class="session-tree">
    <div class="tree-header">
      <span class="brand">
        Juli
        <span v-if="machineName" class="machine-name">{{ machineName }}</span>
      </span>
      <div class="tree-header-actions">
        <button class="btn-add" title="New session" @click="startCreateSession">+</button>
        <button class="btn-logout" title="Logout" @click="logout">&#x23FB;</button>
      </div>
    </div>

    <!-- Create session input -->
    <div v-if="creatingSession" class="new-session-row">
      <input
        v-model="newSessionName"
        class="new-session-input"
        placeholder="Session name"
        @keydown.enter="commitCreateSession"
        @keydown.escape="cancelCreateSession"
        @blur="commitCreateSession"
      />
    </div>

    <div v-if="sessions.length === 0 && !creatingSession" class="tree-empty">
      No sessions found
    </div>

    <div
      v-for="(session, index) in sessions"
      :key="session.id"
      class="session-group"
    >
      <div
        class="session-node"
        :class="{ selected: session.name === selectedSessionName }"
        @click="emit('select', session.name)"
      >
        <!-- Row 1: status dot + tool badge + time ago -->
        <div class="session-header">
          <span class="status-dot" :style="{ background: statusColor(session.status) }" :title="session.status ?? 'unknown'" />
          <span v-if="toolLabel(session.toolType)" class="tool-badge">{{ toolLabel(session.toolType) }}</span>
          <span v-if="session.lastActivity" class="time-ago" :data-tick="tick">{{ formatTimeAgo(session.lastActivity) }}</span>

          <span class="session-actions">
            <button class="btn-icon" title="Rename" @click="startRenameSession(session.name, $event)">&#9998;</button>
            <button class="btn-icon btn-icon-danger" title="Kill session" @click="promptKillSession(session.name, $event)">&times;</button>
          </span>
        </div>

        <!-- Row 2: session name (or rename input) -->
        <div class="session-info">
          <span class="session-index">{{ index + 1 }}.</span>
          <template v-if="renamingSession === session.name">
            <input
              ref="sessionRenameRef"
              v-model="renameValue"
              class="rename-input"
              @keydown.enter="commitRenameSession(session.name)"
              @keydown.escape="cancelRenameSession"
              @blur="commitRenameSession(session.name)"
              @click.stop
            />
          </template>
          <span v-else class="session-name">{{ session.name }}</span>
        </div>

        <!-- Row 3: git info -->
        <div v-if="gitDisplay(session)" class="session-git" :title="gitDisplay(session)">
          {{ truncate(gitDisplay(session), 30) }}
        </div>

        <!-- Row 4: window count (clickable to expand) -->
        <div class="session-meta" @click.stop="toggleExpand(session.id)">
          <span class="expand-icon">{{ isExpanded(session.id) ? '\u25BE' : '\u25B8' }}</span>
          <span class="window-count">{{ session.windows.length }} window{{ session.windows.length !== 1 ? 's' : '' }}</span>
        </div>
      </div>

      <!-- Expanded window list -->
      <div v-if="isExpanded(session.id)" class="window-list">
        <div
          v-for="w in session.windows"
          :key="w.id"
          class="window-node window-selectable"
          @click="emit('selectWindow', session.name, w.index)"
        >
          <template v-if="renamingWindow === `${session.name}:${w.index}`">
            <input
              ref="windowRenameRef"
              v-model="renameValue"
              class="rename-input"
              @keydown.enter="commitRenameWindow(session.name, w.index, w.name)"
              @keydown.escape="cancelRenameWindow"
              @blur="commitRenameWindow(session.name, w.index, w.name)"
              @click.stop
            />
          </template>
          <template v-else>
            <span class="window-index">{{ w.index }}:</span>
            <span class="window-name">{{ w.name }}</span>
            <span v-if="w.active" class="window-active">*</span>
          </template>
          <span class="window-actions">
            <button class="btn-icon" title="Rename" @click="startRenameWindow(session.name, w.index, w.name, $event)">&#9998;</button>
            <button class="btn-icon btn-icon-danger" title="Close window" @click="promptKillWindow(session.name, w.index, w.name, $event)">&times;</button>
          </span>
        </div>
        <div class="window-node window-add" @click.stop="handleCreateWindow(session.name, $event)">
          <span class="window-add-icon">+</span>
          <span class="window-add-label">New window</span>
        </div>
      </div>
    </div>

    <div v-if="showLinkSetting" class="tree-footer">
      <label class="link-setting">
        Open links in
        <select :value="linkBrowser" @change="onLinkBrowserChange">
          <option v-for="b in LINK_BROWSERS" :key="b.value" :value="b.value">{{ b.label }}</option>
        </select>
      </label>
    </div>

    <ConfirmDialog
      :visible="confirmVisible"
      :title="confirmTitle"
      :message="confirmMessage"
      confirm-label="Confirm"
      :danger="true"
      @confirm="handleConfirm"
      @cancel="handleCancel"
    />
  </aside>
</template>

<style scoped>
.session-tree {
  width: 240px;
  flex-shrink: 0;
  background: #161b22;
  border-right: 1px solid #21262d;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  user-select: none;
}

.tree-footer {
  margin-top: auto;
  padding: 10px 16px;
  border-top: 1px solid #21262d;
}

.link-setting {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  font-size: 11px;
  color: #8b949e;
}

.link-setting select {
  background: #0d1117;
  color: #c9d1d9;
  border: 1px solid #30363d;
  border-radius: 4px;
  /* 16px keeps iPadOS from zooming the page when the picker gets focus. */
  font-size: 16px;
  padding: 2px 4px;
}

.tree-header {
  padding: 12px 16px;
  border-bottom: 1px solid #21262d;
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.brand {
  font-weight: 700;
  font-size: 14px;
  color: #58a6ff;
  letter-spacing: -0.02em;
}

.machine-name {
  display: block;
  font-weight: 400;
  font-size: 11px;
  color: #8b949e;
  letter-spacing: 0;
}

.tree-header-actions {
  display: flex;
  align-items: center;
  gap: 6px;
}

.btn-add {
  background: none;
  border: 1px solid #30363d;
  color: #8b949e;
  font-size: 16px;
  width: 24px;
  height: 24px;
  border-radius: 4px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  line-height: 1;
  transition: background 0.1s, color 0.1s;
}

.btn-add:hover {
  background: #21262d;
  color: #e6edf3;
}

.btn-logout {
  background: none;
  border: 1px solid #30363d;
  color: #484f58;
  font-size: 13px;
  width: 24px;
  height: 24px;
  border-radius: 4px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  line-height: 1;
  transition: background 0.1s, color 0.1s;
}

.btn-logout:hover {
  background: #da363322;
  color: #f85149;
  border-color: #f8514933;
}

.new-session-row {
  padding: 8px 16px;
  border-bottom: 1px solid #21262d;
}

.new-session-input {
  width: 100%;
  background: #0d1117;
  border: 1px solid #30363d;
  color: #e6edf3;
  font-size: 12px;
  padding: 4px 8px;
  border-radius: 4px;
  outline: none;
  font-family: inherit;
}

.new-session-input:focus {
  border-color: #58a6ff;
}

.tree-empty {
  padding: 16px;
  color: #484f58;
  font-size: 12px;
}

.session-group {
  border-bottom: 1px solid #21262d;
}

.session-node {
  padding: 10px 16px;
  cursor: pointer;
  transition: background 0.1s;
}

.session-node:hover {
  background: #1c2129;
}

.session-node.selected {
  background: #1f6feb22;
  border-left: 2px solid #58a6ff;
  padding-left: 14px;
}

.session-header {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 4px;
}

.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

.tool-badge {
  font-size: 10px;
  font-family: 'SF Mono', 'Fira Code', monospace;
  color: #8b949e;
  background: #21262d;
  padding: 1px 6px;
  border-radius: 3px;
  line-height: 1.4;
}

.time-ago {
  font-size: 10px;
  color: #484f58;
  margin-left: auto;
}

.session-actions {
  display: none;
  gap: 2px;
  margin-left: 4px;
}

.session-node:hover .session-actions {
  display: flex;
}

.btn-icon {
  background: none;
  border: none;
  color: #484f58;
  cursor: pointer;
  font-size: 13px;
  padding: 0 3px;
  line-height: 1;
  border-radius: 3px;
  transition: color 0.1s, background 0.1s;
}

.btn-icon:hover {
  color: #e6edf3;
  background: #30363d;
}

.btn-icon-danger:hover {
  color: #f85149;
  background: #da363322;
}

.session-info {
  display: flex;
  align-items: center;
  gap: 6px;
}

.session-index {
  color: #484f58;
  font-size: 12px;
  min-width: 16px;
}

.session-name {
  font-weight: 600;
  font-size: 13px;
  color: #e6edf3;
}

.rename-input {
  background: #0d1117;
  border: 1px solid #58a6ff;
  color: #e6edf3;
  font-size: 12px;
  padding: 2px 6px;
  border-radius: 3px;
  outline: none;
  width: 140px;
  font-family: inherit;
}

.session-git {
  margin-top: 3px;
  padding-left: 22px;
  font-size: 11px;
  color: #8b949e;
  font-family: 'SF Mono', 'Fira Code', monospace;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.session-meta {
  margin-top: 2px;
  padding-left: 22px;
  display: flex;
  align-items: center;
  gap: 4px;
  cursor: pointer;
}

.expand-icon {
  font-size: 10px;
  color: #484f58;
}

.window-count {
  font-size: 11px;
  color: #484f58;
}

.session-meta:hover .window-count,
.session-meta:hover .expand-icon {
  color: #8b949e;
}

/* Window list */

.window-list {
  padding: 0 0 4px 0;
  background: #0d1117;
}

.window-node {
  padding: 4px 16px 4px 38px;
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: #8b949e;
}

.window-selectable {
  cursor: pointer;
}

.window-node:hover {
  background: #161b22;
}

.window-index {
  color: #484f58;
  font-family: 'SF Mono', 'Fira Code', monospace;
}

.window-name {
  color: #c9d1d9;
}

.window-active {
  color: #3fb950;
  font-size: 10px;
}

.window-actions {
  display: none;
  gap: 2px;
  margin-left: auto;
}

.window-node:hover .window-actions {
  display: flex;
}

.window-add {
  cursor: pointer;
  color: #484f58;
}

.window-add:hover {
  color: #8b949e;
}

.window-add-icon {
  font-size: 14px;
}

.window-add-label {
  font-size: 11px;
}
</style>
