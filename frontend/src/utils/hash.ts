import HashWorker from '@/workers/hash.worker?worker'

// S1 文件哈希：Web Worker 内算 SHA256，避免大文件读取阻塞主线程
// 环境约束：crypto.subtle 仅在 secure context（HTTPS/localhost）可用，
// 答辩机若走内网 IP 需降级——本模块直接抛明确错误，由调用方提示用户。

/** 当前环境是否支持 crypto.subtle（secure context 检测） */
export function canUseSubtle(): boolean {
  return typeof crypto !== 'undefined' && typeof crypto.subtle !== 'undefined'
}

/** Worker 内部把 ArrayBuffer 转 hex 的同款实现（主线程备用校验用） */
export function bytesToHex(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer)
  let out = ''
  for (let i = 0; i < bytes.length; i += 1) {
    out += bytes[i].toString(16).padStart(2, '0')
  }
  return out
}

/**
 * 计算文件 SHA256（小写 hex，64 位）。
 * 走 Web Worker；Worker 不可用或环境不支持时抛错，由调用方给出用户可读提示。
 */
export function computeFileHash(file: File): Promise<string> {
  if (!canUseSubtle()) {
    return Promise.reject(new Error('当前环境不支持安全哈希计算，请用 HTTPS 或 localhost 访问'))
  }
  return new Promise<string>((resolve, reject) => {
    let worker: Worker
    try {
      worker = new HashWorker()
    } catch {
      reject(new Error('无法启动哈希计算线程'))
      return
    }
    const cleanup = (): void => {
      worker.terminate()
    }
    worker.onmessage = (e: MessageEvent<{ hash?: string; error?: string }>): void => {
      cleanup()
      if (e.data.hash) {
        resolve(e.data.hash)
        return
      }
      reject(new Error(e.data.error ?? '计算文件哈希失败'))
    }
    worker.onerror = (): void => {
      cleanup()
      reject(new Error('计算文件哈希失败'))
    }
    worker.postMessage({ file })
  })
}
