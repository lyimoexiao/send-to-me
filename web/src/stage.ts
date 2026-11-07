import { computed, ref } from 'vue'
import { siteConfig } from './site'
import type { Background } from './types'

export interface StageLayer {
  id: string
  src: string
  label: string
  credit: string
  source: 'BING' | 'PICSUM' | 'UPLOAD' | 'URL'
  palette: { a: string; b: string; c: string }
}

const fallbackBackgrounds: Background[] = [
  { id: 'builtin-bing', kind: 'bing', label: 'Bing 每日一图' },
  {
    id: 'builtin-fjord',
    kind: 'picsum',
    label: 'Picsum · 峡湾',
    url: 'https://picsum.photos/id/1015/1600/900',
  },
  {
    id: 'builtin-valley',
    kind: 'picsum',
    label: 'Picsum · 山谷',
    url: 'https://picsum.photos/id/1043/1600/900',
  },
]

const palettes = [
  { a: '#8f3b60', b: '#a05a7a', c: '#6f5a9e' },
  { a: '#344fa0', b: '#5b6ec7', c: '#8c9ae0' },
  { a: '#9a4535', b: '#b96a4e', c: '#7b4d67' },
  { a: '#2f6f5e', b: '#4f9a7f', c: '#7a8f5a' },
  { a: '#8a5a2b', b: '#b0803f', c: '#6d4560' },
  { a: '#3d5a73', b: '#5e8296', c: '#8a7a9e' },
]

const bingIndex = Math.floor(Math.random() * 8)

function sourceFor(background: Background): string {
  if (background.kind === 'bing') {
    return `https://bing.biturl.top/?resolution=1920&format=image&index=${bingIndex}&mkt=zh-CN`
  }
  if (background.url) {
    return background.url
  }
  if (background.kind === 'picsum') {
    return `https://picsum.photos/seed/${encodeURIComponent(background.id)}/1600/900`
  }
  return ''
}

function creditFor(background: Background): string {
  if (background.credit) {
    return background.credit
  }
  switch (background.kind) {
    case 'bing':
      return 'Bing 每日一图'
    case 'picsum':
      return 'Picsum'
    case 'upload':
      return '本地上传'
    default:
      try {
        return new URL(background.url ?? '').hostname
      } catch {
        return '远程图片'
      }
  }
}

function sourceLabel(kind: Background['kind']): StageLayer['source'] {
  switch (kind) {
    case 'bing':
      return 'BING'
    case 'picsum':
      return 'PICSUM'
    case 'upload':
      return 'UPLOAD'
    default:
      return 'URL'
  }
}

export const stageLayers = computed<StageLayer[]>(() => {
  const configured = siteConfig.value.backgrounds
  const backgrounds = configured.length ? configured : fallbackBackgrounds
  return backgrounds.map((background, index) => ({
    id: background.id,
    src: sourceFor(background),
    label: background.label || creditFor(background),
    credit: creditFor(background),
    source: sourceLabel(background.kind),
    palette: palettes[index % palettes.length],
  }))
})

const stageIndex = ref(0)

export const activeStageIndex = computed(() =>
  stageLayers.value.length ? stageIndex.value % stageLayers.value.length : 0,
)

export const stageTheme = computed<StageLayer | null>(
  () => stageLayers.value[activeStageIndex.value] ?? null,
)

export function cycleStageTheme() {
  if (stageLayers.value.length < 2) {
    return
  }
  stageIndex.value = (activeStageIndex.value + 1) % stageLayers.value.length
}

export function stageThemeLabel() {
  return stageTheme.value?.label ?? ''
}

export function stagePhoto() {
  return stageTheme.value?.src ?? ''
}
