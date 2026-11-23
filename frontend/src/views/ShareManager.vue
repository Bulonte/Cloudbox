<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { shareApi, toPublicLink, type ShareItem } from '@/api/share'

// S3 分享管理：列表 + 复制链接 + 取消分享；失败由 request 统一弹窗
const items = ref<ShareItem[]>([])
const loading = ref(false)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)

async function fetchList(): Promise<void> {
  loading.value = true
  try {
    const res = await shareApi.list(page.value, pageSize.value)
    items.value = res.items
    total.value = res.total
    page.value = res.page
    pageSize.value = res.pageSize
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  fetchList().catch(() => undefined)
})

async function copyRow(row: ShareItem): Promise<void> {
  try {
    await navigator.clipboard.writeText(toPublicLink(row.shareUrl))
    ElMessage.success('链接已复制')
  } catch {
    ElMessage.error('复制失败，请手动复制')
  }
}

async function confirmCancel(row: ShareItem): Promise<void> {
  try {
    await ElMessageBox.confirm(`确定取消分享「${row.fileName}」吗？`, '取消分享', { type: 'warning' })
  } catch {
    return
  }
  await shareApi.cancel(row.id)
  ElMessage.success('已取消分享')
  await fetchList()
}

function limitText(row: ShareItem): string {
  const max = row.maxDownloads === null ? '不限' : String(row.maxDownloads)
  return `${String(row.downloadCount)} / ${max}`
}

async function onPageChange(p: number): Promise<void> {
  page.value = p
  await fetchList()
}
</script>

<template>
  <div class="shares">
    <el-table
      v-loading="loading"
      :data="items"
      class="shares-table"
    >
      <el-table-column
        prop="fileName"
        label="文件"
        min-width="220"
        show-overflow-tooltip
      />
      <el-table-column
        prop="createdAt"
        label="创建时间"
        width="170"
      />
      <el-table-column
        prop="expireAt"
        label="到期时间"
        width="170"
      >
        <template #default="{ row }: { row: ShareItem }">
          {{ row.expireAt ?? '不限' }}
        </template>
      </el-table-column>
      <el-table-column
        label="下载次数"
        width="120"
      >
        <template #default="{ row }: { row: ShareItem }">
          {{ limitText(row) }}
        </template>
      </el-table-column>
      <el-table-column
        label="操作"
        width="190"
        fixed="right"
      >
        <template #default="{ row }: { row: ShareItem }">
          <el-button
            link
            @click="copyRow(row)"
          >
            复制链接
          </el-button>
          <el-button
            link
            type="danger"
            @click="confirmCancel(row)"
          >
            取消分享
          </el-button>
        </template>
      </el-table-column>
      <template #empty>
        <el-empty description="暂无分享" />
      </template>
    </el-table>

    <div class="shares-page">
      <el-pagination
        :current-page="page"
        :page-size="pageSize"
        :total="total"
        layout="total, prev, pager, next"
        @current-change="onPageChange"
      />
    </div>
  </div>
</template>

<style scoped>
.shares {
  padding: 20px;
  background: var(--cb-canvas);
  border: 1px solid var(--cb-hairline);
  border-radius: var(--cb-radius-lg);
}

.shares-table {
  width: 100%;
}

.shares-page {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
</style>
