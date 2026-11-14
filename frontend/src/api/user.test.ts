import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { validPassword, validUsername } from '@/utils/validators'
import { getRefresh, getToken, setRefresh, setToken } from '@/api/request'
import { userApi } from '@/api/user'

vi.mock('@/api/user', () => ({
  userApi: {
    register: vi.fn(),
    login: vi.fn(),
    me: vi.fn(),
    refresh: vi.fn(),
    logout: vi.fn(),
  },
}))

const mocked = vi.mocked(userApi)

describe('注册表单校验（与后端 service/user.go 同口径）', () => {
  it('用户名规则', () => {
    expect(validUsername('ab')).toBe(false)
    expect(validUsername('a_b-1')).toBe(false)
    expect(validUsername('abc_123')).toBe(true)
    expect(validUsername('a'.repeat(33))).toBe(false)
  })

  it('密码规则', () => {
    expect(validPassword('12345678')).toBe(false)
    expect(validPassword('abcdefgh')).toBe(false)
    expect(validPassword('abc12345')).toBe(true)
    expect(validPassword('ab12')).toBe(false)
  })
})

describe('用户 store 认证闭环', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    sessionStorage.clear()
    setToken('')
    setRefresh('')
    vi.clearAllMocks()
  })

  it('register → login → me 串联写双 token', async () => {
    const { useUserStore } = await import('@/store/user')
    mocked.register.mockResolvedValue({
      id: 1,
      username: 'alice',
      email: 'a@b.c',
      role: 'user',
      quotaBytes: 1,
      usedBytes: 0,
    })
    mocked.login.mockResolvedValue({
      accessToken: 'a1',
      refreshToken: 'r1',
      tokenType: 'Bearer',
      expiresIn: 7200,
    })
    mocked.me.mockResolvedValue({
      id: 1,
      username: 'alice',
      email: 'a@b.c',
      role: 'user',
      quotaBytes: 1,
      usedBytes: 0,
    })
    const store = useUserStore()
    await store.register('alice', 'a@b.c', 'abc12345')
    expect(mocked.register).toHaveBeenCalledOnce()
    expect(mocked.login).toHaveBeenCalledOnce()
    expect(store.isLoggedIn).toBe(true)
    expect(getToken()).toBe('a1')
    expect(getRefresh()).toBe('r1')
    expect(store.user?.username).toBe('alice')
  })

  it('logout 后端 500 也清本地', async () => {
    const { useUserStore } = await import('@/store/user')
    setToken('a1')
    setRefresh('r1')
    mocked.logout.mockRejectedValue(new Error('500'))
    const store = useUserStore()
    await store.logout()
    expect(getToken()).toBe('')
    expect(getRefresh()).toBe('')
    expect(store.isLoggedIn).toBe(false)
  })
})
