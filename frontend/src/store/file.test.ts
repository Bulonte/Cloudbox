import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { fileApi } from '@/api/file'
import { computeFileHash } from '@/utils/hash'
import type { FileItem } from '@/api/file'

vi.mock('@/api/file', () => ({
  fileApi: {
    list: vi.fn(),
    createFolder: vi.fn(),
    update: vi.fn(),
    remove: vi.fn(),
    upload: vi.fn(),
    instantCheck: vi.fn(),
  },
}))

vi.mock('@/utils/hash', () => ({
  computeFileHash: vi.fn(),
}))

const mocked = vi.mocked(fileApi)
const mockedHash = vi.mocked(computeFileHash)

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
    mockedHash.mockResolvedValue('a'.repeat(64))
    mocked.instantCheck.mockResolvedValue({ hit: false, existsGlobal: false, file: null })
  })

  it('成功后 uploading 回 false 且刷列表', async () => {
    const { useFileStore } = await import('@/store/file')
    mocked.upload.mockResolvedValue(item(9, 'a.pdf'))
    mocked.list.mockResolvedValue({ total: 1, page: 1, pageSize: 20, items: [item(9, 'a.pdf')] })
    const store = useFileStore()
    const outcome = await store.uploadOne(new File(['x'], 'a.pdf'))
    expect(outcome).toBe('uploaded')
    expect(mocked.upload).toHaveBeenCalledOnce()
    expect(store.uploading).toBe(false)
    expect(store.items.length).toBe(1)
  })

  it('小文件成功清掉旧分片残留的 resumable（防幽灵继续上传）', async () => {
    const { useFileStore } = await import('@/store/file')
    mocked.upload.mockResolvedValue(item(9, 'a.pdf'))
    mocked.list.mockResolvedValue({ total: 1, page: 1, pageSize: 20, items: [item(9, 'a.pdf')] })
    const store = useFileStore()
    // 模拟一次分片失败残留
    store.resumable = { fileName: 'big.txt', uploaded: 1, total: 3, skipped: 1 }
    const outcome = await store.uploadOne(new File(['x'], 'a.pdf'))
    expect(outcome).toBe('uploaded')
    expect(store.resumable).toBeNull()
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

describe('S2 file store 秒传预检三分支', () => {
  const HASH = 'b'.repeat(64)

  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    mockedHash.mockResolvedValue(HASH)
    mocked.list.mockResolvedValue({ total: 1, page: 1, pageSize: 20, items: [item(9, 'a.pdf')] })
  })

  it('hit 秒传：零写请求，progress=100，返回 instant', async () => {
    const { useFileStore } = await import('@/store/file')
    mocked.instantCheck.mockResolvedValue({ hit: true, existsGlobal: false, file: item(9, 'a.pdf') })
    const store = useFileStore()
    const outcome = await store.uploadOne(new File(['x'], 'a.pdf'))
    expect(outcome).toBe('instant')
    expect(mocked.instantCheck).toHaveBeenCalledWith({ fileName: 'a.pdf', parentId: 0, fileHash: HASH })
    expect(mocked.upload).not.toHaveBeenCalled()
    expect(store.progress).toBe(100)
    expect(mocked.list).toHaveBeenCalled()
    expect(store.uploading).toBe(false)
  })

  it('confirm-global 取消：零写请求，返回 cancelled', async () => {
    const { useFileStore } = await import('@/store/file')
    mocked.instantCheck.mockResolvedValue({ hit: false, existsGlobal: true, file: null })
    const store = useFileStore()
    const outcome = await store.uploadOne(new File(['x'], 'a.pdf'), async () => false)
    expect(outcome).toBe('cancelled')
    expect(mocked.upload).not.toHaveBeenCalled()
    expect(mocked.list).not.toHaveBeenCalled()
    expect(store.uploading).toBe(false)
    expect(store.phase).toBe('idle')
  })

  it('confirm-global 无回调默认取消：零写请求', async () => {
    const { useFileStore } = await import('@/store/file')
    mocked.instantCheck.mockResolvedValue({ hit: false, existsGlobal: true, file: null })
    const store = useFileStore()
    const outcome = await store.uploadOne(new File(['x'], 'a.pdf'))
    expect(outcome).toBe('cancelled')
    expect(mocked.upload).not.toHaveBeenCalled()
  })

  it('confirm-global 确认后走正常上传', async () => {
    const { useFileStore } = await import('@/store/file')
    mocked.instantCheck.mockResolvedValue({ hit: false, existsGlobal: true, file: null })
    mocked.upload.mockResolvedValue(item(9, 'a.pdf'))
    const store = useFileStore()
    const outcome = await store.uploadOne(new File(['x'], 'a.pdf'), async () => true)
    expect(outcome).toBe('uploaded')
    expect(mocked.upload).toHaveBeenCalledOnce()
  })

  it('预检失败时错误上浮且状态复位', async () => {
    const { useFileStore } = await import('@/store/file')
    mocked.instantCheck.mockRejectedValue(new Error('400'))
    const store = useFileStore()
    await expect(store.uploadOne(new File(['x'], 'a.pdf'))).rejects.toThrow('400')
    expect(mocked.upload).not.toHaveBeenCalled()
    expect(store.uploading).toBe(false)
    expect(store.phase).toBe('idle')
  })

  it('哈希失败时不调预检', async () => {
    const { useFileStore } = await import('@/store/file')
    mockedHash.mockRejectedValue(new Error('不支持安全哈希'))
    const store = useFileStore()
    await expect(store.uploadOne(new File(['x'], 'a.pdf'))).rejects.toThrow('不支持安全哈希')
    expect(mocked.instantCheck).not.toHaveBeenCalled()
    expect(store.uploading).toBe(false)
  })
})
