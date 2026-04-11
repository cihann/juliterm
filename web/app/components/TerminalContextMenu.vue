<script setup lang="ts">
import { ref, watch, nextTick, onBeforeUnmount } from 'vue'

const props = defineProps<{
  visible: boolean
  x: number
  y: number
  zoomed: boolean
}>()

const emit = defineEmits<{
  action: [type: string]
  close: []
}>()

const menuRef = ref<HTMLDivElement | null>(null)

// Clamp position so menu stays within viewport.
const menuStyle = ref<Record<string, string>>({})

function select(type: string) {
  emit('action', type)
  emit('close')
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    emit('close')
  }
}

function onClickOutside(e: MouseEvent) {
  if (menuRef.value && !menuRef.value.contains(e.target as Node)) {
    emit('close')
  }
}

watch(() => props.visible, (v) => {
  // Always remove first to prevent duplicate listeners on rapid toggle.
  document.removeEventListener('mousedown', onClickOutside)

  if (v) {
    // Use requestAnimationFrame to ensure browser has completed layout
    // before reading dimensions for viewport clamping.
    nextTick(() => requestAnimationFrame(() => {
      const el = menuRef.value
      if (!el) return
      const rect = el.getBoundingClientRect()
      const clampedX = Math.min(props.x, window.innerWidth - rect.width - 8)
      const clampedY = Math.min(props.y, window.innerHeight - rect.height - 8)
      menuStyle.value = {
        left: `${Math.max(0, clampedX)}px`,
        top: `${Math.max(0, clampedY)}px`,
      }
      el.focus()
      document.addEventListener('mousedown', onClickOutside)
    }))
  }
})

onBeforeUnmount(() => {
  document.removeEventListener('mousedown', onClickOutside)
})
</script>

<template>
  <Teleport to="body">
    <div
      v-if="visible"
      ref="menuRef"
      class="ctx-menu"
      :style="menuStyle"
      tabindex="-1"
      @keydown="onKeydown"
    >
      <div class="ctx-group">
        <button class="ctx-item" @click="select('horizontal-split')">
          <span class="ctx-label">Horizontal Split</span>
          <span class="ctx-hint">h</span>
        </button>
        <button class="ctx-item" @click="select('vertical-split')">
          <span class="ctx-label">Vertical Split</span>
          <span class="ctx-hint">v</span>
        </button>
      </div>
      <div class="ctx-divider" />
      <div class="ctx-group">
        <button class="ctx-item ctx-item--danger" @click="select('kill-pane')">
          <span class="ctx-label">Kill</span>
          <span class="ctx-hint">x</span>
        </button>
        <button class="ctx-item" @click="select('zoom')">
          <span class="ctx-label">{{ zoomed ? 'Unzoom' : 'Zoom' }}</span>
          <span class="ctx-hint">z</span>
        </button>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.ctx-menu {
  position: fixed;
  z-index: 1100;
  min-width: 180px;
  background: #1a1a2e;
  border: 1px solid #30363d;
  border-radius: 4px;
  padding: 4px 0;
  font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 13px;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.5);
  outline: none;
}

.ctx-group {
  padding: 2px 0;
}

.ctx-divider {
  height: 1px;
  background: #30363d;
  margin: 2px 0;
}

.ctx-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  padding: 6px 12px;
  background: none;
  border: none;
  color: #c9d1d9;
  cursor: pointer;
  font-family: inherit;
  font-size: inherit;
  text-align: left;
  transition: background 0.1s;
}

.ctx-item:hover {
  background: #21262d;
  color: #e6edf3;
}

.ctx-item--danger:hover {
  background: #da363322;
  color: #f85149;
}

.ctx-hint {
  color: #484f58;
  font-size: 11px;
  margin-left: 16px;
}

.ctx-item:hover .ctx-hint {
  color: #6e7681;
}
</style>
