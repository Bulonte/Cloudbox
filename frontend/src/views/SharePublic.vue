<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { shareApi, type ShareInfo } from '@/api/share'
import { formatSize } from '@/utils/format'

// S3 公开分享页（免登录）：/s/:code；密码在页内输入；失败统一“链接无效”
const route = useRoute()
const code = String(route.params.code ?? '')
const password = ref('')
const loading = ref(false)
const info = ref<ShareInfo | null>(null)
const downloading = ref(false)

async function load(pw?: string): Promise<void> {
  loading.value = true
  try {
    info.value = await shareApi.getInfo(code, pw)
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  load().catch(() => undefined)
})

async function submitPassword(): Promise<void> {
  await load(password.value.trim() || undefined).catch(() => undefined)
}

async function download(): Promise<void> {
  if (!info.value) {
    return
  }
  downloading.value = true
  try {
    const blob = await shareApi.download(code, password.value.trim() || undefined)
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = info.value.fileName
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
    ElMessage.success('下载已开始')
  } catch {
    // 失败由 request 拦截器统一弹窗（blob 错误体已解析），此处吞错防未处理 rejection
  } finally {
    downloading.value = false
  }
}
</script>

<template>
  <div class="public">
    <el-card
      v-loading="loading"
      class="public-card"
    >
      <template #header>
        <span>分享文件</span>
      </template>
      <div v-if="info">
        <p class="name">
          {{ info.fileName }}
        </p>
        <p class="meta">
          {{ formatSize(info.fileSize) }} · {{ info.mimeType || '未知类型' }}
        </p>
        <p
          v-if="info.expireAt"
          class="meta"
        >
          有效期至 {{ info.expireAt }}
        </p>
        <el-button
          type="primary"
          :loading="downloading"
          @click="download"
        >
          下载
        </el-button>
      </div>
      <div v-else>
        <p class="tip">
          该链接可能需要提取密码，请输入后查看
        </p>
        <el-input
          v-model="password"
          placeholder="请输入提取密码（无密码可留空重试）"
          maxlength="64"
          clearable
          show-password
        />
        <el-button
          type="primary"
          class="retry"
          @click="submitPassword"
        >
          查看分享
        </el-button>
      </div>
    </el-card>
  </div>
</template>

<style scoped>
.public {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 60vh;
  padding: 64px 24px;
  background: var(--cb-surface);
}

.public-card {
  width: 440px;
}

.name {
  margin: 0 0 8px;
  color: var(--cb-charcoal);
  font-size: 16px;
  font-weight: 600;
  word-break: break-all;
}

.meta {
  margin: 0 0 8px;
  color: var(--cb-slate);
  font-size: 13px;
}

.tip {
  margin: 0 0 12px;
  color: var(--cb-slate);
  font-size: 13px;
}

.retry {
  width: 100%;
  margin-top: 12px;
}
</style>
