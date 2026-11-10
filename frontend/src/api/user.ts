import request from './request'

// 后端 DTO 对齐（backend/internal/api/dto/user.go）：
// snake_case 原样收，camelCase 转换放在 store 层，组件只认 camelCase。

export interface UserInfo {
  id: number
  username: string
  email: string
  role: string
  quotaBytes: number
  usedBytes: number
}

export interface LoginResult {
  accessToken: string
  refreshToken: string
  tokenType: string
  expiresIn: number
}

interface Biz<T> {
  code: number
  message: string
  data: T
}

interface RawLogin {
  access_token: string
  refresh_token: string
  token_type: string
  expires_in: number
}

interface RawUser {
  id: number
  username: string
  email: string
  role: string
  quota_bytes: number
  used_bytes: number
}

function toUser(r: RawUser): UserInfo {
  return {
    id: r.id,
    username: r.username,
    email: r.email,
    role: r.role,
    quotaBytes: r.quota_bytes,
    usedBytes: r.used_bytes,
  }
}

function toLogin(r: RawLogin): LoginResult {
  return {
    accessToken: r.access_token,
    refreshToken: r.refresh_token,
    tokenType: r.token_type,
    expiresIn: r.expires_in,
  }
}

export const userApi = {
  async register(payload: { username: string; email: string; password: string }): Promise<UserInfo> {
    const { data } = await request.post<Biz<RawUser>>('/auth/register', payload)
    return toUser(data.data)
  },
  async login(payload: { login: string; password: string }): Promise<LoginResult> {
    const { data } = await request.post<Biz<RawLogin>>('/auth/login', payload)
    return toLogin(data.data)
  },
  async me(): Promise<UserInfo> {
    const { data } = await request.get<Biz<RawUser>>('/users/me')
    return toUser(data.data)
  },
  async refresh(refreshToken: string): Promise<LoginResult> {
    const { data } = await request.post<Biz<RawLogin>>('/auth/refresh', {
      refresh_token: refreshToken,
    })
    return toLogin(data.data)
  },
  async logout(refreshToken: string): Promise<void> {
    await request.post<Biz<null>>('/auth/logout', { refresh_token: refreshToken })
  },
}
