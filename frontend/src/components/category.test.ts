import { describe, expect, it } from 'vitest'
import { categoryKind, categoryLabel } from '@/components/category'

describe('S4 categoryKind（9 大类轮换 3 套粉彩）', () => {
  it('紫组', () => {
    expect(categoryKind('合同')).toBe('purple')
    expect(categoryKind('图片')).toBe('purple')
    expect(categoryKind('文档')).toBe('purple')
  })

  it('橙组', () => {
    expect(categoryKind('简历')).toBe('orange')
    expect(categoryKind('视频')).toBe('orange')
    expect(categoryKind('压缩包')).toBe('orange')
  })

  it('绿组', () => {
    expect(categoryKind('报表')).toBe('green')
    expect(categoryKind('音频')).toBe('green')
    expect(categoryKind('其他')).toBe('green')
  })

  it('空/未知兜底 pending', () => {
    expect(categoryKind('')).toBe('pending')
    expect(categoryKind('未知类')).toBe('pending')
  })
})

describe('S4 categoryLabel', () => {
  it('空显示待确认', () => {
    expect(categoryLabel('')).toBe('待确认')
    expect(categoryLabel('合同')).toBe('合同')
  })
})
