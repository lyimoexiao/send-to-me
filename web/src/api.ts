import {
  ApiError,
  type AdminBackgroundInput,
  type AdminSession,
  type AdminSettings,
  type CreatedShare,
  type CreateShareInput,
  type PublicConfig,
  type RevealedText,
  type ShareMeta,
} from './types'

interface ErrorEnvelope {
  error?: {
    code?: string
    message?: string
  }
}

async function parseError(response: Response): Promise<ApiError> {
  if (response.status === 413) {
    return new ApiError(413, 'too_large', '上传内容超过大小上限')
  }
  try {
    const body = (await response.json()) as ErrorEnvelope
    return new ApiError(
      response.status,
      body.error?.code ?? 'request_failed',
      body.error?.message ?? '请求失败，请稍后再试',
    )
  } catch {
    return new ApiError(response.status, 'request_failed', '请求失败，请稍后再试')
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let response: Response
  try {
    response = await fetch(path, {
      cache: 'no-store',
      ...init,
      headers: {
        Accept: 'application/json',
        ...(init?.headers ?? {}),
      },
    })
  } catch {
    throw new ApiError(0, 'network', '网络连接失败，请检查网络后重试')
  }
  if (!response.ok) {
    throw await parseError(response)
  }
  return (await response.json()) as T
}

export function getPublicConfig(): Promise<PublicConfig> {
  return request<PublicConfig>('/api/config')
}

function jsonInit(method: string, payload: unknown): RequestInit {
  return {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  }
}

export function getAdminSession(): Promise<AdminSession> {
  return request<AdminSession>('/api/admin/session')
}

export function adminLogin(password: string): Promise<{ authenticated: boolean }> {
  return request('/api/admin/login', jsonInit('POST', { password }))
}

export function adminLogout(): Promise<{ authenticated: boolean }> {
  return request('/api/admin/logout', { method: 'POST' })
}

export function getAdminSettings(): Promise<AdminSettings> {
  return request<AdminSettings>('/api/admin/settings')
}

export function saveAdminSettings(settings: AdminSettings): Promise<AdminSettings> {
  return request<AdminSettings>('/api/admin/settings', jsonInit('PUT', settings))
}

export function addAdminBackground(input: AdminBackgroundInput): Promise<AdminSettings> {
  return request<AdminSettings>('/api/admin/backgrounds', jsonInit('POST', input))
}

export function deleteAdminBackground(id: string): Promise<AdminSettings> {
  return request<AdminSettings>(`/api/admin/backgrounds/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
}

export async function uploadAdminBackground(file: File, label: string): Promise<AdminSettings> {
  const form = new FormData()
  form.append('file', file, file.name)
  if (label.trim()) {
    form.append('label', label.trim())
  }
  let response: Response
  try {
    response = await fetch('/api/admin/backgrounds/upload', {
      method: 'POST',
      body: form,
      cache: 'no-store',
    })
  } catch {
    throw new ApiError(0, 'network', '网络连接失败，请检查网络后重试')
  }
  if (!response.ok) {
    throw await parseError(response)
  }
  return (await response.json()) as AdminSettings
}

export function getShare(code: string): Promise<ShareMeta> {
  return request<ShareMeta>(`/api/shares/${encodeURIComponent(code)}`)
}

export function revealText(code: string): Promise<RevealedText> {
  return request<RevealedText>(`/api/shares/${encodeURIComponent(code)}/text`)
}

export async function checkDownload(code: string): Promise<number | null> {
  let response: Response
  try {
    response = await fetch(`/api/shares/${encodeURIComponent(code)}/download`, {
      method: 'HEAD',
      cache: 'no-store',
    })
  } catch {
    throw new ApiError(0, 'network', '网络连接失败，请检查网络后重试')
  }
  if (!response.ok) {
    throw await parseError(response)
  }
  const remaining = response.headers.get('X-Share-Remaining')
  if (!remaining || remaining === 'unlimited') {
    return null
  }
  const value = Number.parseInt(remaining, 10)
  return Number.isNaN(value) ? null : value
}

export function createShare(
  input: CreateShareInput,
  onProgress: (percent: number) => void,
): Promise<CreatedShare> {
  return new Promise((resolve, reject) => {
    const form = new FormData()
    form.append('kind', input.kind)
    form.append('expires_in', String(input.expiresIn))
    form.append('max_downloads', String(input.maxDownloads))
    if (input.kind === 'text') {
      form.append('text', input.text)
    } else {
      for (const file of input.files) {
        form.append('files', file, file.name)
      }
    }

    const xhr = new XMLHttpRequest()
    xhr.open('POST', '/api/shares')
    xhr.responseType = 'text'
    xhr.upload.addEventListener('progress', (event) => {
      if (event.lengthComputable && event.total > 0) {
        onProgress(Math.min(99, Math.round((event.loaded / event.total) * 100)))
      }
    })
    xhr.addEventListener('load', () => {
      let body: unknown = null
      try {
        body = xhr.responseText ? JSON.parse(xhr.responseText) : null
      } catch {
        body = null
      }
      if (xhr.status >= 200 && xhr.status < 300 && body) {
        onProgress(100)
        resolve(body as CreatedShare)
        return
      }
      const envelope = body as ErrorEnvelope | null
      reject(
        new ApiError(
          xhr.status,
          envelope?.error?.code ?? 'request_failed',
          envelope?.error?.message ?? '发送失败，请稍后再试',
        ),
      )
    })
    xhr.addEventListener('error', () => {
      reject(new ApiError(0, 'network', '网络连接失败，请检查网络后重试'))
    })
    xhr.addEventListener('abort', () => {
      reject(new ApiError(0, 'aborted', '发送已取消'))
    })
    xhr.send(form)
  })
}
