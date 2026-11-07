<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import AppIcon from '../components/AppIcon.vue'
import {
  addAdminBackground,
  adminLogin,
  adminLogout,
  deleteAdminBackground,
  getAdminSession,
  getAdminSettings,
  saveAdminSettings,
  uploadAdminBackground,
} from '../api'
import { formatDuration } from '../format'
import { notifyError, notifySuccess, notifyWarning } from '../notify'
import { ensureSiteConfig } from '../site'
import {
  ApiError,
  type AdminSession,
  type AdminSettings,
  type Background,
  type BackgroundKind,
} from '../types'

const session = ref<AdminSession | null>(null)
const checking = ref(true)
const password = ref('')
const loggingIn = ref(false)
const loading = ref(false)
const saving = ref(false)
const backgrounds = ref<Background[]>([])
const confirmingId = ref('')
const uploadInput = ref<HTMLInputElement | null>(null)
const uploadLabel = ref('')
const uploading = ref(false)
const newLabel = ref('')
const newUrl = ref('')
const addingUrl = ref(false)

const form = reactive({
  maxUploadMb: 2048,
  maxFiles: 20,
  maxTextKb: 64,
  defaultExpirySeconds: 7 * 24 * 60 * 60,
  maxExpirySeconds: 365 * 24 * 60 * 60,
  defaultMaxDownloads: 5,
})

const expiryPresets = [3600, 6 * 3600, 24 * 3600, 7 * 24 * 3600, 30 * 24 * 3600, 90 * 24 * 3600, 365 * 24 * 3600]

const maxExpiryOptions = computed(() => {
  const values = new Set(expiryPresets)
  values.add(form.maxExpirySeconds)
  return [...values]
    .filter((value) => value > 0)
    .sort((left, right) => left - right)
    .map((value) => ({ value, label: formatDuration(value) }))
})

const defaultExpiryOptions = computed(() => {
  const values = new Set(expiryPresets.filter((value) => value <= form.maxExpirySeconds))
  values.add(form.maxExpirySeconds)
  values.add(form.defaultExpirySeconds)
  return [...values]
    .filter((value) => value > 0 && value <= form.maxExpirySeconds)
    .sort((left, right) => left - right)
    .map((value) => ({ value, label: formatDuration(value) }))
})

watch(
  () => form.maxExpirySeconds,
  (max) => {
    if (form.defaultExpirySeconds > max) {
      form.defaultExpirySeconds = max
    }
  },
)

const kindLabels: Record<BackgroundKind, string> = {
  bing: 'Bing 每日一图',
  picsum: 'Picsum',
  upload: '本地上传',
  url: '外链图片',
}

async function loadSession() {
  try {
    session.value = await getAdminSession()
  } catch {
    session.value = { enabled: false, authenticated: false }
  }
  checking.value = false
  if (session.value.authenticated) {
    await loadSettings()
  }
}

async function loadSettings() {
  loading.value = true
  try {
    applySettings(await getAdminSettings())
  } catch (caught) {
    if (caught instanceof ApiError && caught.status === 401 && session.value) {
      session.value = { ...session.value, authenticated: false }
    }
    notifyError(caught instanceof ApiError ? caught.message : '读取配置失败，请稍后再试')
  } finally {
    loading.value = false
  }
}

function applySettings(next: AdminSettings) {
  backgrounds.value = next.backgrounds ?? []
  form.maxUploadMb = Math.round(next.maxUploadBytes / (1024 * 1024))
  form.maxFiles = next.maxFiles
  form.maxTextKb = Math.round(next.maxTextBytes / 1024)
  form.defaultExpirySeconds = next.defaultExpirySeconds
  form.maxExpirySeconds = next.maxExpirySeconds
  form.defaultMaxDownloads = next.defaultMaxDownloads
}

async function login() {
  if (loggingIn.value || !password.value) {
    return
  }
  loggingIn.value = true
  try {
    await adminLogin(password.value)
    password.value = ''
    if (session.value) {
      session.value = { ...session.value, authenticated: true }
    }
    await loadSettings()
    notifySuccess('已登录管理后台')
  } catch (caught) {
    notifyError(caught instanceof ApiError ? caught.message : '登录失败，请稍后再试')
  } finally {
    loggingIn.value = false
  }
}

async function logout() {
  try {
    await adminLogout()
  } catch {
    // Clearing local state is enough when the session already expired.
  }
  session.value = session.value ? { ...session.value, authenticated: false } : null
  backgrounds.value = []
}

function buildPayload(): AdminSettings {
  return {
    maxUploadBytes: Math.max(1, Math.round(Number(form.maxUploadMb) || 1)) * 1024 * 1024,
    maxFiles: Math.max(1, Math.round(Number(form.maxFiles) || 1)),
    maxTextBytes: Math.max(1, Math.round(Number(form.maxTextKb) || 1)) * 1024,
    defaultExpirySeconds: Number(form.defaultExpirySeconds),
    maxExpirySeconds: Number(form.maxExpirySeconds),
    defaultMaxDownloads: Math.max(0, Math.round(Number(form.defaultMaxDownloads) || 0)),
    backgrounds: backgrounds.value,
  }
}

async function saveLimits() {
  if (saving.value) {
    return
  }
  saving.value = true
  try {
    applySettings(await saveAdminSettings(buildPayload()))
    await ensureSiteConfig(true)
    notifySuccess('配置已保存')
  } catch (caught) {
    notifyError(caught instanceof ApiError ? caught.message : '保存配置失败，请稍后再试')
  } finally {
    saving.value = false
  }
}

function previewFor(background: Background): string {
  if (background.kind === 'bing') {
    return 'https://bing.biturl.top/?resolution=640&format=image&index=0&mkt=zh-CN'
  }
  if (background.url) {
    return background.url
  }
  if (background.kind === 'picsum') {
    return `https://picsum.photos/seed/${encodeURIComponent(background.id)}/640/400`
  }
  return ''
}

function hostOf(url?: string) {
  if (!url) {
    return ''
  }
  try {
    return new URL(url).hostname
  } catch {
    return url
  }
}

function pickUpload() {
  if (!uploading.value) {
    uploadInput.value?.click()
  }
}

async function onUpload(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) {
    return
  }
  if (file.size > 8 * 1024 * 1024) {
    notifyWarning('背景图不能超过 8 MB')
    return
  }
  uploading.value = true
  try {
    applySettings(await uploadAdminBackground(file, uploadLabel.value))
    uploadLabel.value = ''
    await ensureSiteConfig(true)
    notifySuccess('背景图已上传')
  } catch (caught) {
    notifyError(caught instanceof ApiError ? caught.message : '上传失败，请稍后再试')
  } finally {
    uploading.value = false
  }
}

async function addUrl() {
  const url = newUrl.value.trim()
  if (!url || addingUrl.value) {
    return
  }
  if (!/^https?:\/\//i.test(url)) {
    notifyWarning('请填写 http(s) 图片链接')
    return
  }
  addingUrl.value = true
  try {
    applySettings(
      await addAdminBackground({
        kind: 'url',
        url,
        label: newLabel.value.trim(),
      }),
    )
    newUrl.value = ''
    newLabel.value = ''
    await ensureSiteConfig(true)
    notifySuccess('背景图已添加')
  } catch (caught) {
    notifyError(caught instanceof ApiError ? caught.message : '添加失败，请稍后再试')
  } finally {
    addingUrl.value = false
  }
}

function requestDelete(background: Background) {
  confirmingId.value = confirmingId.value === background.id ? '' : background.id
}

async function removeBackground(background: Background) {
  confirmingId.value = ''
  try {
    applySettings(await deleteAdminBackground(background.id))
    await ensureSiteConfig(true)
    notifySuccess('背景图已删除')
  } catch (caught) {
    notifyError(caught instanceof ApiError ? caught.message : '删除失败，请稍后再试')
  }
}

onMounted(loadSession)
</script>

<template>
  <main class="admin">
    <div class="admin-shell">
      <header class="admin-head">
        <RouterLink to="/" class="admin-brand" aria-label="返回寄件页">
          <span class="brand-mark">
            <AppIcon name="relay" :size="19" :stroke-width="1.55" />
          </span>
          <span>
            <span class="admin-eyebrow">ADMIN CONSOLE</span>
            <strong>递我 · 管理后台</strong>
          </span>
        </RouterLink>
        <div class="admin-actions">
          <RouterLink to="/" class="admin-link">返回寄件页</RouterLink>
          <button
            v-if="session?.authenticated"
            type="button"
            class="admin-ghost"
            @click="logout"
          >
            退出登录
          </button>
        </div>
      </header>

      <section v-if="checking" class="ct-panel admin-card admin-muted">正在检查登录状态…</section>

      <section v-else-if="!session?.enabled" class="ct-panel admin-card admin-muted">
        <p class="admin-muted-title">管理后台尚未开启</p>
        <p>请为服务设置环境变量 <code>ADMIN_PASSWORD</code> 后重启，即可在此登录。</p>
      </section>

      <form
        v-else-if="!session?.authenticated"
        class="ct-panel admin-card admin-login"
        @submit.prevent="login"
      >
        <span class="login-badge">
          <AppIcon name="lock" :size="15" :stroke-width="1.8" />
        </span>
        <h1>进入管理后台</h1>
        <p class="admin-note">输入服务端配置的管理员密码。会话保持 12 小时。</p>
        <el-input
          v-model="password"
          type="password"
          show-password
          size="large"
          placeholder="管理员密码"
          autocomplete="current-password"
          :disabled="loggingIn"
        />
        <el-button
          type="primary"
          size="large"
          native-type="submit"
          class="login-submit"
          :loading="loggingIn"
          :disabled="!password"
        >
          登录
        </el-button>
      </form>

      <template v-else>
        <section class="ct-panel admin-card">
          <header class="section-head">
            <div>
              <p class="admin-eyebrow">SEND LIMITS</p>
              <h2>寄件限制</h2>
            </div>
            <span class="section-hint">保存后立即对新寄件生效</span>
          </header>

          <div v-if="loading" class="admin-muted">正在读取配置…</div>

          <div v-else class="field-grid">
            <label class="field">
              <span class="setting-label">上传上限（MB）</span>
              <el-input-number
                v-model="form.maxUploadMb"
                :min="1"
                :max="10240"
                :step="128"
                controls-position="right"
              />
            </label>
            <label class="field">
              <span class="setting-label">单次文件数</span>
              <el-input-number v-model="form.maxFiles" :min="1" :max="100" controls-position="right" />
            </label>
            <label class="field">
              <span class="setting-label">文本上限（KB）</span>
              <el-input-number v-model="form.maxTextKb" :min="1" :max="1024" controls-position="right" />
            </label>
            <label class="field">
              <span class="setting-label">默认取件设备数（0 为不限）</span>
              <el-input-number
                v-model="form.defaultMaxDownloads"
                :min="0"
                :max="1000"
                controls-position="right"
              />
            </label>
            <label class="field">
              <span class="setting-label">默认有效期</span>
              <el-select v-model="form.defaultExpirySeconds" size="default">
                <el-option
                  v-for="option in defaultExpiryOptions"
                  :key="option.value"
                  :label="option.label"
                  :value="option.value"
                />
              </el-select>
            </label>
            <label class="field">
              <span class="setting-label">最长有效期</span>
              <el-select v-model="form.maxExpirySeconds" size="default">
                <el-option
                  v-for="option in maxExpiryOptions"
                  :key="option.value"
                  :label="option.label"
                  :value="option.value"
                />
              </el-select>
            </label>
          </div>

          <footer class="section-foot">
            <el-button type="primary" :loading="saving" :disabled="loading" @click="saveLimits">
              保存配置
            </el-button>
          </footer>
        </section>

        <section class="ct-panel admin-card">
          <header class="section-head">
            <div>
              <p class="admin-eyebrow">BACKGROUNDS</p>
              <h2>背景图</h2>
            </div>
            <span class="section-hint">{{ backgrounds.length }} / 30 张</span>
          </header>
          <p class="admin-note">
            寄件页会在这些背景之间轮换；未配置时使用内置的 Bing 与 Picsum 图源。
          </p>

          <div class="bg-grid">
            <article v-for="background in backgrounds" :key="background.id" class="bg-card">
              <div class="bg-thumb">
                <img
                  v-if="previewFor(background)"
                  :src="previewFor(background)"
                  alt=""
                  loading="lazy"
                />
                <span v-else class="bg-fallback"><AppIcon name="file" :size="18" /></span>
              </div>
              <div class="bg-meta">
                <p class="bg-label">{{ background.label }}</p>
                <p class="bg-kind">
                  {{ kindLabels[background.kind] }}
                  <template v-if="background.kind === 'url'"> · {{ hostOf(background.url) }}</template>
                </p>
              </div>
              <button
                type="button"
                class="bg-remove"
                :aria-label="`删除背景 ${background.label}`"
                @click="requestDelete(background)"
              >
                <AppIcon name="trash" :size="14" :stroke-width="1.8" />
              </button>
              <div v-if="confirmingId === background.id" class="bg-confirm">
                <span>删除这张背景？</span>
                <button type="button" class="is-danger" @click="removeBackground(background)">
                  删除
                </button>
                <button type="button" @click="confirmingId = ''">取消</button>
              </div>
            </article>

            <button type="button" class="bg-add" :disabled="uploading" @click="pickUpload">
              <AppIcon :name="uploading ? 'refresh' : 'plus'" :size="18" :stroke-width="1.7" />
              <span>{{ uploading ? '上传中…' : '上传图片' }}</span>
              <small>JPG / PNG / WebP / GIF · 8 MB</small>
            </button>
          </div>

          <div class="bg-upload-name">
            <el-input v-model="uploadLabel" placeholder="上传名称（可选）" />
            <span>上传图片时可自定义名称，留空则使用文件名</span>
          </div>

          <div class="bg-url">
            <el-input v-model="newLabel" placeholder="名称（可选）" class="bg-url-label" />
            <el-input v-model="newUrl" placeholder="https://… 图片链接" />
            <el-button :loading="addingUrl" :disabled="!newUrl.trim()" @click="addUrl">
              添加链接
            </el-button>
          </div>

          <input
            ref="uploadInput"
            class="sr-only"
            type="file"
            accept="image/jpeg,image/png,image/webp,image/gif"
            tabindex="-1"
            @change="onUpload"
          />
        </section>
      </template>
    </div>
  </main>
</template>

<style scoped>
.admin {
  position: relative;
  z-index: 1;
  display: flex;
  min-height: 100vh;
  justify-content: center;
  padding: 38px 22px 64px;
}

.admin-shell {
  display: flex;
  width: min(940px, 100%);
  flex-direction: column;
  gap: 18px;
}

.admin-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
  color: #fff;
}

.admin-brand {
  display: flex;
  align-items: center;
  gap: 12px;
  text-decoration: none;
}

.brand-mark {
  display: grid;
  width: 42px;
  height: 42px;
  flex: 0 0 auto;
  place-items: center;
  color: #fff;
  background: var(--brand);
  border-radius: 50%;
  box-shadow:
    0 16px 34px -20px rgba(10, 4, 12, 0.62),
    0 0 0 4px rgba(255, 255, 255, 0.14);
}

.admin-brand strong {
  display: block;
  font-size: 18px;
  font-weight: 600;
  letter-spacing: 0.01em;
}

.admin-eyebrow {
  display: block;
  margin-bottom: 2px;
  color: rgba(255, 255, 255, 0.62);
  font-family: var(--font-mono);
  font-size: 8.5px;
  letter-spacing: 0.22em;
  text-transform: uppercase;
}

.admin-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.admin-link,
.admin-ghost {
  display: inline-flex;
  height: 38px;
  align-items: center;
  padding: 0 16px;
  color: var(--nav-ink);
  background: var(--glass-strong);
  border: 1px solid var(--glass-border);
  border-radius: 999px;
  box-shadow: 0 16px 38px -22px rgba(10, 4, 12, 0.6);
  backdrop-filter: blur(16px) saturate(1.2);
  -webkit-backdrop-filter: blur(16px) saturate(1.2);
  font-size: 12px;
  font-weight: 600;
  text-decoration: none;
  transition:
    background-color 150ms ease,
    translate 150ms ease,
    scale 150ms ease;
}

@media (hover: hover) {
  .admin-link:hover,
  .admin-ghost:hover {
    background: var(--glass-strong-hover);
  }
}

.admin-link:active,
.admin-ghost:active {
  scale: 0.96;
}

.admin-card {
  width: 100%;
  padding: 26px 28px;
}

.admin-card h2 {
  font-size: 17px;
  font-weight: 600;
}

.admin-muted {
  color: var(--muted);
  font-size: 13px;
}

.admin-muted code {
  padding: 2px 6px;
  background: var(--surface-chip);
  border-radius: 6px;
  font-family: var(--font-mono);
  font-size: 12px;
}

.admin-muted-title {
  margin-bottom: 6px;
  color: var(--ink);
  font-size: 17px;
  font-weight: 600;
}

.admin-login {
  display: flex;
  max-width: 420px;
  flex-direction: column;
  gap: 14px;
  align-self: center;
  padding: 30px;
}

.login-badge {
  display: grid;
  width: 40px;
  height: 40px;
  place-items: center;
  color: var(--brand-text);
  background: var(--brand-wash);
  border-radius: 12px;
}

.admin-login h1 {
  font-size: 20px;
  font-weight: 600;
}

.admin-note {
  color: var(--muted);
  font-size: 12px;
  line-height: 1.6;
}

.login-submit {
  width: 100%;
  height: 46px;
  margin-top: 2px;
  border-radius: 999px;
}

.section-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 18px;
}

.section-head .admin-eyebrow {
  color: var(--muted-light);
}

.section-hint {
  flex: 0 0 auto;
  padding-top: 3px;
  color: var(--muted-light);
  font-family: var(--font-mono);
  font-size: 9.5px;
  letter-spacing: 0.04em;
}

.field-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px 20px;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 7px;
}

.field .el-select {
  width: 100%;
}

.field :deep(.el-input-number) {
  width: 100%;
}

.field :deep(.el-input__inner) {
  text-align: left;
}

.section-foot {
  display: flex;
  justify-content: flex-end;
  margin-top: 20px;
  padding-top: 16px;
  border-top: 1px solid var(--line-row);
}

.section-foot .el-button {
  min-width: 118px;
  height: 40px;
  border-radius: 999px;
}

.bg-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  margin-top: 16px;
}

.bg-card {
  position: relative;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--surface-row);
  border: 1px solid var(--line-row);
  border-radius: 14px;
}

.bg-thumb {
  position: relative;
  aspect-ratio: 16 / 10;
  overflow: hidden;
  background: var(--surface-soft);
}

.bg-thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  outline: 1px solid oklch(0 0 0 / 0.1);
  outline-offset: -1px;
}

.bg-fallback {
  display: grid;
  width: 100%;
  height: 100%;
  place-items: center;
  color: var(--muted);
}

.bg-meta {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 9px 11px 10px;
}

.bg-label {
  overflow: hidden;
  color: var(--ink);
  font-size: 12.5px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.bg-kind {
  overflow: hidden;
  color: var(--muted-light);
  font-size: 10px;
  letter-spacing: 0.02em;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.bg-remove {
  position: absolute;
  top: 7px;
  right: 7px;
  display: grid;
  width: 26px;
  height: 26px;
  place-items: center;
  color: #fff;
  background: rgba(20, 10, 18, 0.52);
  border-radius: 50%;
  backdrop-filter: blur(6px);
  -webkit-backdrop-filter: blur(6px);
  opacity: 0;
  transition:
    opacity 150ms ease,
    background-color 150ms ease;
}

.bg-card:hover .bg-remove,
.bg-card:focus-within .bg-remove {
  opacity: 1;
}

@media (hover: hover) {
  .bg-remove:hover {
    background: var(--red);
  }
}

.bg-confirm {
  position: absolute;
  inset: 0;
  display: flex;
  flex-wrap: wrap;
  align-content: center;
  justify-content: center;
  gap: 6px;
  padding: 10px;
  color: #fff;
  background: rgba(22, 12, 20, 0.8);
  backdrop-filter: blur(4px);
  -webkit-backdrop-filter: blur(4px);
  font-size: 11px;
  text-align: center;
}

.bg-confirm span {
  width: 100%;
  margin-bottom: 2px;
}

.bg-confirm button {
  height: 26px;
  padding: 0 12px;
  color: #fff;
  background: rgba(255, 255, 255, 0.16);
  border-radius: 999px;
  font-size: 11px;
  font-weight: 600;
}

.bg-confirm button.is-danger {
  background: var(--red);
}

.bg-add {
  display: flex;
  min-height: 132px;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  gap: 5px;
  color: var(--muted);
  border: 1px dashed var(--line-dash);
  border-radius: 14px;
  transition:
    color 150ms ease,
    border-color 150ms ease,
    background-color 150ms ease;
}

.bg-add span {
  color: var(--ink-soft);
  font-size: 12.5px;
  font-weight: 600;
}

.bg-add small {
  color: var(--muted-light);
  font-size: 9.5px;
  letter-spacing: 0.02em;
}

@media (hover: hover) {
  .bg-add:hover:not(:disabled) {
    color: var(--brand-text);
    background: var(--brand-wash);
    border-color: var(--line-dash-hover);
  }
}

.bg-add:disabled {
  cursor: progress;
  opacity: 0.64;
}

.bg-upload-name {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 14px;
}

.bg-upload-name .el-input {
  width: 220px;
  flex: 0 0 auto;
}

.bg-upload-name span {
  color: var(--muted-light);
  font-size: 10.5px;
}

.bg-url {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 14px;
  padding-top: 14px;
  border-top: 1px solid var(--line-row);
}

.bg-url-label {
  width: 150px;
  flex: 0 0 auto;
}

.bg-url .el-button {
  flex: 0 0 auto;
  height: 32px;
  border-radius: 999px;
}

@media (max-width: 720px) {
  .admin {
    padding: 18px 14px 48px;
  }

  .admin-card {
    padding: 20px;
  }

  .field-grid {
    grid-template-columns: 1fr;
  }

  .bg-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .bg-url {
    flex-wrap: wrap;
  }

  .bg-url-label {
    width: 100%;
  }

  .bg-upload-name {
    align-items: flex-start;
    flex-direction: column;
  }

  .bg-upload-name .el-input {
    width: 100%;
  }

  .bg-url > .el-input:not(.bg-url-label) {
    flex: 1 1 100%;
  }

  .bg-url .el-button {
    width: 100%;
  }

  .admin-link {
    display: none;
  }
}

@media (hover: none) {
  .bg-remove {
    opacity: 1;
  }
}

@media (prefers-color-scheme: dark) {
  .bg-thumb img {
    outline-color: oklch(1 0 0 / 0.1);
  }
}
</style>
