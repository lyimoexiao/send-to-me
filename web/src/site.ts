import { shallowRef } from 'vue'
import { getPublicConfig } from './api'
import type { PublicConfig } from './types'

// Mirrors the server defaults so the first paint never waits on the network.
export const defaultSiteConfig: PublicConfig = {
  maxUploadBytes: 2 * 1024 ** 3,
  maxFiles: 20,
  maxTextBytes: 64 * 1024,
  defaultExpirySeconds: 7 * 24 * 60 * 60,
  maxExpirySeconds: 365 * 24 * 60 * 60,
  defaultMaxDownloads: 5,
  backgrounds: [],
}

export const siteConfig = shallowRef<PublicConfig>(defaultSiteConfig)

let pending: Promise<PublicConfig> | null = null

export function ensureSiteConfig(force = false): Promise<PublicConfig> {
  if (force) {
    pending = null
  }
  if (!pending) {
    pending = getPublicConfig()
      .then((config) => {
        siteConfig.value = {
          ...defaultSiteConfig,
          ...config,
          backgrounds: config.backgrounds ?? [],
        }
        return siteConfig.value
      })
      .catch(() => siteConfig.value)
  }
  return pending
}
