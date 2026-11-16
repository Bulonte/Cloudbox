import { beforeEach, describe, expect, it, vi } from 'vitest'
import request from '@/api/request'
import { shareApi, toPublicLink } from '@/api/share'

vi.mock('@/api/request', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
    delete: vi.fn(),
  },
}))

const mockedGet = vi.mocked(request.get)
const mockedPost = vi.mocked(request.post)
const mockedDelete = vi.mocked(request.delete)

function rawItem() {
  return {
    id: 7,
    share_code: 'Abc123XyZ456opQr',
    share_url: '/s/Abc123XyZ456opQr',
    file_id: 21,
    file_name: 'a.pdf',
    expire_at: '2026-11-23 12:00:00',
    max_downloads: 3,
    download_count: 1,
    created_at: '2026-11-16 00:00:00',
  }
}

describe('S3 shareApi（snake→camel + 缺省兜底）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('list 转 camelCase，items null 兜空数组', async () => {
    mockedGet.mockResolvedValue({
      data: { code: 0, message: 'success', data: { total: 0, page: 1, page_size: 20, items: null } },
    })
    const res = await shareApi.list(1, 20)
    expect(res.total).toBe(0)
    expect(res.items).toEqual([])
    expect(mockedGet).toHaveBeenCalledWith('/shares', { params: { page: 1, page_size: 20 } })
  })

  it('list 透出 file_name / expire_at / max_downloads', async () => {
    mockedGet.mockResolvedValue({
      data: { code: 0, message: 'success', data: { total: 1, page: 1, page_size: 20, items: [rawItem()] } },
    })
    const res = await shareApi.list(1, 20)
    expect(res.items[0]?.fileName).toBe('a.pdf')
    expect(res.items[0]?.shareCode).toBe('Abc123XyZ456opQr')
    expect(res.items[0]?.maxDownloads).toBe(3)
  })

  it('create 只透白名单字段，不回 password 明文', async () => {
    mockedPost.mockResolvedValue({
      data: {
        code: 0,
        message: 'success',
        data: { id: 7, share_code: 'Abc123XyZ456opQr', share_url: '/s/Abc123XyZ456opQr', file_id: 21 },
      },
    })
    const res = await shareApi.create({ fileId: 21, password: '', expireAt: '', maxDownloads: undefined })
    expect(res.shareCode).toBe('Abc123XyZ456opQr')
    expect(mockedPost).toHaveBeenCalledWith('/shares', { file_id: 21 })
  })

  it('cancel 按 id 拼路径', async () => {
    mockedDelete.mockResolvedValue({ data: { code: 0, message: 'success', data: null } })
    await shareApi.cancel(7)
    expect(mockedDelete).toHaveBeenCalledWith('/shares/7')
  })

  it('toPublicLink 拼 hash 路由可复制链接', () => {
    const link = toPublicLink('/s/Abc123XyZ456opQr')
    expect(link).toContain('/#/s/Abc123XyZ456opQr')
  })
})
