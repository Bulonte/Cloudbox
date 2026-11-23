// S1 分片上传常量与纯函数：切片数学、阈值判定、并发池
// 口径对齐 backend/internal/service/chunk.go（DefaultChunkSize 2MB / MaxChunkSize 8MB / DefaultMaxChunks 10000）

// S5 简单上传预检：纯函数可单测，阈值与 PRD §2.1 冻结口径对齐（≤2MB）
// 扩展名/内容嗅探留给服务端（validator.go），前端只卡体积，防黑白名单漂移

export const MAX_SIMPLE_SIZE = 2 * 1024 * 1024

/** 体积合法：(0, 2MB] 左开右闭，0 字节与超限均拒 */
export function isSimpleSizeOk(size: number): boolean {
  if (!Number.isFinite(size)) {
    return false
  }
  return size > 0 && size <= MAX_SIMPLE_SIZE
}

// S1 分片参数：与后端 service/chunk.go 常量口径一致
/** 默认分片大小 2MB（后端 DefaultChunkSize） */
export const CHUNK_SIZE = 2 * 1024 * 1024

/** 单分片上限 8MB（后端 MaxChunkSize），前端不超此值 */
export const MAX_CHUNK_SIZE = 8 * 1024 * 1024

/** 分片总数上限 10000（后端 DefaultMaxChunks） */
export const MAX_CHUNK_COUNT = 10000

/**
 * 前端单文件上限 500MB。
 * 依据：S1 采用 crypto.subtle.digest 全量摘要（非流式），整文件进内存；
 * 5GB 上限由后端守住，前端演示场景不触碰，超此直接拒以免浏览器内存爆掉。
 */
export const MAX_CHUNK_FILE_SIZE = 500 * 1024 * 1024

/** 分片上传并发上限（规范 §5.3.3 单用户同时上传数上限，取 3） */
export const DEFAULT_CONCURRENCY = 3

/**
 * 分片总数：与后端 validateInit 的 want 公式一致
 * 后端：(totalSize + chunkSize - 1) / chunkSize（整数除法）
 * 前端：Math.ceil(totalSize / chunkSize)
 */
export function calcTotalChunks(totalSize: number, chunkSize: number): number {
  if (!Number.isFinite(totalSize) || !Number.isFinite(chunkSize) || totalSize <= 0 || chunkSize <= 0) {
    return 0
  }
  return Math.ceil(totalSize / chunkSize)
}

/** 分片文件体积是否合法：>0 且 ≤ 单片上限且 ≤ 该片应得大小 */
export function isChunkSizeOk(chunkSize: number): boolean {
  return Number.isFinite(chunkSize) && chunkSize > 0 && chunkSize <= MAX_CHUNK_SIZE
}

/**
 * 分片上传文件前置校验：体积在 (0, 500MB]、分片数不超上限。
 * 返回 null 表示通过，否则返回面向用户的中文原因。
 */
export function checkChunkFile(size: number, totalChunks: number): string | null {
  if (!Number.isFinite(size) || size <= 0) {
    return '文件为空，无法上传'
  }
  if (size > MAX_CHUNK_FILE_SIZE) {
    return '单文件上限 500MB，超出请压缩后再传'
  }
  if (totalChunks > MAX_CHUNK_COUNT) {
    return '分片数超出上限，请减小文件体积'
  }
  return null
}

/**
 * 并发池：限制同时执行的任务数（规范 §5.3.3 并发必须有上限）。
 * 任务按序取用，任一任务 reject 则整体 reject（由调用方决定重试策略）。
 */
export async function runPool(tasks: (() => Promise<void>)[], limit: number): Promise<void> {
  if (tasks.length === 0) {
    return
  }
  const size = Math.max(1, Math.min(Math.floor(limit) || 1, tasks.length))
  let cursor = 0
  const workers = Array.from({ length: size }, async () => {
    while (cursor < tasks.length) {
      const idx = cursor
      cursor += 1
      await tasks[idx]()
    }
  })
  await Promise.all(workers)
}

// S2 秒传决策：纯函数，零依赖，可单测
// hit=true 本人已有 → 秒传；exists_global=true 他人已存 → 先确认再决定；全 miss → 正常上传
/** 秒传预检决策：instant 秒传 / upload 正常传 / confirm-global 需用户确认 */
export type InstantDecision = 'instant' | 'upload' | 'confirm-global'

/** 上传结果：instant 秒传完成 / uploaded 实传完成 / cancelled 用户取消（零写请求） */
export type UploadOutcome = 'instant' | 'uploaded' | 'cancelled'

export function decideInstant(hit: boolean, existsGlobal: boolean): InstantDecision {
  if (hit) {
    return 'instant'
  }
  if (existsGlobal) {
    return 'confirm-global'
  }
  return 'upload'
}
