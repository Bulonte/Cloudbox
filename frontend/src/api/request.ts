import axios, { type AxiosError } from 'axios'
import { ElMessage } from 'element-plus'

// Token 双存：内存 + sessionStorage，禁止 localStorage（规范 §2.3.5 + §5.1.3）
// 安全：禁止在此打印密码 / Token / 分享码

const TOKEN_KEY = 'cb_token'

let memToken: string = sessionStorage.getItem(TOKEN_KEY) ?? ''

/** 保存 Token（内存 + sessionStorage 双写） */
export function setToken(t: string): void {
  memToken = t
  if (t) {
    sessionStorage.setItem(TOKEN_KEY, t)
  } else {
    sessionStorage.removeItem(TOKEN_KEY)
  }
}

/** 读取 Token（只读内存，避免反复读 storage） */
export function getToken(): string {
  return memToken
}

const REFRESH_KEY = 'cb_refresh'

let memRefresh: string = sessionStorage.getItem(REFRESH_KEY) ?? ''

/** 保存 RefreshToken（内存 + sessionStorage 双写，禁 localStorage） */
export function setRefresh(t: string): void {
  memRefresh = t
  if (t) {
    sessionStorage.setItem(REFRESH_KEY, t)
  } else {
    sessionStorage.removeItem(REFRESH_KEY)
  }
}

/** 读取 RefreshToken（只读内存） */
export function getRefresh(): string {
  return memRefresh
}

interface BizBody {
  message?: string
  code?: number
}

/** 读后端错误文案：JSON 直取 message；blob（公开下载 responseType:'blob' 时错误体也是 Blob）先转文本再解析；兜底“请求失败” */
// S3-fix：此前 `err.response?.data?.message` 对 Blob 恒为 undefined，用户只能看到通用“请求失败”；
// Blob 可重复读（text() 不消费），此处 slice(0, 8KB) 限读防大包，解析失败落兜底。
// 注：不用 `instanceof Blob`（jsdom 下跨 realm 会失效），用鸭子类型判 blob-like。
// 另：jsdom 的 Blob 无 `text()`（仅浏览器/Node18+ 有），读文本走 text→arrayBuffer→FileReader 三级降级。
function isBlobLike(data: unknown): data is Blob {
  if (typeof data !== 'object' || data === null) {
    return false
  }
  const b = data as { size?: unknown; type?: unknown; slice?: unknown }
  return typeof b.size === 'number' && typeof b.slice === 'function'
}

async function blobToText(b: Blob): Promise<string> {
  const anyBlob = b as unknown as {
    text?: () => Promise<string>
    arrayBuffer?: () => Promise<ArrayBuffer>
    slice: (s: number, e: number) => unknown
  }
  const part = (b.slice(0, 8192) ?? b) as Blob
  const anyPart = part as unknown as {
    text?: () => Promise<string>
    arrayBuffer?: () => Promise<ArrayBuffer>
  }
  if (typeof anyPart.text === 'function') {
    return await anyPart.text()
  }
  if (typeof anyPart.arrayBuffer === 'function') {
    const buf = await anyPart.arrayBuffer()
    return new TextDecoder().decode(buf)
  }
  if (typeof FileReader !== 'undefined') {
    return await new Promise<string>((resolve, reject) => {
      const fr = new FileReader()
      fr.onload = (): void => resolve(typeof fr.result === 'string' ? fr.result : '')
      fr.onerror = (): void => reject(fr.error ?? new Error('read blob failed'))
      fr.readAsText(part)
    })
  }
  void anyBlob
  return ''
}

export async function readErrorMessage(data: unknown): Promise<string> {
  if (isBlobLike(data)) {
    try {
      const text = await blobToText(data)
      const body = JSON.parse(text) as BizBody
      if (typeof body.message === 'string' && body.message !== '') {
        return body.message
      }
    } catch {
      // 解析失败落兜底，不抛错
    }
    return '请求失败'
  }
  if (typeof data === 'string') {
    try {
      const body = JSON.parse(data) as BizBody
      if (typeof body.message === 'string' && body.message !== '') {
        return body.message
      }
    } catch {
      // 纯文本错误体直接透出（非空则用）
      if (data !== '') {
        return data
      }
    }
    return '请求失败'
  }
  const msg = (data as BizBody | null | undefined)?.message
  return msg ?? '请求失败'
}

const request = axios.create({
  baseURL: '/api/v1',
  timeout: 15000,
})

request.interceptors.request.use((cfg) => {
  const t = getToken()
  if (t) {
    cfg.headers.Authorization = `Bearer ${t}`
  }
  cfg.headers['X-Request-ID'] = crypto.randomUUID()
  return cfg
})

request.interceptors.response.use(
  (resp) => resp,
  async (err: AxiosError) => {
    const msg = await readErrorMessage(err.response?.data)
    // 统一错误提示，禁止各组件重复弹窗；登录失败文案以后端为准，不区分原因
    // 401 也要上屏（此前提前 return 把提示吞了，登录输错密码无反馈），再清 Token 跳登录
    ElMessage.error(msg)
    if (err.response?.status === 401) {
      // 未授权：清 Token 并跳登录，由路由守卫接管
      setToken('')
      if (!window.location.hash.startsWith('#/login')) {
        window.location.hash = '#/login'
      }
    }
    return Promise.reject(err)
  },
)

export default request
