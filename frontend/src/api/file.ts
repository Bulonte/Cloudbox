import request from './request'

// 后端 DTO 对齐（backend/internal/api/dto/file.go）：
// snake_case 原样收，camelCase 转换放在本文件，组件只认 camelCase。
// 注意：后端 FileResponse 无 confidence 字段，S4 按实际契约只透出 category；
// 空 category 前端显示“待确认”，不虚构 confidence 阈值。

export interface FileItem {
  id: number
  fileName: string
  parentId: number
  isFolder: number
  fileSize: number
  mimeType: string
  category: string
  createdAt: string
  updatedAt: string
}

export interface FilePage {
  total: number
  page: number
  pageSize: number
  items: FileItem[]
}

interface Biz<T> {
  code: number
  message: string
  data: T
}

interface RawFile {
  id: number
  file_name: string
  parent_id: number
  is_folder: number
  file_size: number
  mime_type: string
  category?: string
  created_at: string
  updated_at: string
}

interface RawFilePage {
  total: number
  page: number
  page_size: number
  items: RawFile[] | null
}

function toFile(r: RawFile): FileItem {
  return {
    id: r.id,
    fileName: r.file_name,
    parentId: r.parent_id,
    isFolder: r.is_folder,
    fileSize: r.file_size,
    mimeType: r.mime_type,
    category: r.category ?? '',
    createdAt: r.created_at,
    updatedAt: r.updated_at,
  }
}

function toPage(r: RawFilePage): FilePage {
  return {
    total: r.total,
    page: r.page,
    pageSize: r.page_size,
    items: (r.items ?? []).map(toFile),
  }
}

export const fileApi = {
  async list(parentId: number, page: number, pageSize: number): Promise<FilePage> {
    const { data } = await request.get<Biz<RawFilePage>>('/files', {
      params: { parent_id: parentId, page, page_size: pageSize },
    })
    return toPage(data.data)
  },
  async createFolder(payload: { fileName: string; parentId: number }): Promise<FileItem> {
    const { data } = await request.post<Biz<RawFile>>('/files/folders', {
      file_name: payload.fileName,
      parent_id: payload.parentId,
    })
    return toFile(data.data)
  },
  async update(id: number, payload: { fileName?: string; parentId?: number }): Promise<FileItem> {
    const body: { file_name?: string; parent_id?: number } = {}
    if (payload.fileName !== undefined) {
      body.file_name = payload.fileName
    }
    if (payload.parentId !== undefined) {
      body.parent_id = payload.parentId
    }
    const { data } = await request.put<Biz<RawFile>>(`/files/${String(id)}`, body)
    return toFile(data.data)
  },
  async remove(id: number): Promise<void> {
    await request.delete<Biz<null>>(`/files/${String(id)}`)
  },
}
