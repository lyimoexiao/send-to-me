<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import AppIcon from '../components/AppIcon.vue'
import CopyButton from '../components/CopyButton.vue'
import FileGlyph from '../components/FileGlyph.vue'
import PickupCodeForm from '../components/PickupCodeForm.vue'
import StageFrame from '../components/StageFrame.vue'
import { createShare } from '../api'
import { notifyError, notifyWarning } from '../notify'
import { ensureSiteConfig, siteConfig } from '../site'
import {
  fileKindLabel,
  formatBytes,
  formatCode,
  formatDateTime,
  formatDuration,
  limitLabel,
  textByteLength,
  totalFileSize,
} from '../format'
import { ApiError, type CreatedShare, type ShareKind } from '../types'
import { useSpringCard } from '../useSpringCard'

type Panel = 'none' | 'files' | 'text' | 'pickup'

const router = useRouter()

const panel = ref<Panel>('none')
const selectedFiles = ref<File[]>([])
const text = ref('')
const expiresIn = ref(siteConfig.value.defaultExpirySeconds)
const maxDownloads = ref(siteConfig.value.defaultMaxDownloads)
const customLimit = ref(false)
const dragActive = ref(false)
const uploading = ref(false)
const progress = ref(0)
const created = ref<CreatedShare | null>(null)
const fileInput = ref<HTMLInputElement | null>(null)
const cardEl = ref<HTMLElement | null>(null)

const expiryCandidates = [
  { value: 10 * 60, label: '10 分钟' },
  { value: 60 * 60, label: '1 小时' },
  { value: 24 * 60 * 60, label: '1 天' },
  { value: 7 * 24 * 60 * 60, label: '7 天' },
  { value: 30 * 24 * 60 * 60, label: '30 天' },
  { value: 90 * 24 * 60 * 60, label: '90 天' },
  { value: 365 * 24 * 60 * 60, label: '365 天' },
]

const expiryOptions = computed(() => {
  const max = siteConfig.value.maxExpirySeconds
  const options = expiryCandidates.filter((option) => option.value <= max)
  for (const value of [siteConfig.value.defaultExpirySeconds, max]) {
    if (value > 0 && !options.some((option) => option.value === value)) {
      options.push({ value, label: formatDuration(value) })
    }
  }
  return options.sort((left, right) => left.value - right.value)
})

const limitPresets = computed(() => {
  const presets = new Set([1, 3, 5, 10])
  const fallback = siteConfig.value.defaultMaxDownloads
  if (fallback > 0 && fallback <= 1000) {
    presets.add(fallback)
  }
  return [...presets].sort((left, right) => left - right)
})

const mode = computed<ShareKind>(() => (panel.value === 'text' ? 'text' : 'files'))
const expanded = computed(() => panel.value !== 'none' || Boolean(created.value))
const cardKey = computed(() => (created.value ? 'success' : panel.value))
const totalSize = computed(() => totalFileSize(selectedFiles.value))
const textChars = computed(() => [...text.value].length)
const maxUploadBytes = computed(() => siteConfig.value.maxUploadBytes)
const maxFiles = computed(() => siteConfig.value.maxFiles)
const maxTextBytes = computed(() => siteConfig.value.maxTextBytes)
const maxUploadLabel = computed(() => formatBytes(maxUploadBytes.value))
const maxTextLabel = computed(() => formatBytes(maxTextBytes.value))
const panelTitle = computed(() => {
  if (panel.value === 'pickup') {
    return '输入取件码'
  }
  if (panel.value === 'text') {
    return '发送文本'
  }
  return selectedFiles.value.length ? '发送文件' : '添加文件'
})
const panelEyebrow = computed(() => (panel.value === 'pickup' ? 'PICKUP' : 'SEND'))
const submitDisabled = computed(() => {
  if (uploading.value) {
    return true
  }
  return mode.value === 'files' ? selectedFiles.value.length === 0 : text.value.trim().length === 0
})
const resultExpiry = computed(() => {
  if (!created.value) {
    return ''
  }
  return created.value.expiresAt
    ? `至 ${formatDateTime(created.value.expiresAt)}`
    : '永不过期'
})
const resultLimit = computed(() => (created.value ? limitLabel(created.value.maxDownloads) : ''))
const resultContent = computed(() => {
  if (!created.value) {
    return ''
  }
  if (created.value.kind === 'text') {
    return '文本消息'
  }
  return `${created.value.fileCount} 个文件`
})

useSpringCard(cardEl)

function reset() {
  created.value = null
  selectedFiles.value = []
  text.value = ''
  panel.value = 'none'
  expiresIn.value = siteConfig.value.defaultExpirySeconds
  maxDownloads.value = siteConfig.value.defaultMaxDownloads
  customLimit.value = false
  progress.value = 0
}

function preparePanel(next: Panel) {
  if (uploading.value) {
    return false
  }
  if (created.value) {
    reset()
  }
  panel.value = next
  return true
}

function openFilesPanel() {
  preparePanel('files')
}

function openText() {
  preparePanel('text')
}

function openPickup() {
  preparePanel('pickup')
}

function closeCard() {
  if (uploading.value) {
    return
  }
  if (created.value) {
    reset()
    return
  }
  panel.value = 'none'
}

function pickFromHero() {
  if (!preparePanel('files')) {
    return
  }
  fileInput.value?.click()
}

function selectPreset(value: number) {
  customLimit.value = false
  maxDownloads.value = value
}

function selectCustomLimit() {
  customLimit.value = true
  if (!maxDownloads.value || maxDownloads.value < 1) {
    maxDownloads.value = 20
  }
}

function fileKey(file: File) {
  return `${file.name}-${file.size}-${file.lastModified}`
}

function pickFiles() {
  if (!uploading.value) {
    fileInput.value?.click()
  }
}

function onFileInput(event: Event) {
  const input = event.target as HTMLInputElement
  addFiles(input.files)
  input.value = ''
}

function addFiles(list: FileList | null) {
  if (!list || uploading.value || created.value) {
    return
  }
  const existing = new Set(selectedFiles.value.map(fileKey))
  const incoming = Array.from(list).filter((file) => !existing.has(fileKey(file)))
  if (incoming.length === 0) {
    return
  }
  if (selectedFiles.value.length + incoming.length > maxFiles.value) {
    notifyWarning(`一次最多发送 ${maxFiles.value} 个文件`)
    return
  }
  const combined = [...selectedFiles.value, ...incoming]
  if (totalFileSize(combined) > maxUploadBytes.value) {
    notifyWarning(`文件总大小不能超过 ${maxUploadLabel.value}`)
    return
  }
  selectedFiles.value = combined
  if (panel.value !== 'files') {
    panel.value = 'files'
  }
}

function removeFile(index: number) {
  if (uploading.value) {
    return
  }
  selectedFiles.value = selectedFiles.value.filter((_, itemIndex) => itemIndex !== index)
}

function clearFiles() {
  if (!uploading.value) {
    selectedFiles.value = []
  }
}

// Files can be dropped anywhere on the page. The listeners live on window so
// the drop target no longer depends on which element is under the cursor; the
// pickup page never mounts this view, so receiving stays untouched.
const canDrop = computed(() => !uploading.value)

function dragHasFiles(event: DragEvent) {
  return Array.from(event.dataTransfer?.types ?? []).includes('Files')
}

function onWindowDragEnter(event: DragEvent) {
  if (!dragHasFiles(event)) {
    return
  }
  event.preventDefault()
  if (canDrop.value) {
    dragActive.value = true
  }
}

function onWindowDragOver(event: DragEvent) {
  if (!dragHasFiles(event)) {
    return
  }
  event.preventDefault()
  if (event.dataTransfer) {
    event.dataTransfer.dropEffect = canDrop.value ? 'copy' : 'none'
  }
}

function onWindowDragLeave(event: DragEvent) {
  if (!dragHasFiles(event)) {
    return
  }
  // Moving between elements also fires dragleave; only a cursor at or beyond
  // the viewport edge means the drag actually left the window.
  const leftWindow =
    event.relatedTarget === null &&
    (event.clientX <= 0 ||
      event.clientY <= 0 ||
      event.clientX >= window.innerWidth ||
      event.clientY >= window.innerHeight)
  if (leftWindow) {
    dragActive.value = false
  }
}

function onWindowDrop(event: DragEvent) {
  if (!dragHasFiles(event)) {
    dragActive.value = false
    return
  }
  // Always swallow the drop so the browser never navigates to the file.
  event.preventDefault()
  dragActive.value = false
  if (!canDrop.value) {
    return
  }
  if (created.value) {
    reset()
  }
  panel.value = 'files'
  addFiles(event.dataTransfer?.files ?? null)
}

function bindDragListeners() {
  window.addEventListener('dragenter', onWindowDragEnter)
  window.addEventListener('dragover', onWindowDragOver)
  window.addEventListener('dragleave', onWindowDragLeave)
  window.addEventListener('drop', onWindowDrop)
}

function unbindDragListeners() {
  window.removeEventListener('dragenter', onWindowDragEnter)
  window.removeEventListener('dragover', onWindowDragOver)
  window.removeEventListener('dragleave', onWindowDragLeave)
  window.removeEventListener('drop', onWindowDrop)
}

function goPickup(code: string) {
  void router.push(`/p/${code}`)
}

async function submit() {
  if (uploading.value || created.value) {
    return
  }
  if (mode.value === 'files') {
    if (selectedFiles.value.length === 0) {
      notifyWarning('请先选择要发送的文件')
      return
    }
    if (selectedFiles.value.length > maxFiles.value) {
      notifyWarning(`一次最多发送 ${maxFiles.value} 个文件`)
      return
    }
    if (totalSize.value > maxUploadBytes.value) {
      notifyWarning(`文件总大小不能超过 ${maxUploadLabel.value}`)
      return
    }
  } else {
    if (text.value.trim().length === 0) {
      notifyWarning('请输入要发送的文本')
      return
    }
    if (textByteLength(text.value) > maxTextBytes.value) {
      notifyWarning(`文本内容不能超过 ${maxTextLabel.value}`)
      return
    }
  }
  if (customLimit.value && (!maxDownloads.value || maxDownloads.value < 1)) {
    notifyWarning('请输入大于 0 的下载次数，或选择不限')
    return
  }

  uploading.value = true
  progress.value = 0
  try {
    created.value = await createShare(
      {
        kind: mode.value,
        files: selectedFiles.value,
        text: text.value,
        expiresIn: expiresIn.value,
        maxDownloads: maxDownloads.value ?? 0,
      },
      (percent) => {
        progress.value = percent
      },
    )
    progress.value = 100
    panel.value = 'none'
  } catch (caught) {
    notifyError(caught instanceof ApiError ? caught.message : '发送失败，请稍后再试')
  } finally {
    uploading.value = false
  }
}

onMounted(async () => {
  bindDragListeners()
  await ensureSiteConfig()
  expiresIn.value = siteConfig.value.defaultExpirySeconds
  maxDownloads.value = siteConfig.value.defaultMaxDownloads
})

onBeforeUnmount(unbindDragListeners)
</script>

<template>
  <StageFrame @files="openFilesPanel" @pickup="openPickup">
    <div class="home">
      <section
        ref="cardEl"
        class="ct-panel compose-card"
        :class="{ 'is-expanded': expanded }"
        :aria-label="expanded ? panelTitle : '添加文件'"
      >
        <header class="card-head">
          <div class="card-brand">
            <span class="brand-mark">
              <AppIcon name="relay" :size="18" :stroke-width="1.55" />
            </span>
            <span class="brand-words">
              <strong>递我</strong>
              <em>SEND TO ME</em>
            </span>
          </div>

          <button
            v-if="expanded"
            type="button"
            class="card-close"
            :aria-label="created ? '关闭' : '收起'"
            @click="closeCard"
          >
            <AppIcon name="x" :size="14" />
          </button>
        </header>

        <div :key="cardKey" class="card-content">
          <div v-if="!expanded" class="card-idle">
            <button type="button" class="add-row" @click="pickFromHero">
              <span class="add-circle">
                <AppIcon name="plus" :size="18" :stroke-width="2" />
              </span>
              <span class="add-copy">
                <strong>添加文件</strong>
                <em>拖入或点击选择</em>
              </span>
            </button>

            <div class="card-quick">
              <button type="button" @click="openText">发送文本</button>
              <i aria-hidden="true"></i>
              <button type="button" @click="openPickup">输入取件码</button>
            </div>
          </div>

          <div v-else-if="!created" class="card-compose">
            <div class="compose-title">
              <p class="panel-eyebrow">{{ panelEyebrow }}</p>
              <h2 class="panel-title">{{ panelTitle }}</h2>
            </div>

            <template v-if="panel === 'pickup'">
              <p class="panel-note">输入对方给你的 6 位取件码，确认内容后再取件。</p>
              <PickupCodeForm button-label="去取件" autofocus @submit="goPickup" />
              <button type="button" class="panel-link" @click="openFilesPanel">
                <AppIcon name="arrow" :size="12" />
                寄一份给对方
              </button>
            </template>

            <form v-else @submit.prevent="submit">
              <template v-if="panel === 'files'">
                <button
                  v-if="!selectedFiles.length"
                  type="button"
                  class="file-empty"
                  @click="pickFiles"
                >
                  <span class="file-empty-icon">
                    <AppIcon name="upload" :size="18" />
                  </span>
                  <strong>拖入文件，或点击选择</strong>
                  <span>最多 {{ maxFiles }} 个 · 单次 {{ maxUploadLabel }}</span>
                </button>

                <div v-else class="panel-files">
                  <div class="panel-files-head">
                    <span>{{ selectedFiles.length }} 个文件 · {{ formatBytes(totalSize) }}</span>
                    <button type="button" @click="clearFiles">清空</button>
                  </div>
                  <ul class="panel-file-list">
                    <li v-for="(file, index) in selectedFiles" :key="fileKey(file)">
                      <FileGlyph :name="file.name" compact />
                      <div class="file-info">
                        <strong>{{ file.name }}</strong>
                        <span>{{ fileKindLabel(file.name) }} · {{ formatBytes(file.size) }}</span>
                      </div>
                      <button
                        type="button"
                        class="file-remove"
                        :aria-label="`移除 ${file.name}`"
                        :disabled="uploading"
                        @click="removeFile(index)"
                      >
                        <AppIcon name="x" :size="12" />
                      </button>
                    </li>
                  </ul>
                  <button type="button" class="add-more" @click="pickFiles">
                    <AppIcon name="plus" :size="12" />
                    继续添加
                  </button>
                </div>
              </template>

              <div v-else class="panel-text">
                <div class="text-meta">
                  <span>文本内容</span>
                  <span>{{ textChars }} 字符 · 上限 {{ maxTextLabel }}</span>
                </div>
                <el-input
                  v-model="text"
                  type="textarea"
                  :rows="6"
                  :disabled="uploading"
                  placeholder="写点什么…"
                />
              </div>

              <div class="panel-settings">
                <div class="setting-row">
                  <span class="setting-label">有效期</span>
                  <el-select v-model="expiresIn" :disabled="uploading" size="default">
                    <el-option
                      v-for="option in expiryOptions"
                      :key="option.value"
                      :label="option.label"
                      :value="option.value"
                    />
                  </el-select>
                </div>

                <div class="setting-block">
                  <span class="setting-label">下载次数</span>
                  <div class="limit-chips" role="group" aria-label="下载次数">
                    <button
                      v-for="preset in limitPresets"
                      :key="preset"
                      type="button"
                      :class="{ active: !customLimit && maxDownloads === preset }"
                      :disabled="uploading"
                      @click="selectPreset(preset)"
                    >
                      {{ preset }}
                    </button>
                    <button
                      type="button"
                      :class="{ active: !customLimit && maxDownloads === 0 }"
                      :disabled="uploading"
                      @click="selectPreset(0)"
                    >
                      不限
                    </button>
                    <button
                      type="button"
                      :class="{ active: customLimit }"
                      :disabled="uploading"
                      @click="selectCustomLimit"
                    >
                      自定义
                    </button>
                  </div>
                  <Transition name="custom-field">
                    <div v-if="customLimit" class="custom-field">
                      <el-input-number
                        v-model="maxDownloads"
                        :min="1"
                        :max="1000"
                        controls-position="right"
                        :disabled="uploading"
                      />
                    </div>
                  </Transition>
                </div>
              </div>

              <button
                type="submit"
                class="panel-submit"
                :class="{ 'is-busy': uploading }"
                :disabled="submitDisabled && !uploading"
              >
                <template v-if="uploading">
                  发送中 {{ Math.min(progress, 99) }}%
                </template>
                <template v-else>
                  {{ mode === 'text' ? '寄出文本' : '开始发送' }}
                  <AppIcon name="arrow" :size="15" />
                </template>
              </button>
              <div v-if="uploading" class="panel-progress" aria-hidden="true">
                <i :style="{ width: `${Math.min(progress, 99)}%` }"></i>
              </div>
            </form>
          </div>

          <div v-else class="success-block" aria-live="polite">
            <p class="success-eyebrow">寄件成功 · 取件码</p>
            <div class="success-code-line">
              <p class="success-code">{{ formatCode(created.code) }}</p>
              <CopyButton :text="created.code" label="拷贝" copied-label="已复制" />
            </div>

            <div class="success-link">
              <AppIcon name="link" :size="13" />
              <span :title="created.link">{{ created.link }}</span>
              <CopyButton :text="created.link" label="拷贝链接" copied-label="已复制" />
            </div>

            <dl class="success-meta">
              <div>
                <dt>有效期</dt>
                <dd>{{ resultExpiry }}</dd>
              </div>
              <div>
                <dt>取件设备</dt>
                <dd>{{ resultLimit }}</dd>
              </div>
              <div>
                <dt>内容</dt>
                <dd>{{ resultContent }}</dd>
              </div>
            </dl>

            <div class="success-actions">
              <button type="button" class="success-again" @click="reset">再寄一件</button>
              <a class="success-open" :href="created.link" target="_blank" rel="noopener">
                打开取件页
                <AppIcon name="external" :size="12" />
              </a>
            </div>
          </div>
        </div>
      </section>

      <input
        ref="fileInput"
        class="sr-only"
        type="file"
        multiple
        tabindex="-1"
        :disabled="uploading"
        @change="onFileInput"
      />
    </div>

    <Transition name="fade">
      <div v-if="dragActive" class="drop-layer">
        <div class="drop-layer-inner">
          <span class="drop-layer-icon"><AppIcon name="upload" :size="22" /></span>
          <strong>松手即可添加</strong>
          <span>最多 {{ maxFiles }} 个文件 · 单次 {{ maxUploadLabel }}</span>
        </div>
      </div>
    </Transition>
  </StageFrame>
</template>

<style scoped>
.home {
  min-height: 100vh;
}

.compose-card {
  position: fixed;
  top: 24px;
  left: 24px;
  z-index: 10;
  width: 336px;
  max-height: calc(100vh - 48px);
  padding: 20px 20px 18px;
  overflow-y: auto;
  overscroll-behavior: contain;
  color: var(--ink);
  border: 1px solid var(--glass-border);
  border-radius: 20px;
  box-shadow:
    0 24px 64px -30px rgba(12, 4, 16, 0.6),
    0 3px 12px rgba(12, 4, 16, 0.12);
  transition:
    box-shadow 280ms ease,
    background-color 900ms cubic-bezier(0.2, 0, 0, 1);
  animation: card-in 520ms cubic-bezier(0.2, 0, 0, 1) both;
}

@keyframes card-in {
  from {
    opacity: 0;
    transform: translateY(12px) scale(0.985);
  }
  to {
    opacity: 1;
    transform: none;
  }
}

.card-head {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 14px;
  margin-bottom: 16px;
}

.card-brand {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: center;
  gap: 9px;
}

.brand-mark {
  display: grid;
  width: 32px;
  height: 32px;
  flex: 0 0 auto;
  place-items: center;
  color: #fff;
  background: var(--brand);
  border-radius: 50%;
}

.brand-words strong {
  display: block;
  color: var(--ink);
  font-size: 16px;
  font-weight: 700;
  letter-spacing: 0.02em;
  line-height: 1.1;
}

.brand-words em {
  display: block;
  margin-top: 2px;
  color: var(--muted-light);
  font-family: var(--font-mono);
  font-size: 6.5px;
  font-style: normal;
  font-weight: 600;
  letter-spacing: 0.17em;
  line-height: 1;
}

.card-close {
  position: absolute;
  top: 50%;
  right: 0;
  display: grid;
  width: 28px;
  height: 28px;
  place-items: center;
  color: var(--muted);
  background: var(--surface-strong);
  border-radius: 50%;
  translate: 0 -50%;
  transition:
    color 150ms ease,
    background-color 150ms ease,
    scale 150ms ease;
}

@media (hover: hover) {
  .card-close:hover {
    color: var(--brand-text);
    background: var(--brand-soft);
  }
}

.card-close:active {
  scale: 0.96;
}

.card-idle {
  display: flex;
  flex-direction: column;
}

.add-row {
  display: flex;
  width: 100%;
  min-height: 62px;
  align-items: center;
  gap: 11px;
  padding: 10px 12px;
  background: var(--surface-row);
  border: 1px solid var(--inline-border);
  border-radius: 12px;
  text-align: left;
  transition:
    background-color 150ms ease,
    box-shadow 150ms ease,
    translate 150ms ease,
    scale 150ms ease;
}

@media (hover: hover) {
  .add-row:hover {
    background: var(--surface-row-hover);
    box-shadow: 0 12px 24px -20px rgba(30, 10, 30, 0.5);
    translate: 0 -1px;
  }
}

.add-row:active {
  scale: 0.98;
}

.add-circle {
  display: grid;
  width: 34px;
  height: 34px;
  flex: 0 0 auto;
  place-items: center;
  color: #fff;
  background: var(--brand);
  border-radius: 50%;
  transition: background-color 150ms ease;
}

@media (hover: hover) {
  .add-row:hover .add-circle {
    background: var(--brand-deep);
  }
}

.add-copy {
  min-width: 0;
}

.add-copy strong {
  display: block;
  color: var(--ink);
  font-size: 13px;
  font-weight: 600;
  line-height: 1.3;
}

.add-copy em {
  display: block;
  margin-top: 2px;
  color: var(--muted-light);
  font-size: 10px;
  font-style: normal;
  line-height: 1.3;
}

.card-quick {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 12px;
  padding: 0 4px;
}

.card-quick button {
  color: var(--muted);
  font-size: 11px;
  letter-spacing: 0.02em;
  transition: color 150ms ease;
}

@media (hover: hover) {
  .card-quick button:hover {
    color: var(--ink);
  }
}

.card-quick i {
  width: 1px;
  height: 9px;
  background: var(--line-strong);
}

.compose-title {
  margin-bottom: 14px;
}

.card-content {
  animation: card-content-in 220ms ease both;
}

@keyframes card-content-in {
  from {
    opacity: 0;
  }
  to {
    opacity: 1;
  }
}

.custom-field {
  padding-top: 8px;
  transform-origin: 50% 0;
}

.custom-field .el-input-number {
  margin-top: 0;
}

.custom-field-enter-active {
  transition:
    opacity 200ms ease,
    transform 320ms cubic-bezier(0.34, 1.56, 0.64, 1);
}

.custom-field-leave-active {
  transition:
    opacity 120ms ease,
    transform 120ms ease;
}

.custom-field-enter-from,
.custom-field-leave-to {
  opacity: 0;
  transform: translateY(-5px) scale(0.975);
}

.file-empty {
  display: flex;
  width: 100%;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  padding: 22px 14px 20px;
  background: var(--surface-soft);
  border: 1px dashed var(--line-dash);
  border-radius: 14px;
  text-align: center;
  transition:
    background-color 150ms ease,
    border-color 150ms ease;
}

@media (hover: hover) {
  .file-empty:hover {
    background: var(--surface-soft-hover);
    border-color: var(--line-dash-hover);
  }
}

.file-empty-icon {
  display: grid;
  width: 38px;
  height: 38px;
  margin-bottom: 2px;
  place-items: center;
  color: var(--brand-text);
  background: var(--brand-soft);
  border-radius: 50%;
}

.file-empty strong {
  font-size: 12.5px;
  font-weight: 600;
}

.file-empty > span:last-child {
  color: var(--muted-light);
  font-size: 10px;
}

.panel-files-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  color: var(--muted);
  font-size: 10.5px;
}

.panel-files-head button {
  color: var(--muted);
  font-size: 10.5px;
  transition: color 150ms ease;
}

.panel-files-head button:hover {
  color: var(--ink);
}

.panel-file-list {
  max-height: 176px;
  margin-top: 6px;
  overflow-y: auto;
  overscroll-behavior: contain;
}

.panel-file-list li {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  gap: 9px;
  align-items: center;
  padding: 7px 0;
  border-top: 1px solid var(--line-row);
}

.file-info {
  min-width: 0;
}

.file-info strong {
  display: block;
  overflow: hidden;
  font-size: 11.5px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.file-info span {
  display: block;
  margin-top: 1px;
  color: var(--muted-light);
  font-size: 9.5px;
}

.file-remove {
  display: grid;
  width: 26px;
  height: 26px;
  place-items: center;
  color: var(--muted-light);
  border-radius: 50%;
  transition:
    color 150ms ease,
    background-color 150ms ease;
}

.file-remove:hover {
  color: var(--red);
  background: var(--danger-soft);
}

.add-more {
  display: flex;
  width: 100%;
  height: 34px;
  align-items: center;
  justify-content: center;
  gap: 5px;
  margin-top: 8px;
  color: var(--muted);
  border: 1px dashed var(--line-dash);
  border-radius: 10px;
  font-size: 10.5px;
  transition:
    color 150ms ease,
    border-color 150ms ease;
}

.add-more:hover {
  color: var(--ink);
  border-color: var(--ink-soft);
}

.text-meta {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 7px;
  color: var(--muted);
  font-size: 10.5px;
}

.text-meta span:first-child {
  color: var(--ink-soft);
  font-size: 11px;
  font-weight: 600;
}

.text-meta span:last-child {
  color: var(--muted-light);
  font-size: 9.5px;
}

.panel-submit.is-busy {
  color: #fff;
  background: var(--brand);
  cursor: progress;
}

.success-block {
  color: var(--ink);
}

.success-eyebrow {
  color: var(--muted-light);
  font-family: var(--font-mono);
  font-size: 8.5px;
  letter-spacing: 0.18em;
}

.success-code-line {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-top: 7px;
}

.success-code {
  font-family: var(--font-mono);
  font-size: clamp(28px, 2.4vw, 33px);
  font-weight: 600;
  letter-spacing: 0.1em;
  line-height: 1.1;
  white-space: nowrap;
}

.success-link {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 14px;
  padding: 10px 0;
  border-top: 1px solid var(--line-row);
  border-bottom: 1px solid var(--line-row);
}

.success-link > svg {
  flex: 0 0 auto;
  color: var(--muted-light);
}

.success-link > span {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  color: var(--muted);
  font-family: var(--font-mono);
  font-size: 9.5px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.success-block :deep(.copy-button) {
  height: 32px;
  margin: 0;
  padding: 0 13px;
  color: var(--ink);
  background: var(--surface-strong);
  border: 1px solid var(--line-tile);
  border-radius: 999px;
  font-size: 10px;
  font-weight: 600;
}

.success-block :deep(.copy-button > span) {
  gap: 4px;
}

.success-block :deep(.copy-button:hover) {
  color: var(--ink);
  background: var(--surface-strong-hover);
  border-color: var(--line-tile-hover);
}

.success-meta {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 0;
  margin: 14px 0 0;
}

.success-meta div {
  min-width: 0;
  padding-left: 12px;
  border-left: 1px solid var(--line-row);
}

.success-meta div:first-child {
  padding-left: 0;
  border-left: 0;
}

.success-meta dt {
  color: var(--muted-light);
  font-size: 9px;
  letter-spacing: 0.04em;
}

.success-meta dd {
  margin: 3px 0 0;
  overflow: hidden;
  color: var(--ink);
  font-size: 11px;
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.success-actions {
  display: flex;
  align-items: center;
  gap: 14px;
  margin-top: 18px;
}

.success-again {
  display: inline-flex;
  height: 42px;
  align-items: center;
  justify-content: center;
  padding: 0 22px;
  color: #fff;
  background: var(--brand);
  border-radius: 999px;
  font-size: 12.5px;
  font-weight: 600;
  transition:
    background-color 150ms ease,
    scale 150ms ease;
}

.success-again:hover {
  background: var(--brand-deep);
}

.success-again:active {
  scale: 0.96;
}

.success-open {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--muted);
  font-size: 11px;
  text-decoration: none;
  transition: color 150ms ease;
}

.success-open:hover {
  color: var(--ink);
}

.drop-layer {
  position: fixed;
  inset: 0;
  z-index: 40;
  display: grid;
  /* Never intercept drag events, or the overlay makes itself flicker. */
  pointer-events: none;
  place-items: center;
  padding: 24px;
  background: rgba(18, 7, 16, 0.46);
  backdrop-filter: blur(8px);
}

.drop-layer-inner {
  display: flex;
  width: min(520px, 100%);
  flex-direction: column;
  align-items: center;
  gap: 7px;
  padding: 46px 24px;
  color: #fff;
  background: rgba(255, 255, 255, 0.08);
  border: 1.5px dashed rgba(255, 255, 255, 0.62);
  border-radius: 28px;
  text-align: center;
}

.drop-layer-icon {
  display: grid;
  width: 48px;
  height: 48px;
  margin-bottom: 4px;
  place-items: center;
  background: rgba(255, 255, 255, 0.14);
  border-radius: 50%;
}

.drop-layer-inner strong {
  font-size: 17px;
  font-weight: 500;
}

.drop-layer-inner > span:last-child {
  color: rgba(255, 255, 255, 0.62);
  font-size: 10.5px;
}

.fade-enter-active,
.fade-leave-active {
  transition: opacity 180ms ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}

@media (max-width: 860px) {
  .compose-card {
    top: auto;
    right: 12px;
    bottom: calc(12px + env(safe-area-inset-bottom, 0px));
    left: 12px;
    width: auto;
    max-height: calc(100dvh - 24px);
    padding: 18px;
    border-radius: 18px;
  }
}

@media (max-width: 480px) {
  .compose-card {
    padding: 16px;
    border-radius: 16px;
  }

  .success-link {
    flex-wrap: wrap;
  }

  .success-link > span {
    flex-basis: calc(100% - 22px);
  }

  .success-meta div {
    padding-left: 8px;
  }

  .success-meta dd {
    font-size: 10px;
  }
}
</style>
