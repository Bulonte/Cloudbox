import { createRouter, createWebHashHistory } from 'vue-router'
import { getToken } from '@/api/request'

// S1 空路由壳：页面真实现在 S2/S3 落，本切片只保证守卫 + 构建通过
const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/login', component: () => import('@/views/Login.vue') },
    { path: '/register', component: () => import('@/views/Register.vue') },
    { path: '/', redirect: '/files' },
    { path: '/files', component: () => import('@/views/Placeholder.vue') },
    { path: '/:pathMatch(.*)*', component: () => import('@/views/NotFound.vue') },
  ],
})

const WHITE_LIST = ['/login', '/register']

// 未登录访问业务页一律拦到 /login
router.beforeEach((to) => {
  if (!WHITE_LIST.includes(to.path) && !getToken()) {
    return '/login'
  }
  return true
})

export { WHITE_LIST }
export default router
