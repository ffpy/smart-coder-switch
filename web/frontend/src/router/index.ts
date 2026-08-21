import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory('/web/'),
  routes: [
    {
      path: '/',
      redirect: '/dashboard',
    },
    {
      path: '/dashboard',
      name: 'dashboard',
      component: () => import('../views/Dashboard.vue'),
    },
    {
      path: '/config',
      name: 'config',
      component: () => import('../views/ConfigForm.vue'),
    },
    {
      path: '/stats',
      name: 'stats',
      component: () => import('../views/Stats.vue'),
    },
  ],
})

export default router
