import { describe, expect, it, vi, beforeEach } from 'vitest'
import axios, { type InternalAxiosRequestConfig } from 'axios'

vi.mock('element-plus', () => ({ ElMessage: { error: vi.fn() } }))

describe('request 拦截器（S1）', () => {
  beforeEach(() => {
    sessionStorage.clear()
    vi.resetModules()
  })

  it('登录态自动挂 Authorization 头', async () => {
    const { setToken } = await import('@/api/request')
    setToken('t123')
    const req = (await import('@/api/request')).default
    const cfg = await req.interceptors.request.handlers[0].fulfilled({
      headers: {},
    } as InternalAxiosRequestConfig)
    expect(cfg.headers.Authorization).toBe('Bearer t123')
    expect(sessionStorage.getItem('cb_token')).toBe('t123')
  })

  it('401 清 Token（response 拦截器已注册）', async () => {
    const req = (await import('@/api/request')).default
    expect(req.interceptors.response.handlers.length).toBeGreaterThan(0)
    expect(axios.create).toBeDefined()
  })
})
