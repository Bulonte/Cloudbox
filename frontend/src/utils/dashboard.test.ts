import { describe, expect, it } from 'vitest'
import {
  categoryPieOption,
  duplicateRows,
  formatBytes,
  storageRingOption,
  timelineOption,
  usageRate,
} from '@/utils/dashboard'

describe('S5 dashboard 纯函数', () => {
  it('formatBytes 进制与非法兜底', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(-5)).toBe('0 B')
    expect(formatBytes(NaN)).toBe('0 B')
    expect(formatBytes(1536)).toBe('1.5 KB')
    expect(formatBytes(1048576)).toBe('1.0 MB')
  })

  it('usageRate 除零与越界钳制', () => {
    expect(usageRate(100, 0)).toBe(0)
    expect(usageRate(0, 100)).toBe(0)
    expect(usageRate(200, 100)).toBe(1)
    expect(usageRate(50, 100)).toBeCloseTo(0.5)
  })

  it('storageRingOption 固定 6 项顺序，空数据占位', () => {
    const opt = storageRingOption({ usedBytes: 100, quotaBytes: 1000, fileCount: 2, byType: [] })
    expect(opt.series[0]?.data).toEqual([{ name: '暂无数据', value: 1 }])
    const opt2 = storageRingOption({
      usedBytes: 100,
      quotaBytes: 1000,
      fileCount: 2,
      byType: [{ type: 'image', bytes: 100, count: 2 }],
    })
    expect(opt2.series[0]?.data).toHaveLength(6)
    expect(opt2.series[0]?.data[0]?.name).toBe('图片')
  })

  it('timelineOption 透 date/count', () => {
    const opt = timelineOption([{ date: '2026-11-19', count: 3, bytes: 30 }])
    expect(opt.xAxis.data).toEqual(['2026-11-19'])
    expect(opt.series[0]?.data).toEqual([3])
  })

  it('categoryPieOption 空占位不断线', () => {
    expect(categoryPieOption([]).series[0]?.data).toEqual([{ name: '暂无数据', value: 1 }])
    const opt = categoryPieOption([{ category: '图片', count: 2 }])
    expect(opt.series[0]?.data).toEqual([{ name: '图片', value: 2 }])
  })

  it('duplicateRows exact 显示字节 similar 显示—', () => {
    const rows = duplicateRows({
      exactGroups: 1,
      exactFiles: 2,
      exactSavedBytes: 500,
      similarGroups: 1,
      similarFiles: 2,
      items: [
        { kind: 'exact', fileIds: [1, 2], fileNames: ['a', 'b', 'c', 'd'], count: 2, savedBytes: 500 },
        { kind: 'similar', fileIds: [3], fileNames: ['x'], count: 2, savedBytes: 0 },
      ],
    })
    expect(rows[0]?.kind).toBe('精确重复')
    expect(rows[0]?.names).toBe('a、b、c')
    expect(rows[1]?.saved).toBe('—')
  })
})
