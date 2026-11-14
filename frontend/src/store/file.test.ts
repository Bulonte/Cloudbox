import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { fileApi } from '@/api/file'
import type { FileItem } from '@/api/file'

vi.mock('@/api/file', () => ({
  fileApi: {
    list: vi.fn(),
    createFolder: vi.fn(),
    update: vi.fn(),
    remove: vi.fn(),
    upload: vi.fn(),
  },
}))

const mocked = vi.mocked(fileApi)

function item(id: number, name: string): FileItem {
  return {
    id,
    fileName: name,
    parentId: 0,
    isFolder: 1,
    fileSize: 0,
    mimeType: '',
    category: '',
    createdAt: '2026-11-11 00:00:00',
    updatedAt: '2026-11-11 00:00:00',
  }
}

describe('S4 file store 面包屑 + 越界兜底', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('enterFolder 推栈并重置 page=1', async () => {
    const { useFileStore } = await import('@/store/file')
    mocked.list.mockResolvedValue({ total: 0, page: 1, pageSize: 20, items: [] })
    const store = useFileStore()
    await store.enterFolder(item(7, 'docs'))
    expect(store.parentId).toBe(7)
    expect(store.page).toBe(1)
    expect(store.crumbs.map((c) => c.name)).toEqual(['根目录', 'docs'])
  })

  it('goCrumb 截断到目标', async () => {
    const { useFileStore } = await import('@/store/file')
    mocked.list.mockResolvedValue({ total: 0, page: 1, pageSize: 20, items: [] })
    const store = useFileStore()
    await store.enterFolder(item(7, 'docs'))
    await store.enterFolder({ ...item(8, 'sub'), parentId: 7 })
    await store.goCrumb(0)
    expect(store.parentId).toBe(0)
    expect(store.crumbs).toEqual([{ id: 0, name: '根目录' }])
  })

  it('空目录 items 置 []，删空页回退重拉', async () => {
    const { useFileStore } = await import('@/store/file')
    mocked.list
      .mockResolvedValueOnce({ total: 1, page: 2, pageSize: 20, items: [] })
      .mockResolvedValueOnce({ total: 1, page: 1, pageSize: 20, items: [item(1, 'a')] })
    const store = useFileStore()
    store.page = 2
    await store.fetchList()
    expect(store.page).toBe(1)
    expect(store.items.length).toBe(1)
  })
})

describe('S5 file store 上传进度 + 刷列表', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('成功后 uploading 回 false 且刷列表', async () => {
    const { useFileStore } = await import('@/store/file')
    mocked.upload.mockResolvedValue(item(9, 'a.pdf'))
    mocked.list.mockResolvedValue({ total: 1, page: 1, pageSize: 20, items: [item(9, 'a.pdf')] })
    const store = useFileStore()
    await store.uploadOne(new File(['x'], 'a.pdf'))
    expect(mocked.upload).toHaveBeenCalledOnce()
    expect(store.uploading).toBe(false)
    expect(store.items.length).toBe(1)
  })

  it('失败时 uploading 照样回 false 且 items 不变', async () => {
    const { useFileStore } = await import('@/store/file')
    mocked.upload.mockRejectedValue(new Error('400'))
    mocked.list.mockResolvedValue({ total: 0, page: 1, pageSize: 20, items: [] })
    const store = useFileStore()
    await expect(store.uploadOne(new File(['x'], 'a.pdf'))).rejects.toThrow('400')
    expect(store.uploading).toBe(false)
    expect(store.items).toEqual([])
  })
})
