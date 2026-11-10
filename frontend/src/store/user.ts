import { defineStore } from 'pinia'
import { getToken, setToken } from '@/api/request'

// S1 用户状态空壳：只管 Token 存取，真登录逻辑 S2 落
export const useUserStore = defineStore('user', {
  state: () => ({ token: getToken() }),
  actions: {
    save(t: string): void {
      this.token = t
      setToken(t)
    },
    clear(): void {
      this.token = ''
      setToken('')
    },
  },
})
