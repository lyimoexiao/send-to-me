<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { activeStageIndex, stageLayers } from '../stage'

const ready = ref(false)
const loaded = ref(new Set<string>())

onMounted(() => {
  requestAnimationFrame(() => {
    ready.value = true
  })
})

watch(
  stageLayers,
  (layers) => {
    for (const layer of layers) {
      if (!layer.src || loaded.value.has(layer.src)) {
        continue
      }
      const image = new Image()
      image.decoding = 'async'
      image.onload = () => markLoaded(layer.src)
      image.onerror = () => markLoaded(layer.src)
      image.src = layer.src
    }
  },
  { immediate: true },
)

function markLoaded(src: string) {
  if (loaded.value.has(src)) return
  const next = new Set(loaded.value)
  next.add(src)
  loaded.value = next
}
</script>

<template>
  <div class="stage-backdrop" :class="{ 'is-ready': ready }" aria-hidden="true">
    <div
      v-for="(layer, index) in stageLayers"
      :key="layer.id"
      class="stage-layer"
      :class="{ 'is-active': index === activeStageIndex }"
      :style="{
        '--stage-a': layer.palette.a,
        '--stage-b': layer.palette.b,
        '--stage-c': layer.palette.c,
      }"
    >
      <div
        class="stage-photo"
        :class="{ 'is-loaded': loaded.has(layer.src) }"
        :style="{ backgroundImage: `url(${JSON.stringify(layer.src)})` }"
      ></div>
      <div class="stage-wash"></div>
      <div class="stage-shade"></div>
    </div>

    <div class="stage-grain"></div>
    <div class="stage-vignette"></div>
  </div>
</template>

<style scoped>
.stage-backdrop {
  position: fixed;
  inset: 0;
  z-index: 0;
  overflow: hidden;
  background: #1b1420;
  pointer-events: none;
}

.stage-layer {
  position: absolute;
  inset: 0;
  opacity: 0;
  background: linear-gradient(135deg, var(--stage-a), var(--stage-b) 54%, var(--stage-c));
  transition: none;
}

.stage-backdrop.is-ready .stage-layer {
  transition: opacity 1100ms cubic-bezier(0.2, 0, 0, 1);
}

.stage-layer.is-active {
  opacity: 1;
}

.stage-photo {
  position: absolute;
  inset: -2%;
  opacity: 0;
  background-position: center;
  background-size: cover;
  filter: saturate(0.82) contrast(1.04) brightness(0.94);
  transform: scale(1.02);
  transition: opacity 680ms ease;
}

.stage-photo.is-loaded {
  opacity: 1;
}

.stage-layer.is-active .stage-photo {
  animation: stage-breathe 42s ease-in-out infinite alternate;
  will-change: transform;
}

@keyframes stage-breathe {
  from {
    transform: scale(1.02) translate3d(0, 0, 0);
  }
  to {
    transform: scale(1.075) translate3d(-0.8%, -1.1%, 0);
  }
}

.stage-wash {
  position: absolute;
  inset: 0;
  background:
    linear-gradient(132deg, var(--stage-a) 0%, transparent 46%, var(--stage-c) 100%),
    linear-gradient(0deg, rgba(14, 6, 16, 0.12), rgba(14, 6, 16, 0.12));
  mix-blend-mode: soft-light;
  opacity: 0.92;
}

.stage-shade {
  position: absolute;
  inset: 0;
  background:
    linear-gradient(103deg, rgba(12, 5, 15, 0.62) 0%, rgba(12, 5, 15, 0.16) 48%, rgba(12, 5, 15, 0.4) 100%),
    linear-gradient(180deg, rgba(12, 5, 15, 0.38) 0%, transparent 32%, transparent 62%, rgba(12, 5, 15, 0.56) 100%);
}

.stage-grain {
  position: absolute;
  inset: 0;
  opacity: 0.18;
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='180' height='180'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.86' numOctaves='3' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='180' height='180' filter='url(%23n)' opacity='0.65'/%3E%3C/svg%3E");
  mix-blend-mode: soft-light;
}

.stage-vignette {
  position: absolute;
  inset: 0;
  background: radial-gradient(
    128% 102% at 52% 42%,
    transparent 44%,
    rgba(10, 4, 12, 0.26) 100%
  );
}

@media (prefers-reduced-motion: reduce) {
  .stage-layer {
    transition-duration: 0.01ms;
  }

  .stage-layer.is-active .stage-photo {
    animation: none;
  }
}
</style>
