<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useFileStore } from '@/store/file'
import { CHUNK_SIZE, isSimpleSizeOk } from '@/components/upload'

// S1 上传组件：≤2MB 走简单上传，>2MB 自动走分片链路（Worker 算 SHA256 + 并发传片）
// S2 秒传：两条路径先预检；hit 秒传零写请求；exists_global 经确认框，取消零写请求
// S4 断点续传：>2MB 走 resumeChunked（progress 查进度只补 missing）；中断后保留 resumable 供“继续上传”
// 扩展名/内容嗅探留给服务端，前端只卡体积；失败由 request 统一弹窗
const store = useFileStore()
const inputRef = ref<HTMLInputElement | null>(null)
// S4：保留中断时的 File 对象引用，input 清空前先存档，成功后丢弃
let pendingFile: File | null = null

function openPicker(): void {
  inputRef.value?.click()
}

function phaseText(): string {
  if (store.phase === 'hashing') {
    return '正在计算文件哈希…'
  }
  if (store.phase === 'merging') {
    return '正在合并文件…'
  }
  if (store.skippedChunks > 0) {
    return `已跳过 ${store.skippedChunks} 片，正在补传剩余分片…`
  }
  return '正在上传分片…'
}

// S2 exists_global 确认：用户点取消返回 false，store 零写请求直接返回（不抛错，避免双弹窗）
async function onConflict(): Promise<boolean> {
  try {
    await ElMessageBox.confirm(
      '该文件已存在于云盘中（可能由他人上传），继续上传可能失败。是否仍要上传？',
      '文件重复',
      { type: 'warning', confirmButtonText: '仍然上传', cancelButtonText: '取消' },
    )
    return true
  } catch {
    return false
  }
}

async function onChange(e: Event): Promise<void> {
  const input = e.target as HTMLInputElement
  const f = input.files?.[0]
  if (!f) {
    return
  }
  // S4：先存档 File 引用（input 清空后仍可“继续上传”复用同一对象）
  if (!isSimpleSizeOk(f.size)) {
    pendingFile = f
  }
  try {
    const outcome = isSimpleSizeOk(f.size)
      ? await store.uploadOne(f, onConflict)
      : await store.uploadChunked(f, CHUNK_SIZE, onConflict)
    if (outcome === 'cancelled') {
      pendingFile = null
      return
    }
    pendingFile = null
    if (outcome === 'instant') {
      ElMessage.success('秒传完成')
      return
    }
    ElMessage.success(store.skippedChunks > 0 ? `续传完成，已跳过 ${store.skippedChunks} 片` : '上传成功')
  } catch {
    // 失败由 request 统一弹窗；保留 pendingFile + store.resumable 供“继续上传”
  } finally {
    input.value = ''
  }
}

// S4 继续上传：复用中断时的同一 File 对象重入；简单上传无续传，不保留
async function onResume(): Promise<void> {
  const f = pendingFile
  if (!f || store.uploading) {
    return
  }
  try {
    const outcome = await store.resumeLast()
    if (outcome === 'cancelled') {
      return
    }
    pendingFile = null
    ElMessage.success(store.skippedChunks > 0 ? `续传完成，已跳过 ${store.skippedChunks} 片` : '上传成功')
  } catch {
    // 仍失败则继续保留，可再次点击重试
  }
}
</script>

<template>
  <div class="uploader">
    <input
      ref="inputRef"
      type="file"
      class="hidden"
      @change="onChange"
    >
    <el-button
      type="primary"
      :loading="store.uploading"
      @click="openPicker"
    >
      上传文件
    </el-button>
    <div
      v-if="store.uploading"
      class="progress-wrap"
    >
      <el-progress
        :percentage="store.progress"
        :show-text="false"
        class="progress"
      />
      <span class="phase-text">{{ phaseText() }}</span>
    </div>
    <el-button
      v-if="!store.uploading && store.resumable"
      type="warning"
      plain
      @click="onResume"
    >
      继续上传（已传 {{ store.resumable.uploaded }}/{{ store.resumable.total }}）
    </el-button>
  </div>
</template>

<style scoped>
.uploader {
  display: flex;
  align-items: center;
  gap: 12px;
}

.hidden {
  display: none;
}

.progress-wrap {
  display: flex;
  flex-direction: column;
  gap: 4px;
  width: 200px;
}

.progress {
  width: 200px;
}

.phase-text {
  color: var(--cb-slate);
  font-size: 12px;
}
</style>
