import { describe, expect, it } from 'vitest'
import {
  CHUNK_SIZE,
  DEFAULT_CONCURRENCY,
  MAX_CHUNK_COUNT,
  MAX_CHUNK_FILE_SIZE,
  MAX_CHUNK_SIZE,
  calcTotalChunks,
  checkChunkFile,
  decideInstant,
  isChunkSizeOk,
  runPool,
} from '@/components/upload'

describe('S1 分片常量（与后端 service/chunk.go 对齐）', () => {
  it('分片大小默认 2MB、上限 8MB', () => {
    expect(CHUNK_SIZE).toBe(2 * 1024 * 1024)
    expect(MAX_CHUNK_SIZE).toBe(8 * 1024 * 1024)
  })

  it('分片总数上限 10000、并发上限 3、前端单文件上限 500MB', () => {
    expect(MAX_CHUNK_COUNT).toBe(10000)
    expect(DEFAULT_CONCURRENCY).toBe(3)
    expect(MAX_CHUNK_FILE_SIZE).toBe(500 * 1024 * 1024)
  })
})

describe('S1 calcTotalChunks（须与后端 want 公式一致）', () => {
  it('整除时片数等于商', () => {
    expect(calcTotalChunks(4 * CHUNK_SIZE, CHUNK_SIZE)).toBe(4)
  })

  it('有余数时向上取整', () => {
    expect(calcTotalChunks(4 * CHUNK_SIZE + 1, CHUNK_SIZE)).toBe(5)
  })

  it('末片仅 1 字节也计一片', () => {
    expect(calcTotalChunks(2 * CHUNK_SIZE + 1, CHUNK_SIZE)).toBe(3)
  })

  it('非法输入返回 0', () => {
    expect(calcTotalChunks(0, CHUNK_SIZE)).toBe(0)
    expect(calcTotalChunks(-1, CHUNK_SIZE)).toBe(0)
    expect(calcTotalChunks(100, 0)).toBe(0)
    expect(calcTotalChunks(Number.NaN, CHUNK_SIZE)).toBe(0)
  })
})

describe('S1 isChunkSizeOk', () => {
  it('区间内通过、超限与非法拒绝', () => {
    expect(isChunkSizeOk(CHUNK_SIZE)).toBe(true)
    expect(isChunkSizeOk(MAX_CHUNK_SIZE)).toBe(true)
    expect(isChunkSizeOk(MAX_CHUNK_SIZE + 1)).toBe(false)
    expect(isChunkSizeOk(0)).toBe(false)
    expect(isChunkSizeOk(Number.NaN)).toBe(false)
  })
})

describe('S1 checkChunkFile', () => {
  it('正常体积通过', () => {
    expect(checkChunkFile(3 * CHUNK_SIZE, 2)).toBeNull()
  })

  it('空文件与超 500MB 拒绝并给中文原因', () => {
    expect(checkChunkFile(0, 1)).toContain('文件为空')
    expect(checkChunkFile(MAX_CHUNK_FILE_SIZE + 1, 300)).toContain('500MB')
  })

  it('分片数超上限拒绝', () => {
    expect(checkChunkFile(100, MAX_CHUNK_COUNT + 1)).toContain('分片数超出上限')
  })
})

describe('S1 runPool（并发上限必须有界）', () => {
  it('全部任务执行完成', async () => {
    const done: number[] = []
    const tasks = Array.from({ length: 9 }, (_, i) => async (): Promise<void> => {
      done.push(i)
    })
    await runPool(tasks, 3)
    expect(done.length).toBe(9)
  })

  it('并发数不超过 limit', async () => {
    let active = 0
    let peak = 0
    const tasks = Array.from({ length: 10 }, () => async (): Promise<void> => {
      active += 1
      peak = Math.max(peak, active)
      await Promise.resolve()
      active -= 1
    })
    await runPool(tasks, 3)
    expect(peak).toBeLessThanOrEqual(3)
  })

  it('空任务列表直接返回', async () => {
    await expect(runPool([], 3)).resolves.toBeUndefined()
  })

  it('任务失败时整体 reject', async () => {
    const tasks = [
      async (): Promise<void> => {
        throw new Error('boom')
      },
    ]
    await expect(runPool(tasks, 3)).rejects.toThrow('boom')
  })
})

describe('S2 decideInstant（与后端 InstantResult 三分支对齐）', () => {
  it('本人命中走秒传', () => {
    expect(decideInstant(true, false)).toBe('instant')
    expect(decideInstant(true, true)).toBe('instant')
  })

  it('他人已存需用户确认', () => {
    expect(decideInstant(false, true)).toBe('confirm-global')
  })

  it('全 miss 走正常上传', () => {
    expect(decideInstant(false, false)).toBe('upload')
  })
})
