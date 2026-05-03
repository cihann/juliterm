import { ref } from 'vue'

// Module-scoped so MobileKeyBar and TerminalView share sticky modifier state.
export const ctrlActive = ref(false)
export const altActive = ref(false)
