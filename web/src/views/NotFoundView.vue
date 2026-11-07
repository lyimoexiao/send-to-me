<script setup lang="ts">
import { useRouter } from 'vue-router'
import AppIcon from '../components/AppIcon.vue'
import PickupCodeForm from '../components/PickupCodeForm.vue'
import StageFrame from '../components/StageFrame.vue'

const router = useRouter()

function goPickup(code: string) {
  void router.push(`/p/${code}`)
}
</script>

<template>
  <StageFrame rail="pickup" footer="取件码为 6 位字母数字组合" @home="router.push('/')">
    <div class="not-found-page">
      <section class="ct-panel not-found-card">
        <header class="card-head">
          <RouterLink to="/" class="card-brand" aria-label="递我 · 返回寄件">
            <span class="brand-mark">
              <AppIcon name="relay" :size="18" :stroke-width="1.55" />
            </span>
            <span class="brand-words">
              <strong>递我</strong>
              <em>SEND TO ME</em>
            </span>
          </RouterLink>
          <span class="status-pill">404</span>
        </header>

        <p class="panel-eyebrow">PAGE NOT FOUND</p>
        <h1 class="card-title">页面不存在。</h1>
        <p class="card-desc">链接可能写错了，或者页面已经被移动。</p>

        <div class="try-another">
          <span>手里有取件码？</span>
          <PickupCodeForm compact button-label="去取件" @submit="goPickup" />
        </div>

        <button type="button" class="card-primary" @click="router.push('/')">
          返回寄件页
        </button>
      </section>
    </div>
  </StageFrame>
</template>

<style scoped>
.not-found-page {
  min-height: 100vh;
}

.not-found-card {
  position: fixed;
  top: 24px;
  left: 24px;
  z-index: 10;
  width: 360px;
  max-height: calc(100vh - 48px);
  padding: 20px 20px 18px;
  overflow-y: auto;
  overscroll-behavior: contain;
  color: var(--ink);
  border: 1px solid var(--glass-border);
  border-radius: 20px;
  box-shadow:
    0 24px 64px -30px rgba(12, 4, 16, 0.6),
    0 3px 12px rgba(12, 4, 16, 0.12);
}

.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  margin-bottom: 16px;
}

.card-brand {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 9px;
  text-decoration: none;
}

.brand-mark {
  display: grid;
  width: 32px;
  height: 32px;
  flex: 0 0 auto;
  place-items: center;
  color: #fff;
  background: var(--brand);
  border-radius: 50%;
}

.brand-words strong {
  display: block;
  color: var(--ink);
  font-size: 16px;
  font-weight: 700;
  letter-spacing: 0.02em;
  line-height: 1.1;
}

.brand-words em {
  display: block;
  margin-top: 2px;
  color: var(--muted-light);
  font-family: var(--font-mono);
  font-size: 6.5px;
  font-style: normal;
  font-weight: 600;
  letter-spacing: 0.17em;
  line-height: 1;
}

.status-pill {
  display: inline-flex;
  height: 26px;
  align-items: center;
  padding: 0 10px;
  color: var(--muted-light);
  background: var(--surface-strong);
  border-radius: 999px;
  font-family: var(--font-mono);
  font-size: 9.5px;
  font-weight: 600;
  letter-spacing: 0.08em;
}

.card-title {
  margin-top: 2px;
  color: var(--ink);
  font-size: 21px;
  font-weight: 600;
  letter-spacing: -0.01em;
  line-height: 1.3;
}

.card-desc {
  margin-top: 6px;
  color: var(--muted);
  font-size: 12px;
  line-height: 1.65;
}

.try-another {
  margin-top: 16px;
}

.try-another > span {
  display: block;
  margin-bottom: 7px;
  color: var(--muted-light);
  font-size: 10px;
}

.card-primary {
  display: flex;
  width: 100%;
  height: 44px;
  align-items: center;
  justify-content: center;
  margin-top: 16px;
  color: #fff;
  background: var(--brand);
  border-radius: 999px;
  font-size: 12.5px;
  font-weight: 600;
  transition:
    background-color 150ms ease,
    scale 150ms ease;
}

.card-primary:hover {
  background: var(--brand-deep);
}

.card-primary:active {
  scale: 0.97;
}

@media (max-width: 860px) {
  .not-found-card {
    top: auto;
    right: 12px;
    bottom: calc(12px + env(safe-area-inset-bottom, 0px));
    left: 12px;
    width: auto;
    max-height: calc(100dvh - 24px);
    padding: 18px;
    border-radius: 18px;
  }
}

@media (max-width: 480px) {
  .not-found-card {
    padding: 16px;
    border-radius: 16px;
  }
}
</style>
