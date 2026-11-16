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
  runPool,
  type InstantDecision,
  type UploadOutcome,
} from '@/components/upload'

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
   * S1 分片上传：>2MB 文件走此路径。
   * 流程：Worker 算 SHA256 → init 会话 → 并发传片 → complete 合并 → 刷列表。
   * 进度按已传字节 / 总字节计算（不按片数，避免末片大小差异导致跳变）。
   */
  async function uploadChunked(
    f: File,
    chunkSize: number = CHUNK_SIZE,
    onConflict?: () => Promise<boolean>,
  ): Promise<UploadOutcome> {
    const totalChunks = calcTotalChunks(f.size, chunkSize)
    const reason = checkChunkFile(f.size, totalChunks)
    if (reason) {
      throw new Error(reason)
    }
    uploading.value = true
    progress.value = 0
    phase.value = 'hashing'
    try {
      const fileHash = await computeFileHash(f)
      progress.value = 10
      // S2 秒传预检：hit 本人已有直接秒传；confirm-global 经回调确认，取消则零写请求返回
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
      const init = await uploadApi.init({
        fileName: f.name,
        parentId: parentId.value,
        fileHash,
        totalSize: f.size,
        totalChunks,
        chunkSize,
      })

      // 哈希阶段占 10%，合并阶段占 5%，中间 85% 按已传字节推进
      const hashWeight = 10
      const uploadWeight = 85
      progress.value = hashWeight
      let uploadedBytes = 0
      const chunkDone = new Map<number, number>()

      // 切片一律用本地计算的 totalChunks/chunkSize，不依赖 init 回显：
      // 后端按请求值校验并回显同一值，但前端以本地值为准可防回显异常导致漏传/越界。
      const tasks = Array.from({ length: totalChunks }, (_, index) => {
        const start = index * chunkSize
        const end = Math.min(start + chunkSize, f.size)
        const blob = f.slice(start, end)
        return async (): Promise<void> => {
          await uploadApi.uploadChunk(init.uploadId, index, blob, (loaded) => {
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
      phase.value = 'merging'
      progress.value = hashWeight + uploadWeight
      await uploadApi.complete(init.uploadId, f.name, parentId.value)
      progress.value = 100
      await fetchList()
      return 'uploaded'
    } finally {
      uploading.value = false
      phase.value = 'idle'
    }
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
    fetchList,
    enterFolder,
    goCrumb,
    refresh,
    uploadOne,
    uploadChunked,
    reset,
  }
})
