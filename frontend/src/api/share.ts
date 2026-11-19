import request from './request'

// 后端 DTO 对齐（backend/internal/api/dto/share.go）：
// snake_case 原样收，camelCase 转换放在本文件，组件只认 camelCase。
// 安全：禁止在此打印密码 / 分享码明文。

export interface ShareItem {
  id: number
  shareCode: string
  shareUrl: string
  fileId: number
  fileName: string
  expireAt: string | null
  maxDownloads: number | null
  downloadCount: number
  createdAt: string
}

export interface SharePage {
  total: number
  page: number
  pageSize: number
  items: ShareItem[]
}

export interface CreatedShare {
  id: number
  shareCode: string
  shareUrl: string
  fileId: number
  expireAt: string | null
  maxDownloads: number | null
}

export interface ShareInfo {
  shareCode: string
  fileName: string
  fileSize: number
  mimeType: string
  expireAt: string | null
}

interface Biz<T> {
  code: number
  message: string
  data: T
}

interface RawShareItem {
  id: number
  share_code: string
  share_url: string
  file_id: number
  file_name: string
  expire_at?: string | null
  max_downloads?: number | null
  download_count: number
  created_at: string
}

interface RawSharePage {
  total: number
  page: number
  page_size: number
  items: RawShareItem[] | null
}

interface RawCreated {
  id: number
  share_code: string
  share_url: string
  file_id: number
  expire_at?: string | null
  max_downloads?: number | null
}

interface RawShareInfo {
  share_code: string
  file_name: string
  file_size: number
  mime_type: string
  expire_at?: string | null
}

function toItem(r: RawShareItem): ShareItem {
  return {
    id: r.id,
    shareCode: r.share_code,
    shareUrl: r.share_url,
    fileId: r.file_id,
    fileName: r.file_name,
    expireAt: r.expire_at ?? null,
    maxDownloads: r.max_downloads ?? null,
    downloadCount: r.download_count,
    createdAt: r.created_at,
  }
}

/** 后端返回 "/s/xxx" 为相对路径，前端拼 hash 路由得到可复制链接 */
export function toPublicLink(shareUrl: string): string {
  const path = shareUrl.startsWith('/s/') ? `/#/s/${shareUrl.slice(3)}` : `/#${shareUrl}`
  return `${window.location.origin}${path}`
}

export const shareApi = {
  async create(payload: {
    fileId: number
    password?: string
    expireAt?: string
    maxDownloads?: number
  }): Promise<CreatedShare> {
    const body: { file_id: number; password?: string; expire_at?: string; max_downloads?: number } = {
      file_id: payload.fileId,
    }
    if (payload.password !== undefined && payload.password !== '') {
      body.password = payload.password
    }
    if (payload.expireAt !== undefined && payload.expireAt !== '') {
      body.expire_at = payload.expireAt
    }
    if (payload.maxDownloads !== undefined) {
      body.max_downloads = payload.maxDownloads
    }
    const { data } = await request.post<Biz<RawCreated>>('/shares', body)
    const r = data.data
    return {
      id: r.id,
      shareCode: r.share_code,
      shareUrl: r.share_url,
      fileId: r.file_id,
      expireAt: r.expire_at ?? null,
      maxDownloads: r.max_downloads ?? null,
    }
  },
  async list(page: number, pageSize: number): Promise<SharePage> {
    const { data } = await request.get<Biz<RawSharePage>>('/shares', {
      params: { page, page_size: pageSize },
    })
    const r = data.data
    return {
      total: r.total,
      page: r.page,
      pageSize: r.page_size,
      items: (r.items ?? []).map(toItem),
    }
  },
  async cancel(id: number): Promise<void> {
    await request.delete<Biz<null>>(`/shares/${String(id)}`)
  },
  // S3 公开访问：未登录也可调；失败由 request 统一弹窗“链接无效”
  async getInfo(code: string, password?: string): Promise<ShareInfo> {
    const { data } = await request.get<Biz<RawShareInfo>>(`/s/${code}`, {
      // /s/:code 不在 /api/v1 下，需走绝对路径绕过 baseURL 拼接
      baseURL: '/',
      params: password ? { password } : undefined,
    })
    const r = data.data
    return {
      shareCode: r.share_code,
      fileName: r.file_name,
      fileSize: r.file_size,
      mimeType: r.mime_type,
      expireAt: r.expire_at ?? null,
    }
  },
  // S3 公开下载：blob 落盘，文件名由后端 Content-Disposition 决定时仍用 info 回退
  // S3-fix：错误体也是 Blob，由 request 拦截器 readErrorMessage 解析后统一弹窗，此处不重复弹窗
  async download(code: string, password?: string): Promise<Blob> {
    const { data } = await request.get<Blob>(`/shares/${code}/download`, {
      params: password ? { password } : undefined,
      responseType: 'blob',
    })
    return data
  },
}
