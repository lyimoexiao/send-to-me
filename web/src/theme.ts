// Sample only images that the browser is permitted to read. Cross-origin images
// without CORS remain usable as backgrounds, with the default blue theme.
const DEFAULT_COLOR = '#1764c0'
const cache = new Map<string, string>()
let requestId = 0

function accessibleColor(r: number, g: number, b: number): string {
  const linear = (value: number) => {
    const c = value / 255
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
  }
  let factor = 1
  while (factor > 0 && 0.2126 * linear(r * factor) + 0.7152 * linear(g * factor) + 0.0722 * linear(b * factor) > 0.183) {
    factor -= 0.01
  }
  return `#${[r, g, b].map((value) => Math.round(value * factor).toString(16).padStart(2, '0')).join('')}`
}

function extractColor(image: HTMLImageElement): string {
  const canvas = document.createElement('canvas')
  canvas.width = 64
  canvas.height = 64
  const context = canvas.getContext('2d', { willReadFrequently: true })
  if (!context) return DEFAULT_COLOR
  context.drawImage(image, 0, 0, 64, 64)
  const pixels = context.getImageData(0, 0, 64, 64).data
  const buckets = new Map<string, { weight: number; r: number; g: number; b: number }>()
  for (let i = 0; i < pixels.length; i += 4) {
    const r = pixels[i], g = pixels[i + 1], b = pixels[i + 2]
    const high = Math.max(r, g, b), low = Math.min(r, g, b)
    const saturation = high === 0 ? 0 : (high - low) / high
    if (pixels[i + 3] < 128 || high < 35 || high > 245 || saturation < 0.2) continue
    const key = `${r >> 5},${g >> 5},${b >> 5}`
    const bucket = buckets.get(key) ?? { weight: 0, r: 0, g: 0, b: 0 }
    const weight = 0.5 + saturation
    bucket.weight += weight
    bucket.r += r * weight
    bucket.g += g * weight
    bucket.b += b * weight
    buckets.set(key, bucket)
  }
  let best: { weight: number; r: number; g: number; b: number } | undefined
  for (const bucket of buckets.values()) {
    if (!best || bucket.weight > best.weight) best = bucket
  }
  return best ? accessibleColor(best.r / best.weight, best.g / best.weight, best.b / best.weight) : DEFAULT_COLOR
}

export function updateThemeFromBackground(src: string) {
  const current = ++requestId
  const apply = (color: string) => {
    if (current === requestId) document.documentElement.style.setProperty('--theme-accent', color)
  }
  if (!src) {
    apply(DEFAULT_COLOR)
    return
  }
  const cached = cache.get(src)
  if (cached) {
    apply(cached)
    return
  }
  // Reset while the new background loads; don't leave the previous image's hue.
  apply(DEFAULT_COLOR)
  const image = new Image()
  image.crossOrigin = 'anonymous'
  image.onload = () => {
    let color = DEFAULT_COLOR
    try {
      color = extractColor(image)
    } catch {
      // A tainted canvas or unavailable decoder is not a reason to break the UI.
    }
    cache.set(src, color)
    apply(color)
  }
  image.onerror = () => {
    cache.set(src, DEFAULT_COLOR)
    apply(DEFAULT_COLOR)
  }
  image.src = src
}
