import type { CategoryItem, DuplicatesBlock, StorageBlock, TimelinePoint } from '@/api/smart'

// S5 仪表盘纯函数：ECharts option 组装 + 数字格式化，零副作用可单测
// 空数组/缺省一律兜底，不抛错不断线。

export const TYPE_LABELS: Record<string, string> = {
  image: '图片',
  video: '视频',
  audio: '音频',
  document: '文档',
  archive: '压缩包',
  other: '其他',
}

export const TYPE_ORDER = ['image', 'video', 'audio', 'document', 'archive', 'other']

/** 字节格式化：B/KB/MB/GB，保留 1 位小数 */
export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i += 1
  }
  return `${v.toFixed(1)} ${units[i]}`
}

/** 配额使用率 0..1，quota 为 0 时回 0（防除零） */
export function usageRate(used: number, quota: number): number {
  if (!Number.isFinite(used) || !Number.isFinite(quota) || quota <= 0) return 0
  if (used <= 0) return 0
  return Math.min(1, used / quota)
}

interface PieDatum {
  name: string
  value: number
}

/** 存储环形图：按 6 类固定顺序，空数据给占位扇区不断线 */
export function storageRingOption(storage: StorageBlock): {
  series: Array<{ type: string; data: PieDatum[] }>
} {
  const byType = new Map(storage.byType.map((t) => [t.type, t.bytes]))
  const data: PieDatum[] = TYPE_ORDER.map((t) => ({
    name: TYPE_LABELS[t] ?? t,
    value: byType.get(t) ?? 0,
  }))
  if (data.every((d) => d.value <= 0)) {
    return { series: [{ type: 'pie', data: [{ name: '暂无数据', value: 1 }] }] }
  }
  return { series: [{ type: 'pie', data }] }
}

/** 上传时间线：date 作 x 轴，count 作 y 轴 */
export function timelineOption(points: TimelinePoint[]): {
  xAxis: { data: string[] }
  series: Array<{ type: string; data: number[] }>
} {
  return {
    xAxis: { data: points.map((p) => p.date) },
    series: [{ type: 'line', data: points.map((p) => p.count) }],
  }
}

/** 分类饼图：空数据占位不断线 */
export function categoryPieOption(items: CategoryItem[]): {
  series: Array<{ type: string; data: PieDatum[] }>
} {
  if (items.length === 0) {
    return { series: [{ type: 'pie', data: [{ name: '暂无数据', value: 1 }] }] }
  }
  return {
    series: [
      {
        type: 'pie',
        data: items.map((c) => ({ name: c.category, value: c.count })),
      },
    ],
  }
}

export interface DuplicateRow {
  kind: string
  names: string
  count: number
  saved: string
}

/** 重复报告表格行：exact 显示可省字节，similar 显示"—" */
export function duplicateRows(d: DuplicatesBlock): DuplicateRow[] {
  return d.items.map((it) => ({
    kind: it.kind === 'similar' ? '相似' : '精确重复',
    names: it.fileNames.slice(0, 3).join('、'),
    count: it.count,
    saved: it.kind === 'exact' ? formatBytes(it.savedBytes) : '—',
  }))
}
