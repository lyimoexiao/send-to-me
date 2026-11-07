<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppIcon from '../components/AppIcon.vue'
import CopyButton from '../components/CopyButton.vue'
import FileGlyph from '../components/FileGlyph.vue'
import PickupCodeForm from '../components/PickupCodeForm.vue'
import StageFrame from '../components/StageFrame.vue'
import { notifyError } from '../notify'
import { checkDownload, getShare, revealText } from '../api'
import {
  expiryHint,
  formatBytes,
  formatCode,
  formatDateTime,
  normalizeCodeInput,
  remainingLabel,
  totalFileSize,
} from '../format'
import { ApiError, type RevealedText, type ShareMeta, type ShareStatus } from '../types'
import { useSpringCard } from '../useSpringCard'

interface LoadFailure {
  kind: 'not_found' | 'network' | 'server'
  title: string
  message: string
}

const route = useRoute()
const router = useRouter()

const code = ref('')
const meta = ref<ShareMeta | null>(null)
const loading = ref(true)
const loadFailure = ref<LoadFailure | null>(null)
const busy = ref(false)
const downloaded = ref(false)
const consumedThisPickup = ref(false)
const revealed = ref<RevealedText | null>(null)
const now = ref(Date.now())
const cardEl = ref<HTMLElement | null>(null)
let clock: ReturnType<typeof setInterval> | undefined

useSpringCard(cardEl)

const files = computed(() => meta.value?.files ?? [])
const totalSize = computed(() => totalFileSize(files.value))
const deviceConsumed = computed(() => meta.value?.deviceConsumed ?? false)
const effectiveStatus = computed<ShareStatus>(() => {
  const current = meta.value
  if (!current) {
    return 'active'
  }
  if (current.status === 'exhausted' && current.deviceConsumed) {
    // This terminal already picked the drop up; the limit is about devices.
    return 'active'
  }
  if (current.status !== 'active') {
    return current.status
  }
  if (current.expiresAt && new Date(current.expiresAt).getTime() <= now.value) {
    return 'expired'
  }
  return 'active'
})
const remainingZero = computed(
  () => meta.value?.remainingDownloads !== null && (meta.value?.remainingDownloads ?? 1) <= 0,
)
const locked = computed(() => remainingZero.value && !deviceConsumed.value)
const badgeStatus = computed<ShareStatus>(() => {
  if (deviceConsumed.value && effectiveStatus.value === 'active') {
    return 'active'
  }
  if (
    meta.value?.status === 'active' &&
    meta.value &&
    remainingZero.value &&
    (downloaded.value || revealed.value)
  ) {
    return 'exhausted'
  }
  return effectiveStatus.value
})
const statusLabel = computed(() => {
  if (loadFailure.value) {
    return loadFailure.value.kind === 'not_found' ? '未找到' : '连接失败'
  }
  if (deviceConsumed.value && effectiveStatus.value === 'active') {
    return '已取件'
  }
  switch (badgeStatus.value) {
    case 'expired':
      return '已过期'
    case 'exhausted':
      return '已取完'
    default:
      return '可取件'
  }
})
const displayTitle = computed(() => {
  if (!meta.value) {
    return ''
  }
  if (meta.value.kind === 'text') {
    return '一份文本'
  }
  if (files.value.length === 1) {
    return files.value[0].name
  }
  return `${files.value.length} 个文件`
})
const actionLabel = computed(() => {
  if (!meta.value) {
    return ''
  }
  if (busy.value) {
    return '正在准备…'
  }
  if (locked.value) {
    return meta.value.kind === 'text' ? '次数已用完' : '下载次数已用完'
  }
  if (deviceConsumed.value) {
    if (meta.value.kind === 'text') {
      return '再次查看文本'
    }
    return files.value.length > 1 ? '再次打包下载' : '再次下载文件'
  }
  if (meta.value.kind === 'text') {
    return '查看文本'
  }
  if (downloaded.value) {
    return files.value.length > 1 ? '再次打包下载' : '再次下载文件'
  }
  return files.value.length > 1 ? '打包下载全部文件' : '下载文件'
})
const unavailableTitle = computed(() => {
  if (effectiveStatus.value === 'expired') {
    return '这份分享已经过期。'
  }
  return '这份分享已经被取完。'
})
const unavailableDesc = computed(() => {
  if (effectiveStatus.value === 'expired') {
    return '有效期已结束，内容不再开放。'
  }
  return '下载次数已经用完，内容不再开放。'
})
const remainingText = computed(() => {
  if (!meta.value) {
    return ''
  }
  const remaining = meta.value.remainingDownloads
  if (!deviceConsumed.value) {
    return remainingLabel(remaining)
  }
  if (remaining === null) {
    return '本设备已取件 · 不限次数'
  }
  if (remaining <= 0) {
    return '本设备已取件'
  }
  return `本设备已取件 · 剩余 ${remaining} 次`
})
const noteText = computed(() => {
  if (downloaded.value) {
    return consumedThisPickup.value
      ? '下载已开始，本次取件已记录。'
      : '下载已开始，同一设备重复下载不再计数。'
  }
  if (deviceConsumed.value) {
    return `本设备已取件，可重复${meta.value?.kind === 'text' ? '查看' : '下载'}，不再消耗次数。`
  }
  return `打开页面不计数，每台设备首次${meta.value?.kind === 'text' ? '查看' : '下载'}计 1 次。`
})
const revealedRemaining = computed(() => remainingText.value)
const revealedSize = computed(() => {
  if (!revealed.value) {
    return ''
  }
  return formatBytes(new TextEncoder().encode(revealed.value.text).length)
})
const retryable = computed(
  () => loadFailure.value?.kind === 'network' || loadFailure.value?.kind === 'server',
)

function titleForMeta(value: ShareMeta | null) {
  document.title = value ? `取件 ${formatCode(value.code)} · 递我` : '取件 · 递我'
}

function failureFrom(caught: unknown): LoadFailure {
  if (caught instanceof ApiError) {
    if (caught.status === 0) {
      return {
        kind: 'network',
        title: '暂时连不上服务器。',
        message: '请检查网络连接后再试。',
      }
    }
    if (caught.status === 404 || caught.status === 410) {
      return {
        kind: 'not_found',
        title: '没有找到这份分享。',
        message: '取件码可能输错了，或者分享已被清理。',
      }
    }
    return {
      kind: 'server',
      title: '取件时出了点问题。',
      message: caught.message || '请稍后再试一次。',
    }
  }
  return {
    kind: 'server',
    title: '取件时出了点问题。',
    message: '请稍后再试一次。',
  }
}

async function load(rawCode: string) {
  const normalized = normalizeCodeInput(rawCode)
  code.value = normalized
  loading.value = true
  loadFailure.value = null
  downloaded.value = false
  consumedThisPickup.value = false
  revealed.value = null
  meta.value = null
  titleForMeta(null)

  if (!normalized) {
    loading.value = false
    loadFailure.value = failureFrom(new ApiError(404, 'not_found', '取件码不存在'))
    return
  }

  try {
    const result = await getShare(normalized)
    meta.value = result
    titleForMeta(result)
  } catch (caught) {
    loadFailure.value = failureFrom(caught)
  } finally {
    loading.value = false
  }
}

function goPickup(nextCode: string) {
  void router.push(`/p/${nextCode}`)
}

function goHome() {
  void router.push('/')
}

function retry() {
  void load(code.value || String(route.params.code ?? ''))
}

function triggerDownload() {
  const anchor = document.createElement('a')
  anchor.href = `/api/shares/${encodeURIComponent(code.value)}/download`
  anchor.setAttribute('download', '')
  anchor.rel = 'noopener'
  anchor.style.display = 'none'
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
}

async function downloadFiles() {
  if (busy.value || !meta.value || meta.value.kind !== 'files' || locked.value) {
    return
  }
  busy.value = true
  try {
    const alreadyClaimed = meta.value.deviceConsumed
    const remainingBefore = await checkDownload(code.value)
    triggerDownload()
    downloaded.value = true
    consumedThisPickup.value = !alreadyClaimed
    // A repeat download from this device does not spend another pickup.
    if (!alreadyClaimed) {
      meta.value.downloadCount += 1
      if (remainingBefore !== null) {
        meta.value.remainingDownloads = Math.max(0, remainingBefore - 1)
      }
    }
    meta.value.deviceConsumed = true
  } catch (caught) {
    if (caught instanceof ApiError && (caught.status === 404 || caught.status === 410)) {
      await load(code.value)
      return
    }
    notifyError(caught instanceof ApiError ? caught.message : '下载失败，请稍后再试')
  } finally {
    busy.value = false
  }
}

async function openText() {
  if (busy.value || !meta.value || meta.value.kind !== 'text') {
    return
  }
  busy.value = true
  try {
    revealed.value = await revealText(code.value)
    consumedThisPickup.value = revealed.value.consumed
    meta.value.downloadCount = revealed.value.downloadCount
    meta.value.remainingDownloads = revealed.value.remainingDownloads
    meta.value.deviceConsumed = true
  } catch (caught) {
    if (caught instanceof ApiError && (caught.status === 404 || caught.status === 410)) {
      await load(code.value)
      return
    }
    notifyError(caught instanceof ApiError ? caught.message : '查看失败，请稍后再试')
  } finally {
    busy.value = false
  }
}

function primaryAction() {
  if (meta.value?.kind === 'text') {
    void openText()
    return
  }
  void downloadFiles()
}

function saveText() {
  if (!revealed.value) {
    return
  }
  const blob = new Blob([revealed.value.text], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = `递我-${meta.value?.code ?? '文本'}.txt`
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

watch(
  () => route.params.code,
  (value) => {
    void load(typeof value === 'string' ? value : '')
  },
  { immediate: true },
)

onMounted(() => {
  clock = setInterval(() => {
    now.value = Date.now()
  }, 30_000)
})

onBeforeUnmount(() => {
  if (clock) {
    clearInterval(clock)
  }
})
</script>

<template>
  <StageFrame
    rail="pickup"
    footer="打开取件页不计数，每台设备首次下载或查看文本才计 1 次 · 图源 Bing / Picsum"
    @home="goHome"
  >
    <div class="pickup-page">
      <section ref="cardEl" class="ct-panel pickup-card">
        <header class="card-head">
          <RouterLink to="/" class="card-brand" aria-label="递我 · 返回寄件">
            <span class="brand-mark">
              <AppIcon name="relay" :size="18" :stroke-width="1.55" />
            </span>
            <span class="brand-words">
              <strong>递我</strong>
              <em>PICKUP</em>
            </span>
          </RouterLink>

          <span
            class="status-pill"
            :class="{
              'is-ready': !loading && !loadFailure && badgeStatus === 'active',
              'is-muted': !loading && (!!loadFailure || badgeStatus !== 'active'),
            }"
          >
            <i aria-hidden="true"></i>
            {{ loading ? '读取中' : statusLabel }}
          </span>
        </header>

        <div v-if="loading" class="card-state" aria-busy="true" aria-label="正在读取分享">
          <p class="panel-eyebrow">取件码 {{ formatCode(code) || '········' }}</p>
          <h1 class="card-title">正在取件</h1>
          <div class="pickup-skeleton" aria-hidden="true">
            <span class="skeleton-line is-long"></span>
            <span class="skeleton-line is-wide"></span>
            <span class="skeleton-line is-short"></span>
            <span class="skeleton-pill"></span>
          </div>
        </div>

        <div v-else-if="loadFailure" class="card-state">
          <p class="panel-eyebrow">PICKUP</p>
          <h1 class="card-title">{{ loadFailure.title }}</h1>
          <p class="card-desc">{{ loadFailure.message }}</p>

          <div v-if="loadFailure.kind === 'not_found'" class="sheet-form">
            <PickupCodeForm compact button-label="打开" @submit="goPickup" />
          </div>

          <div class="card-actions">
            <button v-if="retryable" type="button" class="card-primary" @click="retry">
              重新连接
            </button>
            <button type="button" class="panel-link" @click="goHome">返回寄件</button>
          </div>
        </div>

        <div v-else-if="meta && effectiveStatus !== 'active'" class="card-state">
          <p class="panel-eyebrow">PICKUP · {{ formatCode(meta.code) }}</p>
          <h1 class="card-title">{{ unavailableTitle }}</h1>
          <p class="card-desc">{{ unavailableDesc }}</p>

          <dl class="sheet-meta">
            <div>
              <dt>取件码</dt>
              <dd>{{ formatCode(meta.code) }}</dd>
            </div>
            <div>
              <dt>有效期</dt>
              <dd>{{ formatDateTime(meta.expiresAt) }}</dd>
            </div>
          </dl>

          <div class="card-actions">
            <button type="button" class="card-primary" @click="goHome">再寄一份</button>
          </div>
          <PickupCodeForm compact button-label="换一个" @submit="goPickup" />
        </div>

        <div v-else-if="revealed && meta" class="card-state text-sheet">
          <p class="panel-eyebrow">TEXT · {{ formatCode(meta.code) }}</p>
          <h1 class="card-title">文本已经打开</h1>
          <pre>{{ revealed.text }}</pre>
          <div class="text-actions">
            <CopyButton :text="revealed.text" label="复制文本" copied-label="已复制" />
            <button type="button" class="text-save" @click="saveText">保存 .txt</button>
          </div>
          <p class="text-foot">{{ revealedSize }} · {{ revealedRemaining }}</p>
          <button type="button" class="panel-link" @click="goHome">再寄一份</button>
        </div>

        <div v-else-if="meta" class="card-state">
          <p class="panel-eyebrow">{{ statusLabel }} · {{ formatCode(meta.code) }}</p>

          <div class="title-row">
            <FileGlyph v-if="meta.kind === 'files' && files.length === 1" :name="files[0].name" compact />
            <h1 class="card-title">{{ displayTitle }}</h1>
          </div>

          <p class="card-meta">
            <span v-if="meta.kind === 'files'">{{ formatBytes(totalSize) }}</span>
            <span v-else>{{ meta.textLength }} 个字符</span>
            <span>{{ expiryHint(meta.expiresAt) }}</span>
            <span>{{ remainingText }}</span>
          </p>

          <ul v-if="meta.kind === 'files' && files.length > 1" class="file-list">
            <li v-for="file in files" :key="file.id">
              <span class="file-name">{{ file.name }}</span>
              <span class="file-size">{{ formatBytes(file.size) }}</span>
            </li>
          </ul>

          <button
            type="button"
            class="card-primary"
            :disabled="locked || busy"
            @click="primaryAction"
          >
            {{ actionLabel }}
            <AppIcon
              v-if="!busy"
              :name="meta.kind === 'text' ? 'text' : 'download'"
              :size="16"
            />
          </button>

          <p v-if="downloaded" class="card-note is-good">{{ noteText }}</p>
          <p v-else class="card-note">{{ noteText }}</p>

          <div class="card-foot">
            <span class="card-code">取件码 <strong>{{ formatCode(meta.code) }}</strong></span>
            <button type="button" class="card-alt" @click="goHome">我也要寄一份 →</button>
          </div>
        </div>
      </section>
    </div>
  </StageFrame>
</template>

<style scoped>
.pickup-page {
  min-height: 100vh;
}

.pickup-card {
  position: fixed;
  top: 24px;
  left: 24px;
  z-index: 10;
  width: 380px;
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
}

.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  margin-bottom: 16px;
}

.card-brand {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 9px;
  text-decoration: none;
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

.status-pill {
  display: inline-flex;
  height: 26px;
  flex: 0 0 auto;
  align-items: center;
  gap: 5px;
  padding: 0 10px;
  color: var(--muted-light);
  background: var(--surface-strong);
  border-radius: 999px;
  font-size: 9.5px;
  font-weight: 600;
  letter-spacing: 0.04em;
}

.status-pill i {
  width: 5px;
  height: 5px;
  background: currentColor;
  border-radius: 50%;
}

.status-pill.is-ready {
  color: var(--brand-text);
  background: var(--pill-ready-bg);
}

.status-pill.is-muted {
  color: var(--muted-light);
}

.card-title {
  margin-top: 2px;
  color: var(--ink);
  font-size: 21px;
  font-weight: 600;
  letter-spacing: -0.01em;
  line-height: 1.3;
  overflow-wrap: anywhere;
}

.card-desc {
  margin-top: 6px;
  color: var(--muted);
  font-size: 12px;
  line-height: 1.65;
}

.title-row {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 4px;
}

.title-row .card-title {
  margin-top: 0;
}

.card-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 10px;
  margin-top: 8px;
  color: var(--muted-light);
  font-size: 10px;
  letter-spacing: 0.01em;
}

.card-meta span + span::before {
  content: '·';
  margin-right: 10px;
  color: var(--line-strong);
}

.file-list {
  max-height: 176px;
  margin-top: 14px;
  overflow-y: auto;
  overscroll-behavior: contain;
}

.file-list li {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 16px;
  padding: 9px 0;
  border-top: 1px solid var(--line-row);
}

.file-list li:last-child {
  border-bottom: 1px solid var(--line-row);
}

.file-name {
  min-width: 0;
  overflow: hidden;
  color: var(--ink-soft);
  font-size: 11.5px;
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.file-size {
  flex: 0 0 auto;
  color: var(--muted-light);
  font-family: var(--font-mono);
  font-size: 9px;
}

.card-primary {
  display: flex;
  width: 100%;
  height: 46px;
  align-items: center;
  justify-content: center;
  gap: 8px;
  margin-top: 18px;
  color: #fff;
  background: var(--brand);
  border-radius: 999px;
  font-size: 13px;
  font-weight: 600;
  transition:
    background-color 150ms ease,
    scale 150ms ease;
}

@media (hover: hover) {
  .card-primary:not(:disabled):hover {
    background: var(--brand-deep);
  }
}

.card-primary:disabled {
  color: rgba(255, 255, 255, 0.55);
  background: #b9b2a7;
  cursor: not-allowed;
}

.card-primary:not(:disabled):active {
  scale: 0.97;
}

.card-note {
  margin-top: 10px;
  color: var(--muted-light);
  font-size: 9.5px;
  line-height: 1.55;
}

.card-note.is-good {
  color: var(--green);
}

.card-foot {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px 12px;
  margin-top: 16px;
  padding-top: 12px;
  border-top: 1px solid var(--line-row);
}

.card-code {
  color: var(--muted-light);
  font-family: var(--font-mono);
  font-size: 8.5px;
  letter-spacing: 0.12em;
}

.card-code strong {
  color: var(--ink-soft);
  font-weight: 600;
  letter-spacing: 0.16em;
}

.card-alt {
  color: var(--muted);
  font-size: 10.5px;
  letter-spacing: 0.02em;
  transition: color 150ms ease;
}

@media (hover: hover) {
  .card-alt:hover {
    color: var(--ink);
  }
}

.card-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  margin-top: 16px;
}

.card-actions .card-primary {
  width: auto;
  margin-top: 0;
  padding: 0 22px;
}

.card-actions .panel-link {
  margin-top: 0;
}

.sheet-form {
  margin-top: 14px;
}

.card-actions + .pickup-code-form {
  margin-top: 12px;
}

.sheet-meta {
  display: flex;
  gap: 28px;
  margin: 16px 0 0;
  padding: 11px 0;
  border-top: 1px solid var(--line-row);
  border-bottom: 1px solid var(--line-row);
}

.sheet-meta dt {
  color: var(--muted-light);
  font-size: 9.5px;
}

.sheet-meta dd {
  margin: 2px 0 0;
  font-family: var(--font-mono);
  font-size: 11px;
  font-weight: 500;
  letter-spacing: 0.04em;
}

.text-sheet pre {
  max-height: 268px;
  margin: 12px 0 0;
  padding: 13px;
  overflow: auto;
  overscroll-behavior: contain;
  color: var(--ink-soft);
  background: var(--surface-row);
  border-radius: 12px;
  font-family: var(--font-sans);
  font-size: 12px;
  line-height: 1.8;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.text-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
}

.text-actions :deep(.el-button) {
  height: 30px;
  margin: 0;
  padding: 0 13px;
  color: var(--ink);
  background: var(--surface-strong);
  border: 1px solid var(--line-tile);
  border-radius: 999px;
  font-size: 10px;
  font-weight: 600;
}

.text-save {
  height: 30px;
  padding: 0 14px;
  color: var(--ink-soft);
  border: 1px solid var(--line-dash);
  border-radius: 999px;
  font-size: 10px;
  font-weight: 600;
  transition:
    background-color 150ms ease,
    border-color 150ms ease;
}

@media (hover: hover) {
  .text-save:hover {
    background: var(--surface-chip);
    border-color: var(--line-dash-hover);
  }
}

.text-foot {
  margin-top: 10px;
  color: var(--muted-light);
  font-size: 9.5px;
}

.pickup-skeleton {
  display: flex;
  flex-direction: column;
  gap: 11px;
  margin-top: 22px;
}

.skeleton-line {
  display: block;
  height: 11px;
  background: var(--skeleton-fill);
  border-radius: 999px;
  animation: skeleton-breathe 1.6s ease-in-out infinite alternate;
}

.skeleton-line.is-long {
  width: 78%;
}

.skeleton-line.is-wide {
  width: 100%;
  height: 32px;
  border-radius: 8px;
}

.skeleton-line.is-short {
  width: 56%;
}

.skeleton-pill {
  display: block;
  width: 132px;
  height: 40px;
  margin-top: 5px;
  background: var(--skeleton-fill);
  border-radius: 999px;
  animation: skeleton-breathe 1.6s ease-in-out infinite alternate;
}

@keyframes skeleton-breathe {
  from {
    opacity: 0.5;
  }
  to {
    opacity: 1;
  }
}

@media (max-width: 860px) {
  .pickup-card {
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
  .pickup-card {
    padding: 16px;
    border-radius: 16px;
  }

  .card-title {
    font-size: 19px;
  }
}
</style>
