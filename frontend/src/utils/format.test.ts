import { describe, expect, it } from 'vitest'
import { formatSize, validFileName } from '@/utils/format'

describe('S4 formatSize', () => {
  it('0 与非法值兜底 0 B', () => {
    expect(formatSize(0)).toBe('0 B')
    expect(formatSize(-1)).toBe('0 B')
    expect(formatSize(Number.NaN)).toBe('0 B')
  })

  it('1024 进制换算', () => {
    expect(formatSize(1024)).toBe('1 KB')
    expect(formatSize(1536)).toBe('1.5 KB')
    expect(formatSize(1048576)).toBe('1 MB')
  })
})

describe('S4 validFileName（与后端 validFileName 同口径）', () => {
  it('空/空白非法', () => {
    expect(validFileName('')).toBe(false)
    expect(validFileName('   ')).toBe(false)
  })

  it('含斜杠非法', () => {
    expect(validFileName('a/b')).toBe(false)
    expect(validFileName('a\\b')).toBe(false)
  })

  it('超 255 rune 非法，正常通过', () => {
    expect(validFileName('a'.repeat(256))).toBe(false)
    expect(validFileName('docs')).toBe(true)
    expect(validFileName('报告2024.pdf')).toBe(true)
  })
})
