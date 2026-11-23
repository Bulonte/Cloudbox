// S1 Web Worker：全文件 SHA256 摘要
// 依赖 crypto.subtle（secure context：HTTPS 或 localhost），失败由主线程兜底提示。
// 禁 any/@ts-ignore；worker 作用域类型见 tsconfig lib DOM。

interface HashRequest {
  file: File
}

interface HashResponse {
  hash?: string
  error?: string
}

/** 字节数组转小写 hex（64 位小写，与后端 validSHA256 大小写均收但存原串） */
function toHex(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer)
  let out = ''
  for (let i = 0; i < bytes.length; i += 1) {
    out += bytes[i].toString(16).padStart(2, '0')
  }
  return out
}

self.onmessage = async (e: MessageEvent<HashRequest>): Promise<void> => {
  try {
    const file = e.data.file
    const buffer = await file.arrayBuffer()
    const digest = await crypto.subtle.digest('SHA-256', buffer)
    const payload: HashResponse = { hash: toHex(digest) }
    self.postMessage(payload)
  } catch (err) {
    const payload: HashResponse = { error: err instanceof Error ? err.message : '计算文件哈希失败' }
    self.postMessage(payload)
  }
}
