// 注册表单校验（与后端 service/user.go 同口径，前端只做体验优化，服务端为准）
export function validUsername(v: string): boolean {
  return /^[A-Za-z0-9_]{3,32}$/.test(v)
}

export function validPassword(v: string): boolean {
  return /^(?=.*[A-Za-z])(?=.*\d).{8,}$/.test(v)
}
