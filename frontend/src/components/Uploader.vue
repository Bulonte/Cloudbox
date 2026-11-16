<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useFileStore } from '@/store/file'
import { CHUNK_SIZE, isSimpleSizeOk } from '@/components/upload'

// S1 上传组件：≤2MB 走简单上传，>2MB 自动走分片链路（Worker 算 SHA256 + 并发传片）
// S2 秒传：两条路径先预检；hit 秒传零写请求；exists_global 经确认框，取消零写请求
// 扩展名/内容嗅探留给服务端，前端只卡体积；失败由 request 统一弹窗
const store = useFileStore()
const inputRef = ref<HTMLInputElement | null>(null)

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
  try {
    const outcome = isSimpleSizeOk(f.size)
      ? await store.uploadOne(f, onConflict)
      : await store.uploadChunked(f, CHUNK_SIZE, onConflict)
    if (outcome === 'cancelled') {
      return
    }
    ElMessage.success(outcome === 'instant' ? '秒传完成' : '上传成功')
  } finally {
    input.value = ''
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
