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

interface BizBody {
  message?: string
  code?: number
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
  (err: AxiosError<BizBody>) => {
    const status = err.response?.status
    if (status === 401) {
      // 未授权：清 Token 并跳登录，由路由守卫接管
      setToken('')
      window.location.hash = '#/login'
      return Promise.reject(err)
    }
    // 统一错误提示，禁止各组件重复弹窗；登录失败文案以后端为准，不区分原因
    ElMessage.error(err.response?.data?.message ?? '请求失败')
    return Promise.reject(err)
  },
)

export default request
