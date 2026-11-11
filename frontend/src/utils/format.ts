// S4 文件大小格式化：纯函数可单测，1024 进制保留 1 位小数去零
export function formatSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return '0 B'
  }
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = bytes
  let u = 0
  while (v >= 1024 && u < units.length - 1) {
    v /= 1024
    u += 1
  }
  const text = v >= 100 || Number.isInteger(v) ? String(Math.round(v)) : String(Math.round(v * 10) / 10)
  return `${text} ${units[u] ?? 'B'}`
}

// S4 文件名校验（与后端 service/file.go validFileName 同口径）：
// trim 非空、按 rune ≤255、禁 / \ 与空字符；前端只做体验拦截，服务端为准
export function validFileName(v: string): boolean {
  const n = v.trim()
  if (n === '') {
    return false
  }
  if ([...n].length > 255) {
    return false
  }
  // eslint-disable-next-line no-control-regex
  return !/[/\\\x00]/.test(n)
}
