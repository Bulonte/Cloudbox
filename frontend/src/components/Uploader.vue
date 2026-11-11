<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useFileStore } from '@/store/file'
import { isSimpleSizeOk } from '@/components/upload'

// S5 简单上传：原生 input + axios onUploadProgress，不用 el-upload 内置上传；
// 扩展名/内容嗅探留给服务端，前端只卡 ≤2MB；失败由 request 统一弹窗
const store = useFileStore()
const inputRef = ref<HTMLInputElement | null>(null)

function openPicker(): void {
  inputRef.value?.click()
}

async function onChange(e: Event): Promise<void> {
  const input = e.target as HTMLInputElement
  const f = input.files?.[0]
  if (!f) {
    return
  }
  if (!isSimpleSizeOk(f.size)) {
    ElMessage.error('文件需 ≤2MB，请走 W8 分片上传')
    input.value = ''
    return
  }
  try {
    await store.uploadOne(f)
    ElMessage.success('上传成功')
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
    <el-progress
      v-if="store.uploading"
      :percentage="store.progress"
      :show-text="false"
      class="progress"
    />
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

.progress {
  width: 160px;
}
</style>
