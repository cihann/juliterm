// Reactive session tree that updates from WebSocket SESSION_TREE messages.
// With PTY-based architecture, users select sessions (not individual panes).
// Selection is tracked by session ID internally so renames don't break it.

import { ref, computed, readonly, type Ref, type ComputedRef } from 'vue'

interface Pane {
  id: string
  width: number
  height: number
  active: boolean
  currentCommand?: string
  currentPath?: string
  pid?: number
}

interface Window {
  id: string
  name: string
  index: number
  active: boolean
  zoomed: boolean
  panes: Pane[]
}

type SessionStatus = 'running' | 'idle' | 'error' | 'detached'
type ToolType = 'claude' | 'codex' | 'opencode' | 'aider' | 'cursor' | 'shell' | 'unknown'
type ActivityState = 'needs_attention' | 'executing' | 'coding' | 'waiting' | 'idle' | 'detached' | 'unknown'

interface Session {
  id: string
  name: string
  windows: Window[]
  status?: SessionStatus
  lastActivity?: number
  toolType?: ToolType
  gitBranch?: string
  gitRepo?: string
  workingDir?: string
  activityState?: ActivityState
  activitySummary?: string
}

const sessions = ref<Session[]>([])

// Internal: track selection by stable session ID so renames don't clear it.
const selectedSessionId = ref<string | null>(null)

// Public: derive the current name from ID + tree. WS attachment uses this.
const selectedSessionName = computed<string | null>(() => {
  if (!selectedSessionId.value) return null
  return sessions.value.find(s => s.id === selectedSessionId.value)?.name ?? null
})

export function useSessionTree() {
  function updateFromMessage(data: unknown[]) {
    if (!Array.isArray(data)) return

    sessions.value = data.map((s: any) => ({
      id: s.id ?? '',
      name: s.name ?? '',
      windows: (s.windows ?? []).map((w: any) => ({
        id: w.id ?? '',
        name: w.name ?? '',
        index: w.index ?? 0,
        active: w.active ?? false,
        zoomed: w.zoomed ?? false,
        panes: (w.panes ?? []).map((p: any) => ({
          id: p.id ?? '',
          width: p.width ?? 0,
          height: p.height ?? 0,
          active: p.active ?? false,
          currentCommand: p.currentCommand,
          currentPath: p.currentPath,
          pid: p.pid,
        })),
      })),
      status: s.status,
      lastActivity: s.lastActivity,
      toolType: s.toolType,
      gitBranch: s.gitBranch,
      gitRepo: s.gitRepo,
      workingDir: s.workingDir,
      activityState: s.activityState,
      activitySummary: s.activitySummary,
    }))

    // If the selected session (by ID) no longer exists, clear selection.
    if (selectedSessionId.value) {
      const exists = sessions.value.some(s => s.id === selectedSessionId.value)
      if (!exists) {
        selectedSessionId.value = null
      }
    }
  }

  function selectSession(sessionName: string) {
    const session = sessions.value.find(s => s.name === sessionName)
    selectedSessionId.value = session?.id ?? null
  }

  function clearSelection() {
    selectedSessionId.value = null
  }

  function findSession(sessionName: string): Session | null {
    return sessions.value.find(s => s.name === sessionName) ?? null
  }

  return {
    sessions: sessions as Readonly<Ref<Session[]>>,
    selectedSessionName: selectedSessionName as ComputedRef<string | null>,
    selectSession,
    clearSelection,
    updateFromMessage,
    findSession,
  }
}

export type { Session, Window, Pane, SessionStatus, ToolType, ActivityState }
