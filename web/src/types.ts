export type ShareKind = 'files' | 'text'
export type ShareStatus = 'active' | 'expired' | 'exhausted'

export interface ShareFile {
  id: string
  name: string
  size: number
  contentType: string
}

export interface ShareMeta {
  code: string
  kind: ShareKind
  files?: ShareFile[]
  textLength: number
  createdAt: string
  expiresAt: string | null
  maxDownloads: number
  downloadCount: number
  remainingDownloads: number | null
  status: ShareStatus
  deviceConsumed: boolean
}

export interface CreatedShare {
  code: string
  link: string
  kind: ShareKind
  fileCount: number
  expiresAt: string | null
  maxDownloads: number
}

export type BackgroundKind = 'bing' | 'picsum' | 'upload' | 'url'

export interface Background {
  id: string
  kind: BackgroundKind
  url?: string
  label: string
  credit?: string
}

export interface PublicConfig {
  maxUploadBytes: number
  maxFiles: number
  maxTextBytes: number
  defaultExpirySeconds: number
  maxExpirySeconds: number
  defaultMaxDownloads: number
  backgrounds: Background[]
}

export type AdminSettings = PublicConfig

export interface AdminSession {
  enabled: boolean
  authenticated: boolean
}

export type AdminBackgroundInput = Pick<Background, 'kind' | 'label'> & {
  url?: string
  credit?: string
}

export interface RevealedText {
  text: string
  downloadCount: number
  maxDownloads: number
  remainingDownloads: number | null
  consumed: boolean
}

export interface CreateShareInput {
  kind: ShareKind
  files: File[]
  text: string
  expiresIn: number
  maxDownloads: number
}

export class ApiError extends Error {
  status: number
  code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}
