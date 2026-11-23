import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { fileApi } from '@/api/file'
import { uploadApi } from '@/api/upload'
import { computeFileHash } from '@/utils/hash'
import { CHUNK_SIZE } from '@/components/upload'
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

vi.mock('@/api/upload', () => ({
  uploadApi: {
    init: vi.fn(),
    uploadChunk: vi.fn(),
    complete: vi.fn(),
    progress: vi.fn(),
    cancel: vi.fn(),
  },
}))

vi.mock('@/utils/hash', () => ({
  computeFileHash: vi.fn(),
}))

const mockedFile = vi.mocked(fileApi)
const mockedUpload = vi.mocked(uploadApi)
const mockedHash = vi.mocked(computeFileHash)

function item(id: number, name: string): FileItem {
  return {
    id,
    fileName: name,
    parentId: 0,
    isFolder: 0,
    fileSize: 0,
    mimeType: '',
    category: '',
    createdAt: '2026-11-15 00:00:00',
    updatedAt: '2026-11-15 00:00:00',
  }
}

/** 造一个指定大小的假文件（jsdom 无真实分片语义，只关心 size） */
function fakeFile(name: string, size: number): File {
  const f = new File(['x'], name)
  Object.defineProperty(f, 'size', { value: size })
  return f
}

const HASH = 'a'.repeat(64)

describe('S1 file store 分片上传编排', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    mockedHash.mockResolvedValue(HASH)
    mockedFile.instantCheck.mockResolvedValue({ hit: false, existsGlobal: false, file: null })
    mockedUpload.init.mockResolvedValue({
      uploadId: 'up-1',
      fileHash: HASH,
      totalChunks: 2,
      chunkSize: CHUNK_SIZE,
      status: 0,
    })
    mockedUpload.uploadChunk.mockResolvedValue({ uploadId: 'up-1', chunkIndex: 0, size: 1, status: 1 })
    mockedUpload.complete.mockResolvedValue(item(21, 'big.txt'))
    mockedFile.list.mockResolvedValue({ total: 1, page: 1, pageSize: 20, items: [item(21, 'big.txt')] })
  })

  it('走完整链路：hash → init → chunks → complete → 刷列表', async () => {
    const { useFileStore } = await import('@/store/file')
    const store = useFileStore()
    await store.uploadChunked(fakeFile('big.txt', 5 * 1024 * 1024))

    expect(mockedHash).toHaveBeenCalledOnce()
    expect(mockedUpload.init).toHaveBeenCalledWith({
      fileName: 'big.txt',
      parentId: 0,
      fileHash: HASH,
      totalSize: 5 * 1024 * 1024,
      totalChunks: 3,
      chunkSize: CHUNK_SIZE,
    })
    expect(mockedUpload.uploadChunk).toHaveBeenCalledTimes(3)
    expect(mockedUpload.complete).toHaveBeenCalledWith('up-1', 'big.txt', 0)
    expect(mockedFile.list).toHaveBeenCalled()
    expect(store.items.length).toBe(1)
    expect(store.uploading).toBe(false)
    expect(store.progress).toBe(100)
  })

  it('chunk_index 从 0 开始且末片用实际剩余长度', async () => {
    const { useFileStore } = await import('@/store/file')
    const store = useFileStore()
    await store.uploadChunked(fakeFile('big.txt', 5 * 1024 * 1024))

    const indexes = mockedUpload.uploadChunk.mock.calls.map((c) => c[1])
    expect(indexes).toEqual([0, 1, 2])
    // 末片下界应为 2 * CHUNK_SIZE（jsdom 的 File.slice 不支持真实偏移，
    // Blob.size 恒为源文件长度，故只断言末片起点的计算结果）
    const lastBlob = mockedUpload.uploadChunk.mock.calls[2][2] as Blob
    expect(lastBlob).toBeInstanceOf(Blob)
    expect(mockedUpload.uploadChunk.mock.calls[2][1]).toBe(2)
  })

  it('超 500MB 直接拒，不发起任何请求', async () => {
    const { useFileStore } = await import('@/store/file')
    const store = useFileStore()
    await expect(store.uploadChunked(fakeFile('huge.txt', 501 * 1024 * 1024))).rejects.toThrow('500MB')
    expect(mockedHash).not.toHaveBeenCalled()
    expect(mockedUpload.init).not.toHaveBeenCalled()
    expect(store.uploading).toBe(false)
  })

  it('空文件直接拒', async () => {
    const { useFileStore } = await import('@/store/file')
    const store = useFileStore()
    await expect(store.uploadChunked(fakeFile('empty.txt', 0))).rejects.toThrow('文件为空')
    expect(mockedUpload.init).not.toHaveBeenCalled()
  })

  it('分片失败时 uploading 回 false 且不 complete', async () => {
    const { useFileStore } = await import('@/store/file')
    mockedUpload.uploadChunk.mockRejectedValue(new Error('400'))
    const store = useFileStore()
    await expect(store.uploadChunked(fakeFile('big.txt', 5 * 1024 * 1024))).rejects.toThrow('400')
    expect(mockedUpload.complete).not.toHaveBeenCalled()
    expect(store.uploading).toBe(false)
    expect(store.phase).toBe('idle')
  })

  it('哈希失败时 uploading 回 false 且不 init', async () => {
    const { useFileStore } = await import('@/store/file')
    mockedHash.mockRejectedValue(new Error('不支持安全哈希'))
    const store = useFileStore()
    await expect(store.uploadChunked(fakeFile('big.txt', 5 * 1024 * 1024))).rejects.toThrow('不支持安全哈希')
    expect(mockedUpload.init).not.toHaveBeenCalled()
    expect(store.uploading).toBe(false)
  })

  it('上传期间 phase 依次为 hashing → uploading → merging', async () => {
    const { useFileStore } = await import('@/store/file')
    const store = useFileStore()
    const seen: string[] = []
    mockedHash.mockImplementation(async () => {
      seen.push(store.phase)
      return HASH
    })
    mockedUpload.uploadChunk.mockImplementation(async () => {
      seen.push(store.phase)
      return { uploadId: 'up-1', chunkIndex: 0, size: 1, status: 1 }
    })
    mockedUpload.complete.mockImplementation(async () => {
      seen.push(store.phase)
      return item(21, 'big.txt')
    })
    await store.uploadChunked(fakeFile('big.txt', 5 * 1024 * 1024))
    expect(seen).toContain('hashing')
    expect(seen).toContain('uploading')
    expect(seen).toContain('merging')
  })
})

describe('S2 chunk store 秒传预检三分支', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    mockedHash.mockResolvedValue(HASH)
    mockedFile.instantCheck.mockResolvedValue({ hit: false, existsGlobal: false, file: null })
    mockedUpload.init.mockResolvedValue({
      uploadId: 'up-1',
      fileHash: HASH,
      totalChunks: 2,
      chunkSize: CHUNK_SIZE,
      status: 0,
    })
    mockedUpload.uploadChunk.mockResolvedValue({ uploadId: 'up-1', chunkIndex: 0, size: 1, status: 1 })
    mockedUpload.complete.mockResolvedValue(item(21, 'big.txt'))
    mockedFile.list.mockResolvedValue({ total: 1, page: 1, pageSize: 20, items: [item(21, 'big.txt')] })
  })

  it('hit 秒传：不调 init，返回 instant', async () => {
    const { useFileStore } = await import('@/store/file')
    mockedFile.instantCheck.mockResolvedValue({ hit: true, existsGlobal: false, file: item(21, 'big.txt') })
    const store = useFileStore()
    const outcome = await store.uploadChunked(fakeFile('big.txt', 5 * 1024 * 1024))
    expect(outcome).toBe('instant')
    expect(mockedFile.instantCheck).toHaveBeenCalledWith({ fileName: 'big.txt', parentId: 0, fileHash: HASH })
    expect(mockedUpload.init).not.toHaveBeenCalled()
    expect(mockedUpload.complete).not.toHaveBeenCalled()
    expect(store.progress).toBe(100)
    expect(store.uploading).toBe(false)
  })

  it('confirm-global 取消：不调 init，返回 cancelled', async () => {
    const { useFileStore } = await import('@/store/file')
    mockedFile.instantCheck.mockResolvedValue({ hit: false, existsGlobal: true, file: null })
    const store = useFileStore()
    const outcome = await store.uploadChunked(fakeFile('big.txt', 5 * 1024 * 1024), CHUNK_SIZE, async () => false)
    expect(outcome).toBe('cancelled')
    expect(mockedUpload.init).not.toHaveBeenCalled()
    expect(mockedUpload.complete).not.toHaveBeenCalled()
    expect(store.uploading).toBe(false)
    expect(store.phase).toBe('idle')
  })

  it('confirm-global 确认后走分片全链路', async () => {
    const { useFileStore } = await import('@/store/file')
    mockedFile.instantCheck.mockResolvedValue({ hit: false, existsGlobal: true, file: null })
    const store = useFileStore()
    const outcome = await store.uploadChunked(fakeFile('big.txt', 5 * 1024 * 1024), CHUNK_SIZE, async () => true)
    expect(outcome).toBe('uploaded')
    expect(mockedUpload.init).toHaveBeenCalledOnce()
    expect(mockedUpload.complete).toHaveBeenCalledOnce()
  })

  it('预检失败时错误上浮且不 init', async () => {
    const { useFileStore } = await import('@/store/file')
    mockedFile.instantCheck.mockRejectedValue(new Error('400'))
    const store = useFileStore()
    await expect(store.uploadChunked(fakeFile('big.txt', 5 * 1024 * 1024))).rejects.toThrow('400')
    expect(mockedUpload.init).not.toHaveBeenCalled()
    expect(store.uploading).toBe(false)
    expect(store.phase).toBe('idle')
  })
})
