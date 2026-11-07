export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return '0 B'
  }
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const exponent = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const value = bytes / 1024 ** exponent
  const decimals = exponent === 0 || value >= 100 ? 0 : value >= 10 ? 1 : 2
  return `${value.toFixed(decimals).replace(/\.0+$/, '').replace(/(\.\d)0$/, '$1')} ${units[exponent]}`
}

export function formatDuration(seconds: number): string {
  if (seconds % 86400 === 0) {
    return `${seconds / 86400} 天`
  }
  if (seconds % 3600 === 0) {
    return `${seconds / 3600} 小时`
  }
  return `${Math.max(1, Math.round(seconds / 60))} 分钟`
}

export function formatCode(code: string): string {
  const normalized = code.toUpperCase()
  // New codes are 6 characters (3 + 3); legacy 8-character links keep 4 + 4.
  const size = normalized.length % 3 === 0 ? 3 : 4
  return normalized.match(new RegExp(`.{1,${size}}`, 'g'))?.join(' ') ?? normalized
}

export function normalizeCodeInput(raw: string, maxLength = 6): string {
  return raw
    .toUpperCase()
    .replace(/[^A-Z0-9]/g, '')
    .slice(0, maxLength)
}

export function formatDateTime(value: string | null): string {
  if (!value) {
    return '永不过期'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return '—'
  }
  const now = new Date()
  const includeYear = date.getFullYear() !== now.getFullYear()
  return new Intl.DateTimeFormat('zh-CN', {
    year: includeYear ? 'numeric' : undefined,
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
    .format(date)
    .replace(/\//g, '.')
}

export function expiryHint(value: string | null): string {
  if (!value) {
    return '长期有效'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return '有效期未知'
  }
  const delta = date.getTime() - Date.now()
  if (delta <= 0) {
    return '已过期'
  }
  const minutes = Math.floor(delta / 60_000)
  if (minutes < 60) {
    return `剩余 ${Math.max(1, minutes)} 分钟`
  }
  const hours = Math.floor(minutes / 60)
  if (hours < 24) {
    return `剩余 ${hours} 小时`
  }
  const days = Math.floor(hours / 24)
  return `剩余 ${days} 天`
}

export function remainingLabel(remaining: number | null): string {
  if (remaining === null) {
    return '不限次数'
  }
  if (remaining <= 0) {
    return '次数已用完'
  }
  return `剩余 ${remaining} 次`
}

export function limitLabel(maxDownloads: number): string {
  return maxDownloads === 0 ? '不限设备数' : `可被 ${maxDownloads} 台设备取件`
}

export type FileAccent =
  | 'pdf'
  | 'doc'
  | 'sheet'
  | 'slides'
  | 'text'
  | 'json'
  | 'html'
  | 'xml'
  | 'archive'
  | 'image'
  | 'video'
  | 'audio'
  | 'code'
  | 'file'

interface FileFormat {
  icon: string
  accent: FileAccent
  label: string
}

// Extension → Carbon glyph key, accent family and human label.
const extensionMap: Record<string, FileFormat> = {
  png: { icon: 'png', accent: 'image', label: 'PNG 图片' },
  jpg: { icon: 'jpg', accent: 'image', label: 'JPEG 图片' },
  jpeg: { icon: 'jpg', accent: 'image', label: 'JPEG 图片' },
  gif: { icon: 'gif', accent: 'image', label: 'GIF 图片' },
  svg: { icon: 'svg', accent: 'image', label: 'SVG 图片' },
  webp: { icon: 'image', accent: 'image', label: 'WebP 图片' },
  avif: { icon: 'image', accent: 'image', label: 'AVIF 图片' },
  heic: { icon: 'image', accent: 'image', label: 'HEIC 图片' },
  bmp: { icon: 'image', accent: 'image', label: 'BMP 图片' },
  tif: { icon: 'image', accent: 'image', label: 'TIFF 图片' },
  tiff: { icon: 'image', accent: 'image', label: 'TIFF 图片' },
  ico: { icon: 'image', accent: 'image', label: '图标文件' },
  mp4: { icon: 'mp4', accent: 'video', label: 'MP4 视频' },
  mov: { icon: 'mov', accent: 'video', label: 'MOV 视频' },
  mkv: { icon: 'video', accent: 'video', label: 'MKV 视频' },
  webm: { icon: 'video', accent: 'video', label: 'WebM 视频' },
  avi: { icon: 'video', accent: 'video', label: 'AVI 视频' },
  mp3: { icon: 'mp3', accent: 'audio', label: 'MP3 音频' },
  wav: { icon: 'music', accent: 'audio', label: 'WAV 音频' },
  flac: { icon: 'music', accent: 'audio', label: 'FLAC 音频' },
  aac: { icon: 'music', accent: 'audio', label: 'AAC 音频' },
  m4a: { icon: 'music', accent: 'audio', label: 'M4A 音频' },
  ogg: { icon: 'music', accent: 'audio', label: 'OGG 音频' },
  opus: { icon: 'music', accent: 'audio', label: 'OPUS 音频' },
  pdf: { icon: 'pdf', accent: 'pdf', label: 'PDF 文档' },
  doc: { icon: 'doc', accent: 'doc', label: 'Word 文档' },
  docx: { icon: 'doc', accent: 'doc', label: 'Word 文档' },
  odt: { icon: 'doc', accent: 'doc', label: 'ODT 文档' },
  rtf: { icon: 'doc', accent: 'doc', label: 'RTF 文档' },
  txt: { icon: 'txt', accent: 'text', label: '文本文件' },
  log: { icon: 'txt', accent: 'text', label: '日志文件' },
  md: { icon: 'document', accent: 'text', label: 'Markdown' },
  markdown: { icon: 'document', accent: 'text', label: 'Markdown' },
  xls: { icon: 'xls', accent: 'sheet', label: 'Excel 表格' },
  xlsx: { icon: 'xls', accent: 'sheet', label: 'Excel 表格' },
  ods: { icon: 'xls', accent: 'sheet', label: '电子表格' },
  csv: { icon: 'csv', accent: 'sheet', label: 'CSV 表格' },
  tsv: { icon: 'csv', accent: 'sheet', label: 'TSV 表格' },
  ppt: { icon: 'ppt', accent: 'slides', label: 'PPT 演示' },
  pptx: { icon: 'ppt', accent: 'slides', label: 'PPT 演示' },
  odp: { icon: 'ppt', accent: 'slides', label: '演示文稿' },
  key: { icon: 'ppt', accent: 'slides', label: 'Keynote' },
  zip: { icon: 'zip', accent: 'archive', label: 'ZIP 压缩包' },
  rar: { icon: 'archive', accent: 'archive', label: 'RAR 压缩包' },
  '7z': { icon: 'archive', accent: 'archive', label: '7Z 压缩包' },
  tar: { icon: 'archive', accent: 'archive', label: 'TAR 归档' },
  gz: { icon: 'archive', accent: 'archive', label: 'GZ 压缩包' },
  bz2: { icon: 'archive', accent: 'archive', label: 'BZ2 压缩包' },
  xz: { icon: 'archive', accent: 'archive', label: 'XZ 压缩包' },
  js: { icon: 'code', accent: 'code', label: 'JavaScript' },
  mjs: { icon: 'code', accent: 'code', label: 'JavaScript' },
  ts: { icon: 'code', accent: 'code', label: 'TypeScript' },
  tsx: { icon: 'code', accent: 'code', label: 'TypeScript' },
  jsx: { icon: 'code', accent: 'code', label: 'JavaScript' },
  vue: { icon: 'code', accent: 'code', label: 'Vue 组件' },
  go: { icon: 'code', accent: 'code', label: 'Go 源码' },
  py: { icon: 'code', accent: 'code', label: 'Python' },
  rs: { icon: 'code', accent: 'code', label: 'Rust 源码' },
  c: { icon: 'code', accent: 'code', label: 'C 源码' },
  h: { icon: 'code', accent: 'code', label: 'C 头文件' },
  cpp: { icon: 'code', accent: 'code', label: 'C++ 源码' },
  cxx: { icon: 'code', accent: 'code', label: 'C++ 源码' },
  hpp: { icon: 'code', accent: 'code', label: 'C++ 头文件' },
  cs: { icon: 'code', accent: 'code', label: 'C# 源码' },
  rb: { icon: 'code', accent: 'code', label: 'Ruby' },
  php: { icon: 'code', accent: 'code', label: 'PHP' },
  swift: { icon: 'code', accent: 'code', label: 'Swift' },
  kt: { icon: 'code', accent: 'code', label: 'Kotlin' },
  sh: { icon: 'code', accent: 'code', label: 'Shell 脚本' },
  bash: { icon: 'code', accent: 'code', label: 'Shell 脚本' },
  sql: { icon: 'code', accent: 'code', label: 'SQL' },
  css: { icon: 'code', accent: 'code', label: 'CSS' },
  scss: { icon: 'code', accent: 'code', label: 'SCSS' },
  sass: { icon: 'code', accent: 'code', label: 'Sass' },
  less: { icon: 'code', accent: 'code', label: 'Less' },
  yaml: { icon: 'code', accent: 'code', label: 'YAML' },
  yml: { icon: 'code', accent: 'code', label: 'YAML' },
  toml: { icon: 'code', accent: 'code', label: 'TOML' },
  ini: { icon: 'code', accent: 'code', label: '配置文件' },
  conf: { icon: 'code', accent: 'code', label: '配置文件' },
  env: { icon: 'code', accent: 'code', label: '环境配置' },
  java: { icon: 'java', accent: 'code', label: 'Java 源码' },
  html: { icon: 'html', accent: 'html', label: 'HTML' },
  htm: { icon: 'html', accent: 'html', label: 'HTML' },
  xml: { icon: 'xml', accent: 'xml', label: 'XML' },
  json: { icon: 'json', accent: 'json', label: 'JSON' },
  jsonc: { icon: 'json', accent: 'json', label: 'JSON' },
  json5: { icon: 'json', accent: 'json', label: 'JSON' },
}

function formatFor(name: string): FileFormat | undefined {
  const dot = name.lastIndexOf('.')
  const extension = dot > 0 && dot < name.length - 1 ? name.slice(dot + 1).toLowerCase() : ''
  return extension ? extensionMap[extension] : undefined
}

export function fileExtension(name: string): string {
  const dot = name.lastIndexOf('.')
  if (dot <= 0 || dot === name.length - 1) {
    return ''
  }
  return name.slice(dot + 1).toUpperCase()
}

export function fileIconKey(name: string): string {
  return formatFor(name)?.icon ?? 'document'
}

export function fileAccent(name: string): FileAccent {
  return formatFor(name)?.accent ?? 'file'
}

export function fileKindLabel(name: string): string {
  const format = formatFor(name)
  if (format) {
    return format.label
  }
  const extension = fileExtension(name)
  return extension ? `${extension} 文件` : '文件'
}

export function totalFileSize(files: { size: number }[]): number {
  return files.reduce((sum, file) => sum + file.size, 0)
}

export function textByteLength(text: string): number {
  return new TextEncoder().encode(text).length
}
