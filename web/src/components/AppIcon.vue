<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    name: string
    size?: number | string
    strokeWidth?: number
  }>(),
  {
    size: 20,
    strokeWidth: 1.8,
  },
)

const icons: Record<string, string> = {
  send: '<path d="M21.5 2.5 10.8 13.2"/><path d="m21.5 2.5-6.7 19-3.9-8.3-8.4-3.9 19-6.8Z"/>',
  plus: '<path d="M12 5.2v13.6"/><path d="M5.2 12h13.6"/>',
  grip:
    '<circle cx="9" cy="7" r="1" fill="currentColor" stroke="none"/><circle cx="15" cy="7" r="1" fill="currentColor" stroke="none"/><circle cx="9" cy="12" r="1" fill="currentColor" stroke="none"/><circle cx="15" cy="12" r="1" fill="currentColor" stroke="none"/><circle cx="9" cy="17" r="1" fill="currentColor" stroke="none"/><circle cx="15" cy="17" r="1" fill="currentColor" stroke="none"/>',
  relay:
    '<rect x="2.6" y="8.2" width="10.2" height="7.6" rx="3.8" transform="rotate(-30 7.7 12)"/><rect x="11.2" y="8.2" width="10.2" height="7.6" rx="3.8" transform="rotate(-30 16.3 12)"/>',
  arrow:
    '<path d="M4.5 12h15"/><path d="m13.5 6 6 6-6 6"/>',
  copy: '<rect x="8" y="8" width="12" height="12" rx="2.5"/><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2"/>',
  check: '<path d="m4.5 12.5 5 5 10-11"/>',
  x: '<path d="m6 6 12 12M18 6 6 18"/>',
  upload:
    '<path d="M4 14.5v3A2.5 2.5 0 0 0 6.5 20h11a2.5 2.5 0 0 0 2.5-2.5v-3"/><path d="M12 3.5v11"/><path d="m7.5 10 4.5 4.5L16.5 10"/>',
  file: '<path d="M14 2.5H7a2 2 0 0 0-2 2v15a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V7.5z"/><path d="M14 2.5v5h5"/>',
  text: '<path d="M4 6.5h16"/><path d="M4 12h12"/><path d="M4 17.5h9"/>',
  clock: '<circle cx="12" cy="12" r="8.5"/><path d="M12 7v5.2l3.2 1.9"/>',
  download:
    '<path d="M12 3.5v11"/><path d="m7.5 10 4.5 4.5L16.5 10"/><path d="M5 20.5h14"/>',
  shield:
    '<path d="M12 2.8 5 6v6c0 4.4 2.9 7.5 7 9.2 4.1-1.7 7-4.8 7-9.2V6l-7-3.2Z"/><path d="m9 12 2.1 2.1L15.2 10"/>',
  link: '<path d="M10.2 13.3a5 5 0 0 0 7.2.5l2-2a5 5 0 0 0-7.1-7.1L11.1 5.9"/><path d="M13.8 10.7a5 5 0 0 0-7.2-.5l-2 2a5 5 0 0 0 7.1 7.1l1.2-1.2"/>',
  key: '<circle cx="8.5" cy="15.5" r="4"/><path d="m11.5 12.5 8.5-8.5"/><path d="m16 5 3 3"/><path d="m18.5 7.5 2 2"/>',
  lock: '<rect x="4.5" y="10" width="15" height="10.5" rx="2.5"/><path d="M8 10V7a4 4 0 0 1 8 0v3"/><path d="M12 14v2.5"/>',
  trash:
    '<path d="M4 7h16"/><path d="M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2"/><path d="m6.5 7 1 13h9l1-13"/><path d="M10 11v5M14 11v5"/>',
  external:
    '<path d="M14 4h6v6"/><path d="M20 4 11 13"/><path d="M20 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1h5"/>',
  refresh:
    '<path d="M20 11a8 8 0 1 0-2.2 6.3"/><path d="M20 4.5v6.8h-6.8"/>',
  alert:
    '<circle cx="12" cy="12" r="8.8"/><path d="M12 7.5v5.3"/><path d="M12 16.4h.01"/>',
  spark:
    '<path d="m12 2.8 1.8 5.1 5.1 1.8-5.1 1.8-1.8 5.1-1.8-5.1-5.1-1.8 5.1-1.8L12 2.8Z"/><path d="m19 16 .7 2 2 .7-2 .7-.7 2-.7-2-2-.7 2-.7.7-2Z"/>',
  code: '<path d="m8 9-3 3 3 3"/><path d="m16 9 3 3-3 3"/><path d="m13.5 6-3 12"/>',
  chevron: '<path d="m6.5 9.5 5.5 5.5 5.5-5.5"/>',
  info: '<circle cx="12" cy="12" r="8.8"/><path d="M12 11v5.2"/><path d="M12 7.8h.01"/>',
  image:
    '<rect x="3" y="4" width="18" height="16" rx="2.5"/><circle cx="8.5" cy="9.5" r="1.5"/><path d="m21 15-5-5L5 20"/>',
  video:
    '<rect x="3" y="5" width="13" height="14" rx="2.5"/><path d="m16 10 5-3v10l-5-3"/>',
  audio:
    '<path d="M9 18V6l10-2v12"/><circle cx="6.5" cy="18.5" r="2.5"/><circle cx="16.5" cy="16.5" r="2.5"/>',
  archive:
    '<path d="M4 8h16"/><rect x="5" y="8" width="14" height="12" rx="2"/><path d="M10 8V5h4v3"/><path d="M12 12v2"/>',
  sheet:
    '<rect x="4" y="3" width="16" height="18" rx="2"/><path d="M4 9h16M4 15h16M10 3v18"/>',
  slides:
    '<rect x="3" y="4" width="18" height="13" rx="2"/><path d="M12 17v4M8 21h8"/><path d="m9 13 2.5-3 2 2L16 8"/>',
  doc: '<path d="M14 2.5H7a2 2 0 0 0-2 2v15a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V7.5z"/><path d="M14 2.5v5h5"/><path d="M8.5 12h7M8.5 15.5h7M8.5 8.5h2"/>',
  pdf: '<path d="M14 2.5H7a2 2 0 0 0-2 2v15a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V7.5z"/><path d="M14 2.5v5h5"/><path d="M8.5 16.5V12h1.8a1.5 1.5 0 0 1 0 3H8.5"/>',
  mail: '<rect x="3" y="5" width="18" height="14" rx="2.5"/><path d="m4 7 8 6 8-6"/>',
}

const content = computed(() => icons[props.name] ?? icons.file)
</script>

<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    :stroke-width="strokeWidth"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
    v-html="content"
  />
</template>
