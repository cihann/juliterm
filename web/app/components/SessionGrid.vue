<script setup lang="ts">
import { ref, watch, nextTick } from 'vue'
import type { Session } from '~/composables/useSessionTree'
import { useTimeAgo } from '~/composables/useTimeAgo'
import { useSessionApi } from '~/composables/useSessionApi'
import { useLogout } from '~/composables/useLogout'

const props = defineProps<{
  sessions: Session[]
}>()

const emit = defineEmits<{
  select: [sessionName: string]
  error: [message: string]
}>()

const { tick, formatTimeAgo } = useTimeAgo()
const api = useSessionApi()
const { logout } = useLogout()

// --- State ---

const renamingSession = ref<string | null>(null)
const renameValue = ref('')
const creatingSession = ref(false)
const newSessionName = ref('')
const renameInputRef = ref<HTMLInputElement | null>(null)

// Confirm dialog
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

const activityLabels: Record<string, string> = {
  needs_attention: 'Needs attention',
  executing: 'Running',
  coding: 'Coding',
  waiting: 'Waiting',
  idle: 'Idle',
  detached: 'Detached',
  unknown: 'Unknown',
}

function statusColor(status?: string): string {
  return statusColors[status ?? ''] ?? '#484f58'
}

function activityLabel(activityState?: Session['activityState']): string {
  if (!activityState) return ''
  return activityLabels[activityState] ?? 'Unknown'
}

function activityCardClass(activityState?: Session['activityState']): string {
  return activityState === 'needs_attention' ? 'session-card--attention' : ''
}

function activityBadgeClass(activityState?: Session['activityState']): string {
  return activityState ? `activity-badge--${activityState}` : ''
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
    const el = Array.isArray(renameInputRef.value) ? renameInputRef.value[0] : renameInputRef.value
    el?.focus()
    el?.select()
  })
}

async function commitRenameSession(oldName: string) {
  const newName = renameValue.value.trim()
  renamingSession.value = null
  if (!newName || newName === oldName) return
  await safeCall(() => api.renameSession(oldName, newName))
}

function cancelRenameSession() {
  renameValue.value = renamingSession.value ?? ''
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
  <div class="grid-container">
    <div class="grid-header">
      <span class="brand">Juli</span>
      <div class="grid-header-actions">
        <button class="btn-add" title="New session" @click="startCreateSession">+</button>
        <button class="btn-logout" title="Logout" @click="logout">&#x23FB;</button>
      </div>
    </div>

    <!-- Empty state -->
    <div v-if="sessions.length === 0 && !creatingSession" class="grid-empty">
      <div class="empty-inner">
        <h2>No sessions</h2>
        <p>Create a tmux session to get started</p>
        <button class="btn-create" @click="startCreateSession">+ New session</button>
      </div>
    </div>

    <!-- Grid body -->
    <div v-else class="grid-body">
      <!-- Session cards -->
      <div
        v-for="session in sessions"
        :key="session.id"
        class="session-card"
        :class="activityCardClass(session.activityState)"
        @click="renamingSession !== session.name && emit('select', session.name)"
      >
        <!-- Row 1: status + tool + time + actions -->
        <div class="card-header">
          <span class="status-dot" :style="{ background: statusColor(session.status) }" :title="session.status ?? 'unknown'" />
          <span v-if="toolLabel(session.toolType)" class="tool-badge">{{ toolLabel(session.toolType) }}</span>
          <span v-if="session.lastActivity" class="time-ago" :data-tick="tick">{{ formatTimeAgo(session.lastActivity) }}</span>
          <span class="card-actions">
            <button class="btn-icon" title="Rename" @click="startRenameSession(session.name, $event)">&#9998;</button>
            <button class="btn-icon btn-icon-danger" title="Kill session" @click="promptKillSession(session.name, $event)">&times;</button>
          </span>
        </div>

        <!-- Row 2: session name -->
        <div class="card-name">
          <template v-if="renamingSession === session.name">
            <input
              ref="renameInputRef"
              v-model="renameValue"
              class="rename-input"
              @keydown.enter="commitRenameSession(session.name)"
              @keydown.escape.stop="cancelRenameSession"
              @blur="commitRenameSession(session.name)"
              @click.stop
            />
          </template>
          <span v-else>{{ session.name }}</span>
        </div>

        <!-- Row 3: activity -->
        <div v-if="session.activityState || session.activitySummary" class="card-activity">
          <span
            v-if="session.activityState"
            class="activity-badge"
            :class="activityBadgeClass(session.activityState)"
          >
            {{ activityLabel(session.activityState) }}
          </span>
          <div v-if="session.activitySummary" class="activity-summary" :title="session.activitySummary">
            {{ session.activitySummary }}
          </div>
        </div>

        <!-- Row 4: git info -->
        <div v-if="gitDisplay(session)" class="card-git" :title="gitDisplay(session)">
          {{ truncate(gitDisplay(session), 40) }}
        </div>

        <!-- Row 5: working directory -->
        <div v-if="session.workingDir" class="card-dir" :title="session.workingDir">
          {{ truncate(session.workingDir, 40) }}
        </div>

        <!-- Row 6: window count -->
        <div class="card-meta">
          {{ session.windows.length }} window{{ session.windows.length !== 1 ? 's' : '' }}
        </div>
      </div>

      <!-- New session card -->
      <div v-if="!creatingSession" class="session-card new-session-card" @click="startCreateSession">
        <div class="new-session-inner">
          <span class="new-session-icon">+</span>
          <span class="new-session-label">New session</span>
        </div>
      </div>

      <!-- Create session input card -->
      <div v-else class="session-card create-session-card">
        <div class="create-inner">
          <label class="create-label">Session name</label>
          <input
            v-model="newSessionName"
            class="new-session-input"
            placeholder="my-session"
            @keydown.enter="commitCreateSession"
            @keydown.escape="cancelCreateSession"
            @blur="commitCreateSession"
          />
        </div>
      </div>
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
  </div>
</template>

<style scoped>
.grid-container {
  flex: 1;
  display: flex;
  flex-direction: column;
  background: #0d1117;
  overflow: hidden;
}

.grid-header {
  padding: 12px 24px;
  border-bottom: 1px solid #21262d;
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
}

.brand {
  font-weight: 700;
  font-size: 14px;
  color: #58a6ff;
  letter-spacing: -0.02em;
}

.grid-header-actions {
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

/* Empty state */

.grid-empty {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}

.empty-inner {
  text-align: center;
}

.empty-inner h2 {
  font-size: 20px;
  font-weight: 600;
  color: #e6edf3;
  margin: 0 0 8px;
}

.empty-inner p {
  font-size: 14px;
  color: #484f58;
  margin: 0 0 20px;
}

.btn-create {
  background: #1f6feb;
  border: none;
  color: #fff;
  font-size: 13px;
  padding: 8px 16px;
  border-radius: 6px;
  cursor: pointer;
  font-family: inherit;
  transition: background 0.1s;
}

.btn-create:hover {
  background: #388bfd;
}

/* Grid body */

.grid-body {
  flex: 1;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 16px;
  padding: 24px;
  overflow-y: auto;
  align-content: start;
}

/* Session cards */

.session-card {
  background: #161b22;
  border: 1px solid #21262d;
  border-radius: 8px;
  padding: 16px;
  cursor: pointer;
  transition: border-color 0.1s, background 0.1s;
}

.session-card:hover {
  border-color: #30363d;
  background: #1c2129;
}

.session-card--attention {
  border-color: #f85149;
  box-shadow: 0 0 0 1px #f8514933;
}

.session-card--attention:hover {
  border-color: #ff7b72;
}

.card-header {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 10px;
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

.card-actions {
  display: none;
  gap: 2px;
  margin-left: 4px;
}

.session-card:hover .card-actions {
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

.card-name {
  font-weight: 600;
  font-size: 15px;
  color: #e6edf3;
  margin-bottom: 6px;
}

.card-activity {
  margin-bottom: 8px;
}

.activity-badge {
  display: inline-flex;
  align-items: center;
  border-radius: 999px;
  padding: 2px 8px;
  font-size: 11px;
  line-height: 1.4;
  color: #8b949e;
  background: #21262d;
  border: 1px solid #30363d;
  margin-bottom: 6px;
}

.activity-badge--needs_attention {
  color: #ffb3ad;
  background: #3d1719;
  border-color: #f85149;
}

.activity-badge--executing {
  color: #7ee787;
  background: #12261e;
  border-color: #1f6f43;
}

.activity-badge--coding {
  color: #79c0ff;
  background: #111d2f;
  border-color: #1f6feb;
}

.activity-badge--waiting {
  color: #e3b341;
  background: #2d210f;
  border-color: #9e6a03;
}

.activity-badge--idle,
.activity-badge--detached,
.activity-badge--unknown {
  color: #8b949e;
  background: #161b22;
  border-color: #30363d;
}

.activity-summary {
  font-size: 12px;
  color: #c9d1d9;
  line-height: 1.4;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.rename-input {
  background: #0d1117;
  border: 1px solid #58a6ff;
  color: #e6edf3;
  font-size: 13px;
  padding: 4px 8px;
  border-radius: 4px;
  outline: none;
  width: 100%;
  font-family: inherit;
  box-sizing: border-box;
}

.card-git {
  font-size: 12px;
  color: #8b949e;
  font-family: 'SF Mono', 'Fira Code', monospace;
  margin-bottom: 4px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.card-dir {
  font-size: 11px;
  color: #484f58;
  font-family: 'SF Mono', 'Fira Code', monospace;
  margin-bottom: 4px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.card-meta {
  font-size: 11px;
  color: #484f58;
  margin-top: 6px;
}

/* New session card */

.new-session-card {
  border-style: dashed;
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 120px;
}

.new-session-card:hover {
  border-color: #58a6ff;
}

.new-session-inner {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  color: #484f58;
}

.new-session-card:hover .new-session-inner {
  color: #8b949e;
}

.new-session-icon {
  font-size: 24px;
  line-height: 1;
}

.new-session-label {
  font-size: 12px;
}

/* Create session input card */

.create-session-card {
  display: flex;
  align-items: center;
}

.create-inner {
  width: 100%;
}

.create-label {
  display: block;
  font-size: 11px;
  color: #8b949e;
  margin-bottom: 8px;
}

.new-session-input {
  width: 100%;
  background: #0d1117;
  border: 1px solid #30363d;
  color: #e6edf3;
  font-size: 13px;
  padding: 8px 12px;
  border-radius: 4px;
  outline: none;
  font-family: inherit;
  box-sizing: border-box;
}

.new-session-input:focus {
  border-color: #58a6ff;
}

@media (max-width: 640px) {
  .grid-body {
    padding: 12px;
    gap: 12px;
  }

  .grid-header {
    padding: 8px 12px;
  }
}
</style>
