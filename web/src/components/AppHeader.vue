<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppIcon from './AppIcon.vue'

const route = useRoute()
const router = useRouter()
const isPickup = computed(() => route.name === 'pickup')

function goHome() {
  void router.push('/')
}
</script>

<template>
  <header class="site-header">
    <div class="header-inner">
      <RouterLink to="/" class="brand" aria-label="递我 · 返回寄件首页">
        <span class="brand-mark">
          <AppIcon name="send" :size="17" :stroke-width="1.9" />
        </span>
        <span class="brand-text">
          <strong>递我</strong>
          <em>SEND TO ME</em>
        </span>
      </RouterLink>

      <el-button v-if="isPickup" @click="goHome">
        <AppIcon name="upload" :size="15" class="button-icon" />
        寄一件
      </el-button>
    </div>
  </header>
</template>

<style scoped>
.site-header {
  position: sticky;
  top: 0;
  z-index: 20;
  padding: 12px 0;
  background: rgba(255, 255, 255, 0.86);
  border-bottom: 1px solid var(--line);
  backdrop-filter: blur(12px);
}

.header-inner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  max-width: 1080px;
  margin: 0 auto;
  padding: 0 24px;
}

.brand {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  color: var(--ink);
  text-decoration: none;
}

.brand-mark {
  display: grid;
  place-items: center;
  width: 36px;
  height: 36px;
  color: #fff;
  background: var(--accent);
  border-radius: 9px;
}

.brand-text {
  display: flex;
  flex-direction: column;
  line-height: 1;
}

.brand-text strong {
  font-family: var(--font-sans);
  font-size: 18px;
  font-weight: 700;
  letter-spacing: 0.06em;
}

.brand-text em {
  margin-top: 4px;
  color: var(--muted-light);
  font-family: var(--font-mono);
  font-size: 7px;
  font-style: normal;
  letter-spacing: 0.16em;
}

.button-icon {
  margin-right: 5px;
}

@media (max-width: 640px) {
  .site-header {
    padding: 10px 0;
  }

  .header-inner {
    padding: 0 16px;
  }

  .brand-mark {
    width: 32px;
    height: 32px;
    border-radius: 9px;
  }

  .brand-text strong {
    font-size: 17px;
  }
}
</style>
