import { createRouter, createWebHashHistory } from 'vue-router'
import { getToken } from '@/api/request'

// S3 主框架路由：业务页挂 Layout（aside + header + main），登录注册保持裸页
const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/login', component: () => import('@/views/Login.vue') },
    { path: '/register', component: () => import('@/views/Register.vue') },
    {
      path: '/',
      component: () => import('@/views/Layout.vue'),
      children: [
        { path: '', redirect: '/files' },
        // S4 真列表落地：FileExplorer 容器 + FileList 纯展示
        { path: 'files', component: () => import('@/views/FileExplorer.vue') },
        // S3 分享管理：需登录的本人分享列表
        { path: 'shares', component: () => import('@/views/ShareManager.vue') },
      ],
    },
    // S3 公开分享：免登录，密码在页内输入
    { path: '/s/:code', component: () => import('@/views/SharePublic.vue') },
    { path: '/:pathMatch(.*)*', component: () => import('@/views/NotFound.vue') },
  ],
})

const WHITE_LIST = ['/login', '/register']

/** 路由守卫决策（纯函数可单测）：未登录拦到 /login，已登录回看 /login 踢回 /files；/s/:code 公开分享免登录 */
export function resolveRedirect(toPath: string): string | true {
  if (toPath === '/s' || toPath.startsWith('/s/')) {
    return true
  }
  const t = getToken()
  if (!WHITE_LIST.includes(toPath) && !t) {
    return '/login'
  }
  if (WHITE_LIST.includes(toPath) && t && toPath !== '/register') {
    return '/files'
  }
  return true
}

router.beforeEach((to) => resolveRedirect(to.path))

export { WHITE_LIST }
export default router
