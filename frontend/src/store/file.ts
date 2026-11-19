import { ref } from 'vue'
import { defineStore } from 'pinia'
import { fileApi, type FileItem } from '@/api/file'
import { uploadApi } from '@/api/upload'
import { computeFileHash } from '@/utils/hash'
import {
  CHUNK_SIZE,
  DEFAULT_CONCURRENCY,
  calcTotalChunks,
  checkChunkFile,
  decideInstant,
  isResumeUsable,
  runPool,
  sessionKey,
  uploadedBytesOf,
  type InstantDecision,
  type UploadOutcome,
} from '@/components/upload'

// S4 续传提示：可续传会话摘要（供 Uploader 展示“继续上传”按钮）
export interface ResumableInfo {
  fileName: string
  uploaded: number
  total: number
  skipped: number
}

// S4 文件状态：唯一可信源（items/total/page/parentId/crumbs），选中态等派生状态由组件本地维护
export interface Crumb {
  id: number
  name: string
}

// S1 上传阶段：idle 空闲 / hashing 算哈希 / uploading 传分片 / merging 合并
export type UploadPhase = 'idle' | 'hashing' | 'uploading' | 'merging'

export const useFileStore = defineStore('file', () => {
  const items = ref<FileItem[]>([])
  const total = ref(0)
  const page = ref(1)
  const pageSize = ref(20)
  const parentId = ref(0)
  const loading = ref(false)
  const uploading = ref(false)
  const progress = ref(0)
  const phase = ref<UploadPhase>('idle')
  const crumbs = ref<Crumb[]>([{ id: 0, name: '根目录' }])

  // S4 断点续传：同页会话内存表（fileKey -> uploadId），禁 localStorage；
  // 刷新即清空，仅覆盖“中断后同页重选/继续上传”场景（推进计划 W8-8.5 降级口径）。
  const pendingSessions = ref(new Map<string, string>())
  const resumable = ref<ResumableInfo | null>(null)
  const skippedChunks = ref(0)
  let lastFile: File | null = null
  let lastChunkSize = CHUNK_SIZE
  let lastConflict: (() => Promise<boolean>) | undefined

  async function fetchList(): Promise<void> {
    loading.value = true
    try {
      const res = await fileApi.list(parentId.value, page.value, pageSize.value)
      total.value = res.total
      page.value = res.page
      pageSize.value = res.pageSize
      items.value = res.items
      // 越界兜底：删空页时回退一页重拉（后端分页已归一，前端只处理越界）
      if (items.value.length === 0 && total.value > 0 && page.value > 1) {
        page.value -= 1
        const retry = await fileApi.list(parentId.value, page.value, pageSize.value)
        total.value = retry.total
        page.value = retry.page
        pageSize.value = retry.pageSize
        items.value = retry.items
      }
    } finally {
      loading.value = false
    }
  }

  async function enterFolder(f: FileItem): Promise<void> {
    crumbs.value.push({ id: f.id, name: f.fileName })
    parentId.value = f.id
    page.value = 1
    await fetchList()
  }

  async function goCrumb(id: number): Promise<void> {
    const idx = crumbs.value.findIndex((c) => c.id === id)
    if (idx < 0) {
      return
    }
    crumbs.value = crumbs.value.slice(0, idx + 1)
    parentId.value = id
    page.value = 1
    await fetchList()
  }

  async function refresh(): Promise<void> {
    await fetchList()
  }

  // S2 秒传预检：只读查询，不写盘不碰配额；决策映射见 decideInstant
  async function checkInstant(f: File, fileHash: string): Promise<InstantDecision> {
    const res = await fileApi.instantCheck({
      fileName: f.name,
      parentId: parentId.value,
      fileHash,
    })
    return decideInstant(res.hit, res.existsGlobal)
  }

  // S2 简单上传：哈希 → 预检 → 上传；hit 秒传零写请求；confirm-global 经回调确认，取消零写请求
  async function uploadOne(f: File, onConflict?: () => Promise<boolean>): Promise<UploadOutcome> {
    uploading.value = true
    progress.value = 0
    phase.value = 'hashing'
    try {
      const fileHash = await computeFileHash(f)
      progress.value = 10
      const decision = await checkInstant(f, fileHash)
      if (decision === 'instant') {
        progress.value = 100
        await fetchList()
        return 'instant'
      }
      if (decision === 'confirm-global') {
        const goOn = onConflict ? await onConflict() : false
        if (!goOn) {
          return 'cancelled'
        }
      }
      phase.value = 'uploading'
      await fileApi.upload(f, parentId.value, (p) => {
        progress.value = 10 + Math.round((90 * p) / 100)
      })
      await fetchList()
      return 'uploaded'
    } finally {
      uploading.value = false
      phase.value = 'idle'
    }
  }

  /**
   * S4 按序号列表传片（uploadChunked/resumeChunked 复用）：
   * 切片一律用本地 totalChunks/chunkSize，不依赖 init 回显；
   * 进度按字节口径，baseBytes 为已传基线（续传时由 uploaded 折算）。
   */
  async function uploadIndexes(
    f: File,
    uploadId: string,
    indexes: number[],
    chunkSize: number,
    baseBytes: number,
  ): Promise<void> {
    const hashWeight = 10
    const uploadWeight = 85
    let uploadedBytes = baseBytes
    const chunkDone = new Map<number, number>()
    const tasks = indexes.map((index) => {
      const start = index * chunkSize
      const end = Math.min(start + chunkSize, f.size)
      const blob = f.slice(start, end)
      return async (): Promise<void> => {
        await uploadApi.uploadChunk(uploadId, index, blob, (loaded) => {
          uploadedBytes += loaded - (chunkDone.get(index) ?? 0)
          chunkDone.set(index, loaded)
          const ratio = f.size > 0 ? uploadedBytes / f.size : 0
          progress.value = Math.min(
            hashWeight + uploadWeight,
            hashWeight + Math.round(uploadWeight * ratio),
          )
        })
      }
    })
    await runPool(tasks, DEFAULT_CONCURRENCY)
  }

  /**
   * S4 断点续传：>2MB 文件走此路径（uploadChunked 薄包装调此函数）。
   * 流程：Worker 算 SHA256 → S2 预检 → 有旧会话则 GET progress 校验
   * → complete=true 直调 complete；否则只补 missing；404/漂移则全新 init。
   * 失败保留 Map 供“继续上传”；成功/cancel/秒传则清理。
   */
  async function resumeChunked(
    f: File,
    chunkSize: number = CHUNK_SIZE,
    onConflict?: () => Promise<boolean>,
  ): Promise<UploadOutcome> {
    const totalChunks = calcTotalChunks(f.size, chunkSize)
    const reason = checkChunkFile(f.size, totalChunks)
    if (reason) {
      throw new Error(reason)
    }
    // 记住本次文件，供“继续上传”按钮复用同一 File 对象重入
    lastFile = f
    lastChunkSize = chunkSize
    lastConflict = onConflict
    uploading.value = true
    progress.value = 0
    phase.value = 'hashing'
    skippedChunks.value = 0
    resumable.value = null
    try {
      const fileHash = await computeFileHash(f)
      progress.value = 10
      // S2 秒传预检：hit 秒传；confirm-global 经回调确认，取消则零写请求返回
      const decision = await checkInstant(f, fileHash)
      if (decision === 'instant') {
        progress.value = 100
        lastFile = null
        await fetchList()
        return 'instant'
      }
      if (decision === 'confirm-global') {
        const goOn = onConflict ? await onConflict() : false
        if (!goOn) {
          lastFile = null
          return 'cancelled'
        }
      }

      const key = sessionKey(fileHash, f.size, totalChunks, chunkSize)
      const oldId = pendingSessions.value.get(key)
      let uploadId = oldId ?? ''
      let missing: number[] = Array.from({ length: totalChunks }, (_, i) => i)
      let baseBytes = 0

      if (oldId) {
        // 先只查 progress：查询失败（404/400 会话过期）或回显漂移 → 丢弃旧会话走全新 init；
        // complete 调用的失败不在此吞，单独上浮以保留 Map 供重试。
        let usable = false
        try {
          phase.value = 'uploading'
          const p = await uploadApi.progress(oldId)
          if (!isResumeUsable(p, { fileHash, totalChunks, chunkSize })) {
            throw new Error('stale-session')
          }
          if (p.complete) {
            // 全片已在盘：跳过传片直调 complete（闪断在 complete 前的收尾）；
            // 此处 complete 失败直接上浮，不删 Map，保留“继续上传”重试。
            skippedChunks.value = totalChunks
            resumable.value = null
            phase.value = 'merging'
            progress.value = 95
            await uploadApi.complete(oldId, f.name, parentId.value)
            progress.value = 100
            await fetchList()
            pendingSessions.value.delete(key)
            lastFile = null
            return 'uploaded'
          }
          missing = [...p.missing].sort((a, b) => a - b)
          baseBytes = uploadedBytesOf(p.uploaded, f.size, chunkSize)
          skippedChunks.value = p.uploaded.length
          const ratio = f.size > 0 ? baseBytes / f.size : 0
          progress.value = 10 + Math.round(85 * ratio)
          uploadId = oldId
          usable = true
          if (missing.length > 0) {
            resumable.value = {
              fileName: f.name,
              uploaded: p.uploaded.length,
              total: totalChunks,
              skipped: p.uploaded.length,
            }
          }
        } catch (e) {
          if (usable) {
            // complete=true 分支内的 complete 失败：保留 Map 上浮
            throw e
          }
          // progress 查询失败或 stale：丢弃旧会话走全新 init
          pendingSessions.value.delete(key)
          uploadId = ''
          missing = Array.from({ length: totalChunks }, (_, i) => i)
          baseBytes = 0
          skippedChunks.value = 0
          resumable.value = null
        }
      }

      if (!uploadId) {
        phase.value = 'uploading'
        const init = await uploadApi.init({
          fileName: f.name,
          parentId: parentId.value,
          fileHash,
          totalSize: f.size,
          totalChunks,
          chunkSize,
        })
        uploadId = init.uploadId
        pendingSessions.value.set(key, uploadId)
        missing = Array.from({ length: totalChunks }, (_, i) => i)
        baseBytes = 0
      }

      phase.value = 'uploading'
      try {
        await uploadIndexes(f, uploadId, missing, chunkSize, baseBytes)
      } catch (e) {
        // 补传中断：保留 Map + resumable 供“继续上传”，上浮错误由 Uploader 弹窗
        const doneCount = totalChunks - missing.length
        resumable.value = {
          fileName: f.name,
          uploaded: doneCount,
          total: totalChunks,
          skipped: doneCount,
        }
        throw e
      }
      phase.value = 'merging'
      progress.value = 95
      await uploadApi.complete(uploadId, f.name, parentId.value)
      progress.value = 100
      await fetchList()
      pendingSessions.value.delete(key)
      resumable.value = null
      // S4：skipped 保留到下次上传开始，供 Uploader 成功提示“已跳过 N 片”
      lastFile = null
      return 'uploaded'
    } finally {
      uploading.value = false
      phase.value = 'idle'
    }
  }

  /**
   * S1 分片上传：>2MB 文件走此路径（S4 起委托 resumeChunked，保留签名兼容旧调用）。
   * 进度按已传字节 / 总字节计算（不按片数，避免末片大小差异导致跳变）。
   */
  async function uploadChunked(
    f: File,
    chunkSize: number = CHUNK_SIZE,
    onConflict?: () => Promise<boolean>,
  ): Promise<UploadOutcome> {
    return resumeChunked(f, chunkSize, onConflict)
  }

  /** S4 “继续上传”：复用中断时同一 File 对象重入 resumeChunked */
  async function resumeLast(): Promise<UploadOutcome> {
    if (!lastFile) {
      throw new Error('没有可继续的上传任务')
    }
    return resumeChunked(lastFile, lastChunkSize, lastConflict)
  }

  function reset(): void {
    items.value = []
    total.value = 0
    page.value = 1
    pageSize.value = 20
    parentId.value = 0
    uploading.value = false
    progress.value = 0
    phase.value = 'idle'
    crumbs.value = [{ id: 0, name: '根目录' }]
    pendingSessions.value.clear()
    resumable.value = null
    skippedChunks.value = 0
    lastFile = null
  }

  return {
    items,
    total,
    page,
    pageSize,
    parentId,
    loading,
    uploading,
    progress,
    phase,
    crumbs,
    resumable,
    skippedChunks,
    fetchList,
    enterFolder,
    goCrumb,
    refresh,
    uploadOne,
    uploadChunked,
    resumeChunked,
    resumeLast,
    reset,
  }
})
