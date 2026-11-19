import request from './request'

// S5 仪表盘聚合：GET /api/v1/smart/stats?days=14
// 后端 DTO 对齐 backend/internal/api/dto/smart_stats.go，snake→camel 一层转换。

export interface StorageTypeItem {
  type: string
  bytes: number
  count: number
}

export interface StorageBlock {
  usedBytes: number
  quotaBytes: number
  fileCount: number
  byType: StorageTypeItem[]
}

export interface TimelinePoint {
  date: string
  count: number
  bytes: number
}

export interface CategoryItem {
  category: string
  count: number
}

export interface DuplicateItem {
  kind: 'exact' | 'similar'
  fileIds: number[]
  fileNames: string[]
  count: number
  savedBytes: number
}

export interface DuplicatesBlock {
  exactGroups: number
  exactFiles: number
  exactSavedBytes: number
  similarGroups: number
  similarFiles: number
  items: DuplicateItem[]
}

export interface SmartStats {
  storage: StorageBlock
  timeline: TimelinePoint[]
  category: CategoryItem[]
  duplicates: DuplicatesBlock
}

interface Biz<T> {
  code: number
  message: string
  data: T
}

interface RawStats {
  storage: {
    used_bytes: number
    quota_bytes: number
    file_count: number
    by_type: Array<{ type: string; bytes: number; count: number }> | null
  }
  timeline: Array<{ date: string; count: number; bytes: number }> | null
  category: Array<{ category: string; count: number }> | null
  duplicates: {
    exact_groups: number
    exact_files: number
    exact_saved_bytes: number
    similar_groups: number
    similar_files: number
    items:
      | Array<{
          kind: string
          file_ids: number[] | null
          file_names: string[] | null
          count: number
          saved_bytes: number
        }>
      | null
  }
}

function toStats(r: RawStats): SmartStats {
  return {
    storage: {
      usedBytes: r.storage.used_bytes,
      quotaBytes: r.storage.quota_bytes,
      fileCount: r.storage.file_count,
      byType: (r.storage.by_type ?? []).map((t) => ({
        type: t.type,
        bytes: t.bytes,
        count: t.count,
      })),
    },
    timeline: (r.timeline ?? []).map((p) => ({ date: p.date, count: p.count, bytes: p.bytes })),
    category: (r.category ?? []).map((c) => ({ category: c.category, count: c.count })),
    duplicates: {
      exactGroups: r.duplicates.exact_groups,
      exactFiles: r.duplicates.exact_files,
      exactSavedBytes: r.duplicates.exact_saved_bytes,
      similarGroups: r.duplicates.similar_groups,
      similarFiles: r.duplicates.similar_files,
      items: (r.duplicates.items ?? []).map((it) => ({
        kind: it.kind === 'similar' ? 'similar' : 'exact',
        fileIds: it.file_ids ?? [],
        fileNames: it.file_names ?? [],
        count: it.count,
        savedBytes: it.saved_bytes,
      })),
    },
  }
}

export const smartApi = {
  async stats(days = 14): Promise<SmartStats> {
    const { data } = await request.get<Biz<RawStats>>('/smart/stats', { params: { days } })
    return toStats(data.data)
  },
}
