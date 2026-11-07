import { ElMessage } from 'element-plus'

// Toasts share the app's glass language: see `.stm-message` in style.css.
const base = {
  grouping: true,
  duration: 2600,
  offset: 24,
  showClose: false,
  customClass: 'stm-message',
}

export function notifyError(message: string) {
  ElMessage({ ...base, type: 'error', message })
}

export function notifyWarning(message: string) {
  ElMessage({ ...base, type: 'warning', message })
}

export function notifySuccess(message: string) {
  ElMessage({ ...base, type: 'success', message, duration: 2000 })
}
