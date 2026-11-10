import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { getRefresh, getToken, setRefresh, setToken } from '@/api/request'
import { userApi, type UserInfo } from '@/api/user'

// S2 用户状态：只存可信源（token + refresh + user），isLoggedIn 用 computed 派生
export const useUserStore = defineStore('user', () => {
  const token = ref<string>(getToken())
  const refreshToken = ref<string>(getRefresh())
  const user = ref<UserInfo | null>(null)
  const isLoggedIn = computed(() => token.value !== '')

  function persist(a: string, r: string): void {
    token.value = a
    refreshToken.value = r
    setToken(a)
    setRefresh(r)
  }

  async function login(login: string, password: string): Promise<void> {
    const res = await userApi.login({ login, password })
    persist(res.accessToken, res.refreshToken)
    await fetchMe()
  }

  async function register(username: string, email: string, password: string): Promise<void> {
    await userApi.register({ username, email, password })
    // 注册成功后直接登录，减少一次跳转
    await login(username, password)
  }

  async function fetchMe(): Promise<void> {
    user.value = await userApi.me()
  }

  async function logout(): Promise<void> {
    const r = refreshToken.value
    // 先清本地，再调后端：后端 500 也不回滚，保证退出必成功
    persist('', '')
    user.value = null
    if (r) {
      await userApi.logout(r).catch(() => undefined)
    }
  }

  return { token, refreshToken, user, isLoggedIn, login, register, fetchMe, logout }
})
