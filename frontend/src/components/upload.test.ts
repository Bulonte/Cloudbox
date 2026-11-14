import { describe, expect, it } from 'vitest'
import { MAX_SIMPLE_SIZE, isSimpleSizeOk } from '@/components/upload'

describe('S5 isSimpleSizeOk（PRD §2.1 冻结口径 ≤2MB）', () => {
  it('阈值常量为 2MB', () => {
    expect(MAX_SIMPLE_SIZE).toBe(2 * 1024 * 1024)
  })

  it('0 字节与超限拒绝', () => {
    expect(isSimpleSizeOk(0)).toBe(false)
    expect(isSimpleSizeOk(-1)).toBe(false)
    expect(isSimpleSizeOk(MAX_SIMPLE_SIZE + 1)).toBe(false)
  })

  it('边界内通过', () => {
    expect(isSimpleSizeOk(1)).toBe(true)
    expect(isSimpleSizeOk(MAX_SIMPLE_SIZE - 1)).toBe(true)
    expect(isSimpleSizeOk(MAX_SIMPLE_SIZE)).toBe(true)
  })

  it('非法数值拒绝', () => {
    expect(isSimpleSizeOk(Number.NaN)).toBe(false)
    expect(isSimpleSizeOk(Number.POSITIVE_INFINITY)).toBe(false)
  })
})
