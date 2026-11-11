import { beforeEach, describe, expect, it } from 'vitest'
import { setToken } from '@/api/request'
import { NAV_ITEMS, resolveActive } from '@/components/nav'
import { WHITE_LIST, resolveRedirect } from '@/router'

describe('S3 主框架导航（nav.ts）', () => {
  it('NAV_ITEMS 首项为文件页，W8 两项 disabled 占位', () => {
    expect(NAV_ITEMS[0]?.index).toBe('/files')
    expect(NAV_ITEMS.filter((i) => i.disabled).length).toBe(2)
  })

  it('resolveActive 由 path 推导高亮，空路径兜底 /files', () => {
    expect(resolveActive('/files/a')).toBe('/files')
    expect(resolveActive('/shares')).toBe('/shares')
    expect(resolveActive('/')).toBe('/files')
  })
})

describe('S3 路由守卫（resolveRedirect）', () => {
  beforeEach(() => {
    sessionStorage.clear()
    setToken('')
  })

  it('白名单保持可达（登录/注册/404 定义完整）', () => {
    expect(WHITE_LIST).toEqual(['/login', '/register'])
  })

  it('未登录访业务页一律拦到 /login', () => {
    expect(resolveRedirect('/files')).toBe('/login')
    expect(resolveRedirect('/')).toBe('/login')
  })

  it('未登录访白名单放行', () => {
    expect(resolveRedirect('/login')).toBe(true)
    expect(resolveRedirect('/register')).toBe(true)
  })

  it('已登录回看 /login 踢回 /files', () => {
    setToken('t123')
    expect(resolveRedirect('/login')).toBe('/files')
    expect(resolveRedirect('/files')).toBe(true)
  })
})
