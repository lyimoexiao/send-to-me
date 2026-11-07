import { createRouter, createWebHistory } from 'vue-router'
import HomeView from './views/HomeView.vue'
import PickupView from './views/PickupView.vue'
import AdminView from './views/AdminView.vue'
import NotFoundView from './views/NotFoundView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      name: 'home',
      component: HomeView,
      meta: { title: '递我 · Send to Me' },
    },
    {
      path: '/p/:code',
      name: 'pickup',
      component: PickupView,
      meta: { title: '取件 · 递我' },
    },
    {
      path: '/admin',
      name: 'admin',
      component: AdminView,
      meta: { title: '管理后台 · 递我' },
    },
    {
      path: '/:pathMatch(.*)*',
      name: 'not-found',
      component: NotFoundView,
      meta: { title: '页面不存在 · 递我' },
    },
  ],
  scrollBehavior(to, _from, savedPosition) {
    if (savedPosition) {
      return savedPosition
    }
    if (to.hash) {
      return { el: to.hash, top: 96, behavior: 'smooth' }
    }
    return { top: 0 }
  },
})

router.afterEach((to) => {
  const title = typeof to.meta.title === 'string' ? to.meta.title : ''
  document.title = title || '递我 · Send to Me'
})

export default router
