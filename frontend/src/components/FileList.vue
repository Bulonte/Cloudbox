<script setup lang="ts">
import { formatSize } from '@/utils/format'
import type { FileItem } from '@/api/file'
import CategoryTag from '@/components/CategoryTag.vue'

// S4 文件列表：纯展示，不调 API；文件名插值展示（禁 v-html），超长 CSS 省略 + title 悬停
defineProps<{
  items: FileItem[]
  loading: boolean
}>()

const emit = defineEmits<{
  (e: 'enter', f: FileItem): void
  (e: 'rename', f: FileItem): void
  (e: 'remove', f: FileItem): void
}>()

function sizeText(row: FileItem): string {
  if (row.isFolder === 1) {
    return '-'
  }
  return formatSize(row.fileSize)
}
</script>

<template>
  <el-table
    v-loading="loading"
    :data="items"
    class="file-table"
  >
    <el-table-column
      prop="fileName"
      label="名称"
      min-width="220"
      show-overflow-tooltip
    >
      <template #default="{ row }: { row: FileItem }">
        <span
          v-if="row.isFolder === 1"
          class="folder-name"
          :title="row.fileName"
          @click="emit('enter', row)"
        >
          {{ row.fileName }}
        </span>
        <span
          v-else
          class="file-name"
          :title="row.fileName"
        >
          {{ row.fileName }}
        </span>
      </template>
    </el-table-column>
    <el-table-column
      label="分类"
      width="110"
    >
      <template #default="{ row }: { row: FileItem }">
        <CategoryTag :category="row.isFolder === 1 ? '' : row.category" />
      </template>
    </el-table-column>
    <el-table-column
      label="大小"
      width="110"
    >
      <template #default="{ row }: { row: FileItem }">
        {{ sizeText(row) }}
      </template>
    </el-table-column>
    <el-table-column
      prop="updatedAt"
      label="更新时间"
      width="170"
    />
    <el-table-column
      label="操作"
      width="180"
      fixed="right"
    >
      <template #default="{ row }: { row: FileItem }">
        <el-button
          v-if="row.isFolder === 1"
          link
          @click="emit('enter', row)"
        >
          进入
        </el-button>
        <el-button
          link
          @click="emit('rename', row)"
        >
          改名
        </el-button>
        <el-button
          link
          type="danger"
          @click="emit('remove', row)"
        >
          删除
        </el-button>
      </template>
    </el-table-column>
    <template #empty>
      <el-empty description="空目录" />
    </template>
  </el-table>
</template>

<style scoped>
.file-table {
  width: 100%;
}

.folder-name {
  color: var(--cb-link);
  cursor: pointer;
}

.file-name {
  color: var(--cb-charcoal);
}
</style>
