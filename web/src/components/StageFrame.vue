<script setup lang="ts">
import AppIcon from './AppIcon.vue'
import { cycleStageTheme, stageLayers, stageThemeLabel } from '../stage'

withDefaults(
  defineProps<{
    rail?: 'send' | 'pickup'
    footer?: string
  }>(),
  {
    rail: 'send',
    footer: '打开取件页不计数，每台设备首次下载或查看计 1 次',
  },
)

const emit = defineEmits<{
  files: []
  pickup: []
  home: []
}>()
</script>

<template>
  <div class="stage-page">
    <header class="stage-nav">
      <nav class="nav-pill" aria-label="快捷操作">
        <template v-if="rail === 'send'">
          <button type="button" @click="emit('files')">寄件</button>
          <i aria-hidden="true"></i>
          <button type="button" @click="emit('pickup')">取件</button>
        </template>
        <RouterLink v-else to="/" class="nav-link">寄一件</RouterLink>
      </nav>

      <RouterLink to="/" class="nav-avatar" aria-label="递我 · 返回寄件" title="递我">
        <AppIcon name="relay" :size="19" :stroke-width="1.55" />
      </RouterLink>
    </header>

    <div class="stage-display" aria-hidden="true">
      <span>SEND</span>
      <span>TO ME</span>
    </div>

    <slot />

    <footer class="stage-foot">
      <span>{{ footer }}</span>
    </footer>

    <button
      v-if="stageLayers.length > 1"
      type="button"
      class="bg-switch"
      :title="stageThemeLabel() ? `更换背景 · 当前 ${stageThemeLabel()}` : '更换背景'"
      @click="cycleStageTheme"
    >
      <AppIcon name="refresh" :size="13" :stroke-width="2" />
      更换背景
    </button>
  </div>
</template>

<style scoped>
.stage-page {
  position: relative;
  z-index: 1;
  min-height: 100vh;
  color: #fff;
}

.stage-nav {
  position: fixed;
  top: 24px;
  right: 26px;
  z-index: 20;
  display: flex;
  align-items: center;
  gap: 8px;
}

.nav-pill {
  display: flex;
  height: 42px;
  align-items: center;
  padding: 4px;
  background: var(--glass-strong);
  border: 1px solid var(--glass-border);
  border-radius: 999px;
  box-shadow: 0 16px 38px -22px rgba(10, 4, 12, 0.6);
  backdrop-filter: blur(16px) saturate(1.2);
  -webkit-backdrop-filter: blur(16px) saturate(1.2);
}

.nav-pill button,
.nav-link {
  display: inline-flex;
  height: 34px;
  align-items: center;
  padding: 0 16px;
  color: var(--nav-ink);
  border-radius: 999px;
  font-size: 12px;
  font-weight: 500;
  letter-spacing: 0.02em;
  line-height: 1;
  text-decoration: none;
  transition:
    background-color 150ms ease,
    color 150ms ease,
    scale 150ms ease;
}

.nav-pill i {
  width: 1px;
  height: 12px;
  background: var(--rule-strong);
}

@media (hover: hover) {
  .nav-pill button:hover,
  .nav-link:hover {
    color: var(--brand-text);
    background: var(--brand-wash);
  }
}

.nav-pill button:active,
.nav-link:active {
  scale: 0.96;
}

.nav-avatar {
  display: grid;
  width: 42px;
  height: 42px;
  place-items: center;
  color: #fff;
  background: var(--brand);
  border-radius: 50%;
  box-shadow:
    0 16px 34px -20px rgba(10, 4, 12, 0.62),
    0 0 0 4px rgba(255, 255, 255, 0.14);
  transition:
    background-color 150ms ease,
    scale 150ms ease;
}

@media (hover: hover) {
  .nav-avatar:hover {
    background: var(--brand-deep);
  }
}

.nav-avatar:active {
  scale: 0.96;
}

.stage-display {
  position: fixed;
  top: 50%;
  right: 18px;
  z-index: 0;
  color: rgba(255, 255, 255, 0.16);
  font-family: 'Times New Roman', 'Songti SC', 'Iowan Old Style', serif;
  font-size: clamp(84px, 11.5vw, 196px);
  font-weight: 400;
  letter-spacing: -0.045em;
  line-height: 0.78;
  text-align: right;
  white-space: nowrap;
  transform: translateY(-50%);
  pointer-events: none;
  user-select: none;
}

.stage-display span {
  display: block;
}

.stage-foot {
  position: fixed;
  bottom: 20px;
  left: 26px;
  z-index: 8;
  max-width: min(62vw, 680px);
  color: rgba(255, 255, 255, 0.55);
  font-size: 9.5px;
  letter-spacing: 0.03em;
  line-height: 1.45;
  pointer-events: none;
}

.bg-switch {
  position: fixed;
  right: 26px;
  bottom: 20px;
  z-index: 20;
  display: inline-flex;
  height: 38px;
  align-items: center;
  gap: 7px;
  padding: 0 16px;
  color: var(--nav-ink);
  background: var(--glass-strong);
  border: 1px solid var(--glass-border);
  border-radius: 999px;
  box-shadow: 0 16px 38px -22px rgba(10, 4, 12, 0.62);
  backdrop-filter: blur(16px) saturate(1.2);
  -webkit-backdrop-filter: blur(16px) saturate(1.2);
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: 0.02em;
  transition:
    background-color 150ms ease,
    translate 150ms ease,
    scale 150ms ease;
}

.bg-switch svg {
  color: var(--brand-text);
  transition: rotate 480ms cubic-bezier(0.2, 0, 0, 1);
}

@media (hover: hover) {
  .bg-switch:hover {
    background: var(--glass-strong-hover);
    translate: 0 -1px;
  }

  .bg-switch:hover svg {
    rotate: 180deg;
  }
}

.bg-switch:active {
  scale: 0.96;
}

@media (max-width: 860px) {
  .stage-display {
    display: none;
  }
}

@media (max-width: 860px) {
  .bg-switch {
    top: 14px;
    right: auto;
    bottom: auto;
    left: 14px;
    height: 36px;
    padding: 0 14px;
    font-size: 11px;
  }
}

@media (max-width: 760px) {
  .stage-nav {
    display: none;
  }

  .stage-foot {
    display: none;
  }
}

@media (prefers-reduced-motion: reduce) {
  .bg-switch svg {
    transition: none;
  }
}
</style>
