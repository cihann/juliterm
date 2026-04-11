// Composable for session and window CRUD API calls.
// All name params are URL-encoded for safe path interpolation.

async function apiCall(url: string, method: string, body?: unknown): Promise<void> {
  const opts: RequestInit = {
    method,
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  }
  const res = await fetch(url, opts)
  if (!res.ok) {
    const data = await res.json().catch(() => ({ error: 'Request failed' }))
    throw new Error(data.error ?? `Request failed (${res.status})`)
  }
}

export function useSessionApi() {
  async function createSession(name: string) {
    await apiCall('/api/sessions', 'POST', { name })
  }

  async function renameSession(oldName: string, newName: string) {
    await apiCall(`/api/sessions/${encodeURIComponent(oldName)}`, 'PATCH', { name: newName })
  }

  async function killSession(name: string) {
    await apiCall(`/api/sessions/${encodeURIComponent(name)}`, 'DELETE')
  }

  async function createWindow(sessionName: string) {
    await apiCall(`/api/sessions/${encodeURIComponent(sessionName)}/windows`, 'POST')
  }

  async function renameWindow(sessionName: string, windowIndex: number, newName: string) {
    await apiCall(
      `/api/sessions/${encodeURIComponent(sessionName)}/windows/${windowIndex}`,
      'PATCH',
      { name: newName },
    )
  }

  async function killWindow(sessionName: string, windowIndex: number) {
    await apiCall(
      `/api/sessions/${encodeURIComponent(sessionName)}/windows/${windowIndex}`,
      'DELETE',
    )
  }

  async function splitWindow(sessionName: string, preset: string, command?: string, direction?: string) {
    await apiCall(`/api/sessions/${encodeURIComponent(sessionName)}/split`, 'POST', {
      preset,
      ...(command !== undefined && { command }),
      ...(direction !== undefined && { direction }),
    })
  }

  async function detectTestCommand(sessionName: string): Promise<string | null> {
    const res = await fetch(`/api/sessions/${encodeURIComponent(sessionName)}/detect-test`, {
      credentials: 'include',
    })
    if (!res.ok) return null
    const data = await res.json()
    return data.command ?? null
  }

  async function zoomPane(sessionName: string) {
    await apiCall(`/api/sessions/${encodeURIComponent(sessionName)}/zoom`, 'POST')
  }

  async function killPane(sessionName: string) {
    await apiCall(`/api/sessions/${encodeURIComponent(sessionName)}/pane`, 'DELETE')
  }

  interface Preset {
    name: string
    label: string
    direction: string
    percent: number
    detach: boolean
  }

  async function getPresets(sessionName: string): Promise<{ builtIn: Preset[], global: Preset[], project: Preset[] }> {
    const res = await fetch(`/api/sessions/${encodeURIComponent(sessionName)}/presets`, { credentials: 'include' })
    if (!res.ok) return { builtIn: [], global: [], project: [] }
    return res.json()
  }

  return {
    createSession, renameSession, killSession,
    createWindow, renameWindow, killWindow,
    splitWindow, detectTestCommand, zoomPane, killPane,
    getPresets,
  }
}
