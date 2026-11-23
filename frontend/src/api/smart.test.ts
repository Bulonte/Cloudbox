import { beforeEach, describe, expect, it, vi } from 'vitest'
import request from '@/api/request'
import { smartApi } from '@/api/smart'

vi.mock('@/api/request', () => ({
  default: {
    get: vi.fn(),
  },
}))

const mockedGet = vi.mocked(request.get)

function rawStats() {
  return {
    storage: {
      used_bytes: 100,
      quota_bytes: 1000,
      file_count: 2,
      by_type: [
        { type: 'image', bytes: 60, count: 1 },
        { type: 'document', bytes: 40, count: 1 },
      ],
    },
    timeline: [{ date: '2026-11-19', count: 2, bytes: 100 }],
    category: [{ category: '图片', count: 2 }],
    duplicates: {
      exact_groups: 0,
      exact_files: 0,
      exact_saved_bytes: 0,
      similar_groups: 1,
      similar_files: 2,
      items: [
        {
          kind: 'similar',
          file_ids: [1, 2],
          file_names: ['a.txt', 'b.txt'],
          count: 2,
          saved_bytes: 0,
        },
      ],
    },
  }
}

describe('S5 smartApi（snake→camel + 缺省兜底）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('stats 转 camelCase 并透 4 块', async () => {
    mockedGet.mockResolvedValue({
      data: { code: 0, message: 'success', data: rawStats() },
    })
    const res = await smartApi.stats(7)
    expect(mockedGet).toHaveBeenCalledWith('/smart/stats', { params: { days: 7 } })
    expect(res.storage.usedBytes).toBe(100)
    expect(res.storage.byType[0]?.type).toBe('image')
    expect(res.timeline[0]?.date).toBe('2026-11-19')
    expect(res.duplicates.similarGroups).toBe(1)
    expect(res.duplicates.items[0]?.kind).toBe('similar')
  })

  it('null 数组兜空数组不崩', async () => {
    const raw = rawStats()
    raw.storage.by_type = null
    raw.timeline = null
    raw.category = null
    raw.duplicates.items = null
    mockedGet.mockResolvedValue({
      data: { code: 0, message: 'success', data: raw },
    })
    const res = await smartApi.stats()
    expect(res.storage.byType).toEqual([])
    expect(res.timeline).toEqual([])
    expect(res.category).toEqual([])
    expect(res.duplicates.items).toEqual([])
  })

  it('未知 kind 归 exact，null 明细兜空', async () => {
    const raw = rawStats()
    raw.duplicates.items = [
      { kind: 'weird', file_ids: null, file_names: null, count: 2, saved_bytes: 10 },
    ]
    mockedGet.mockResolvedValue({
      data: { code: 0, message: 'success', data: raw },
    })
    const res = await smartApi.stats()
    expect(res.duplicates.items[0]?.kind).toBe('exact')
    expect(res.duplicates.items[0]?.fileIds).toEqual([])
    expect(res.duplicates.items[0]?.fileNames).toEqual([])
  })
})
