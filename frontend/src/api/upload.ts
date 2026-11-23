import request from './request'
import { toFile, type FileItem } from './file'

// 后端 DTO 对齐（backend/internal/api/dto/upload_chunk.go）：
// snake_case 原样收，camelCase 转换放在本文件，组件只认 camelCase。
// 端点全部挂在 AuthRequired 下，归属与配额由服务端校验，前端禁传 owner_id 伪造。

export interface InitUploadResult {
  uploadId: string
  fileHash: string
  totalChunks: number
  chunkSize: number
  status: number
}

export interface UploadChunkResult {
  uploadId: string
  chunkIndex: number
  size: number
  status: number
}

export interface UploadProgress {
  uploadId: string
  fileHash: string
  totalChunks: number
  chunkSize: number
  status: number
  uploaded: number[]
  missing: number[]
  complete: boolean
}

interface Biz<T> {
  code: number
  message: string
  data: T
}

interface RawInitUpload {
  upload_id: string
  file_hash: string
  total_chunks: number
  chunk_size: number
  status: number
}

interface RawUploadChunk {
  upload_id: string
  chunk_index: number
  size: number
  status: number
}

interface RawProgress {
  upload_id: string
  file_hash: string
  total_chunks: number
  chunk_size: number
  status: number
  uploaded: number[] | null
  missing: number[] | null
  complete: boolean
}

export const uploadApi = {
  /** POST /api/v1/uploads/init：签发分片会话（服务端校验 hash 格式、总数一致性、配额） */
  async init(payload: {
    fileName: string
    parentId: number
    fileHash: string
    totalSize: number
    totalChunks: number
    chunkSize: number
  }): Promise<InitUploadResult> {
    const { data } = await request.post<Biz<RawInitUpload>>('/uploads/init', {
      file_name: payload.fileName,
      parent_id: payload.parentId,
      file_hash: payload.fileHash,
      total_size: payload.totalSize,
      total_chunks: payload.totalChunks,
      chunk_size: payload.chunkSize,
    })
    return {
      uploadId: data.data.upload_id,
      fileHash: data.data.file_hash,
      totalChunks: data.data.total_chunks,
      chunkSize: data.data.chunk_size,
      status: data.data.status,
    }
  },

  /**
   * POST /api/v1/uploads/:id/chunks：上传单个分片。
   * multipart：chunk_index 文本字段 + chunk 文件字段；勿手写 Content-Type（axios 自动带 boundary）。
   */
  async uploadChunk(
    uploadId: string,
    index: number,
    blob: Blob,
    onProgress?: (loaded: number) => void,
  ): Promise<UploadChunkResult> {
    const fd = new FormData()
    fd.append('chunk_index', String(index))
    fd.append('chunk', blob)
    const { data } = await request.post<Biz<RawUploadChunk>>(`/uploads/${encodeURIComponent(uploadId)}/chunks`, fd, {
      onUploadProgress: (e) => {
        onProgress?.(e.loaded)
      },
    })
    return {
      uploadId: data.data.upload_id,
      chunkIndex: data.data.chunk_index,
      size: data.data.size,
      status: data.data.status,
    }
  },

  /** POST /api/v1/uploads/:id/complete：合并收官，响应复用 FileResponse */
  async complete(uploadId: string, fileName: string, parentId: number): Promise<FileItem> {
    const { data } = await request.post<Biz<Parameters<typeof toFile>[0]>>(
      `/uploads/${encodeURIComponent(uploadId)}/complete`,
      { file_name: fileName, parent_id: parentId },
    )
    return toFile(data.data)
  },

  /** GET /api/v1/uploads/:id/progress：断点续传进度（uploaded/missing 恒为数组） */
  async progress(uploadId: string): Promise<UploadProgress> {
    const { data } = await request.get<Biz<RawProgress>>(`/uploads/${encodeURIComponent(uploadId)}/progress`)
    return {
      uploadId: data.data.upload_id,
      fileHash: data.data.file_hash,
      totalChunks: data.data.total_chunks,
      chunkSize: data.data.chunk_size,
      status: data.data.status,
      uploaded: data.data.uploaded ?? [],
      missing: data.data.missing ?? [],
      complete: data.data.complete,
    }
  },

  /** POST /api/v1/uploads/:id/cancel：取消会话（幂等，重复取消仍 200） */
  async cancel(uploadId: string): Promise<void> {
    await request.post<Biz<null>>(`/uploads/${encodeURIComponent(uploadId)}/cancel`)
  },
}
