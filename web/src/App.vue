<script setup lang="ts">
import { onBeforeUnmount, onMounted, watch } from 'vue'
import StageBackdrop from './components/StageBackdrop.vue'
import { ensureSiteConfig } from './site'
import { stageLayers, stageTheme } from './stage'
import { updateThemeFromBackground } from './theme'

watch(() => stageTheme.value?.src ?? '', updateThemeFromBackground, { immediate: true })

function blocksFileDrop(event: DragEvent) {
  if (Array.from(event.dataTransfer?.types ?? []).includes('Files')) {
    event.preventDefault()
  }
}

onMounted(async () => {
  // A file dropped outside the send form must never navigate the page to it.
  // The send view adds its own window handlers to accept drops; the pickup
  // view intentionally ignores them.
  window.addEventListener('dragover', blocksFileDrop)
  window.addEventListener('drop', blocksFileDrop)

  await ensureSiteConfig()
  for (const layer of stageLayers.value) {
    if (!layer.src) {
      continue
    }
    const preload = new Image()
    preload.src = layer.src
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('dragover', blocksFileDrop)
  window.removeEventListener('drop', blocksFileDrop)
})
</script>

<template>
  <div class="app-stage">
    <StageBackdrop />

    <RouterView v-slot="{ Component }">
      <Transition name="page" mode="out-in">
        <component :is="Component" :key="$route.path" />
      </Transition>
    </RouterView>
  </div>
</template>

<style scoped>
.app-stage {
  position: relative;
  min-height: 100vh;
  overflow-x: clip;
}

.page-enter-active,
.page-leave-active {
  transition: opacity 180ms ease;
}

.page-enter-from,
.page-leave-to {
  opacity: 0;
}
</style>
