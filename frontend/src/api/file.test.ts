import { beforeEach, describe, expect, it, vi } from 'vitest'
import request from '@/api/request'
import { fileApi } from '@/api/file'

vi.mock('@/api/request', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}))

const mockedPost = vi.mocked(request.post)

function rawFile() {
  return {
    id: 21,
    file_name: 'a.pdf',
    parent_id: 0,
    is_folder: 0,
    file_size: 100,
    mime_type: 'application/pdf',
    category: '文档',
    created_at: '2026-11-16 00:00:00',
    updated_at: '2026-11-16 00:00:00',
  }
}

describe('S2 fileApi.instantCheck（snake→camel + 缺省兜底）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('hit 时透出 file 并转 camelCase', async () => {
    mockedPost.mockResolvedValue({
      data: { code: 0, message: 'success', data: { hit: true, file: rawFile() } },
    })
    const res = await fileApi.instantCheck({ fileName: 'a.pdf', parentId: 0, fileHash: 'a'.repeat(64) })
    expect(res.hit).toBe(true)
    expect(res.existsGlobal).toBe(false)
    expect(res.file?.fileName).toBe('a.pdf')
    expect(res.file?.fileSize).toBe(100)
    expect(mockedPost).toHaveBeenCalledWith('/files/instant-check', {
      file_name: 'a.pdf',
      parent_id: 0,
      file_hash: 'a'.repeat(64),
    })
  })

  it('exists_global 缺省时兜 false，file null 时不崩', async () => {
    mockedPost.mockResolvedValue({
      data: { code: 0, message: 'success', data: { hit: false, file: null } },
    })
    const res = await fileApi.instantCheck({ fileName: 'b.pdf', parentId: 0, fileHash: 'b'.repeat(64) })
    expect(res.hit).toBe(false)
    expect(res.existsGlobal).toBe(false)
    expect(res.file).toBeNull()
  })

  it('他人已存透出 exists_global=true', async () => {
    mockedPost.mockResolvedValue({
      data: { code: 0, message: 'success', data: { hit: false, exists_global: true, file: null } },
    })
    const res = await fileApi.instantCheck({ fileName: 'b.pdf', parentId: 0, fileHash: 'b'.repeat(64) })
    expect(res.existsGlobal).toBe(true)
    expect(res.file).toBeNull()
  })
})
