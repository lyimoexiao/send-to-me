<script setup lang="ts">
import { onUnmounted, ref } from 'vue'
import AppIcon from './AppIcon.vue'
import { notifyError } from '../notify'

const props = withDefaults(
  defineProps<{
    text: string
    label?: string
    copiedLabel?: string
  }>(),
  {
    label: '复制',
    copiedLabel: '已复制',
  },
)

const emit = defineEmits<{
  copied: []
}>()

const copied = ref(false)
let resetTimer: ReturnType<typeof setTimeout> | undefined

async function writeClipboard(value: string) {
  if (navigator.clipboard && window.isSecureContext) {
    await navigator.clipboard.writeText(value)
    return
  }
  const textarea = document.createElement('textarea')
  textarea.value = value
  textarea.setAttribute('readonly', '')
  textarea.style.position = 'fixed'
  textarea.style.opacity = '0'
  document.body.appendChild(textarea)
  textarea.select()
  document.execCommand('copy')
  textarea.remove()
}

async function copy() {
  try {
    await writeClipboard(props.text)
  } catch {
    notifyError('复制失败，请手动选择内容')
    return
  }
  copied.value = true
  emit('copied')
  if (resetTimer) {
    clearTimeout(resetTimer)
  }
  resetTimer = setTimeout(() => {
    copied.value = false
  }, 1800)
}

onUnmounted(() => {
  if (resetTimer) {
    clearTimeout(resetTimer)
  }
})
</script>

<template>
  <el-button size="small" class="copy-button" @click="copy">
    <AppIcon :name="copied ? 'check' : 'copy'" :size="14" />
    <span>{{ copied ? copiedLabel : label }}</span>
  </el-button>
</template>

<style scoped>
.copy-button :deep(> span) {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}
</style>
