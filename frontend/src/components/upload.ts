// S5 简单上传预检：纯函数可单测，阈值与 PRD §2.1 冻结口径对齐（≤2MB）
// 扩展名/内容嗅探留给服务端（validator.go），前端只卡体积，防黑白名单漂移

export const MAX_SIMPLE_SIZE = 2 * 1024 * 1024

/** 体积合法：(0, 2MB] 左开右闭，0 字节与超限均拒 */
export function isSimpleSizeOk(size: number): boolean {
  if (!Number.isFinite(size)) {
    return false
  }
  return size > 0 && size <= MAX_SIMPLE_SIZE
}
