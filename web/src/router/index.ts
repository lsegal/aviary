import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/chat' },
    { path: '/login', component: () => import('../views/LoginView.vue') },
    { path: '/chat', component: () => import('../views/ChatView.vue'), meta: { requiresAuth: true } },
    { path: '/agents', component: () => import('../views/AgentsView.vue'), meta: { requiresAuth: true } },
    { path: '/tasks', component: () => import('../views/TasksView.vue'), meta: { requiresAuth: true } },
    { path: '/sessions', component: () => import('../views/SessionsView.vue'), meta: { requiresAuth: true } },
  ],
})

router.beforeEach((to) => {
  const auth = useAuthStore()
  if (to.meta.requiresAuth && !auth.isLoggedIn) {
    return '/login'
  }
})

export default router
