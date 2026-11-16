<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { shareApi, toPublicLink, type CreatedShare } from '@/api/share'
import type { FileItem } from '@/api/file'

// S3 创建分享弹窗：纯展示 + 调 shareApi.create；成功回显可复制链接
const props = defineProps<{
  visible: boolean
  file: FileItem | null
}>()

const emit = defineEmits<{
  (e: 'update:visible', v: boolean): void
  (e: 'created'): void
}>()

const password = ref('')
const expireDays = ref<number | null>(7)
const maxDownloads = ref<number | null>(null)
const submitting = ref(false)
const created = ref<CreatedShare | null>(null)

const open = computed({
  get: () => props.visible,
  set: (v: boolean) => emit('update:visible', v),
})

watch(
  () => props.visible,
  (v) => {
    if (v) {
      password.value = ''
      expireDays.value = 7
      maxDownloads.value = null
      created.value = null
    }
  },
)

const publicLink = computed(() => (created.value ? toPublicLink(created.value.shareUrl) : ''))

function expireAtText(): string {
  if (expireDays.value === null) {
    return ''
  }
  const d = new Date(Date.now() + expireDays.value * 86400000)
  const p = (n: number): string => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

async function submit(): Promise<void> {
  if (!props.file) {
    return
  }
  if (maxDownloads.value !== null && (!Number.isInteger(maxDownloads.value) || maxDownloads.value <= 0)) {
    ElMessage.error('下载次数须为正整数')
    return
  }
  submitting.value = true
  try {
    const res = await shareApi.create({
      fileId: props.file.id,
      password: password.value.trim(),
      expireAt: expireAtText(),
      maxDownloads: maxDownloads.value ?? undefined,
    })
    created.value = res
    ElMessage.success('分享创建成功')
    emit('created')
  } finally {
    submitting.value = false
  }
}

async function copyLink(): Promise<void> {
  if (!publicLink.value) {
    return
  }
  try {
    await navigator.clipboard.writeText(publicLink.value)
    ElMessage.success('链接已复制')
  } catch {
    ElMessage.error('复制失败，请手动复制')
  }
}

function close(): void {
  emit('update:visible', false)
}
</script>

<template>
  <el-dialog
    v-model="open"
    :title="created ? '分享已创建' : `分享「${file?.fileName ?? ''}」`"
    width="440px"
    @closed="close"
  >
    <div v-if="!created">
      <el-form label-position="top">
        <el-form-item label="提取密码（留空为公开）">
          <el-input
            v-model="password"
            placeholder="可选，不填则公开访问"
            maxlength="64"
            clearable
            show-password
          />
        </el-form-item>
        <el-form-item label="有效期">
          <el-select v-model="expireDays">
            <el-option
              :value="1"
              label="1 天"
            />
            <el-option
              :value="7"
              label="7 天（默认）"
            />
            <el-option
              :value="30"
              label="30 天（最长）"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="下载次数上限（留空为不限）">
          <el-input-number
            v-model="maxDownloads"
            :min="1"
            placeholder="不限"
          />
        </el-form-item>
      </el-form>
    </div>
    <div v-else>
      <el-input
        :model-value="publicLink"
        readonly
      >
        <template #append>
          <el-button @click="copyLink">
            复制
          </el-button>
        </template>
      </el-input>
      <p class="hint">
        有效期：{{ created.expireAt ?? '不限' }}；下载上限：{{ created.maxDownloads ?? '不限' }}
      </p>
    </div>
    <template #footer>
      <el-button @click="close">
        {{ created ? '关闭' : '取消' }}
      </el-button>
      <el-button
        v-if="!created"
        type="primary"
        :loading="submitting"
        @click="submit"
      >
        创建
      </el-button>
      <el-button
        v-else
        type="primary"
        @click="copyLink"
      >
        复制链接
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.hint {
  margin: 12px 0 0;
  color: var(--cb-slate);
  font-size: 13px;
}
</style>
