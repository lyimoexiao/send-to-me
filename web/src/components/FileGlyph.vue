<script setup lang="ts">
import { computed } from 'vue'
import FileTypeIcon from './FileTypeIcon.vue'
import { fileAccent } from '../format'

const props = withDefaults(
  defineProps<{
    name: string
    compact?: boolean
  }>(),
  {
    compact: false,
  },
)

const accent = computed(() => fileAccent(props.name))
</script>

<template>
  <span
    class="file-glyph"
    :class="[`accent-${accent}`, { 'is-compact': compact }]"
    aria-hidden="true"
  >
    <FileTypeIcon :name="name" :size="compact ? 22 : 26" />
  </span>
</template>

<style scoped>
/* Colored product glyph inside a softly tinted tile; the tint follows the
   format family, so PDF, Word, Excel and friends read apart at a glance. */
.file-glyph {
  --glyph: #8a8175;
  display: grid;
  flex: 0 0 auto;
  width: 36px;
  height: 40px;
  place-items: center;
  color: var(--glyph);
  background: color-mix(in srgb, var(--glyph) 13%, var(--surface-strong));
  border: 1px solid color-mix(in srgb, var(--glyph) 22%, var(--line-tile));
  border-radius: 9px;
  transition:
    background-color 180ms ease,
    border-color 180ms ease;
}

.file-glyph.is-compact {
  width: 30px;
  height: 34px;
  border-radius: 8px;
}

.accent-pdf {
  --glyph: #d23b33;
}

.accent-doc {
  --glyph: #2f5fa8;
}

.accent-sheet {
  --glyph: #1f7a4d;
}

.accent-slides {
  --glyph: #c2542a;
}

.accent-text {
  --glyph: #5c6673;
}

.accent-json {
  --glyph: #a8760a;
}

.accent-html {
  --glyph: #d0491f;
}

.accent-xml {
  --glyph: #7b52c9;
}

.accent-archive {
  --glyph: #a9791c;
}

.accent-image {
  --glyph: #0e8892;
}

.accent-video {
  --glyph: #9a3f86;
}

.accent-audio {
  --glyph: #b8437a;
}

.accent-code {
  --glyph: #4a3fd8;
}

.accent-file {
  --glyph: #8a8175;
}

@media (prefers-color-scheme: dark) {
  .file-glyph {
    color: color-mix(in srgb, var(--glyph) 78%, #ffffff);
  }
}
</style>
