<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { fileApi, type FileItem } from '@/api/file'
import { useFileStore } from '@/store/file'
import { validFileName } from '@/utils/format'
import FileList from '@/components/FileList.vue'
import Uploader from '@/components/Uploader.vue'

// S4 文件浏览器：容器管数据 + 弹窗，列表渲染下沉 FileList；成功刷列表，失败由 request 统一弹窗
const store = useFileStore()

const createVisible = ref(false)
const renameVisible = ref(false)
const folderName = ref('')
const renameName = ref('')
const renameTarget = ref<FileItem | null>(null)
const submitting = ref(false)

onMounted(() => {
  store.fetchList().catch(() => undefined)
})

function openCreate(): void {
  folderName.value = ''
  createVisible.value = true
}

function openRename(f: FileItem): void {
  renameTarget.value = f
  renameName.value = f.fileName
  renameVisible.value = true
}

async function submitCreate(): Promise<void> {
  const name = folderName.value.trim()
  if (!validFileName(name)) {
    ElMessage.error('文件名需 1-255 字符且不含 / \\')
    return
  }
  submitting.value = true
  try {
    await fileApi.createFolder({ fileName: name, parentId: store.parentId })
    createVisible.value = false
    ElMessage.success('创建成功')
    await store.refresh()
  } finally {
    submitting.value = false
  }
}

async function submitRename(): Promise<void> {
  if (!renameTarget.value) {
    return
  }
  const name = renameName.value.trim()
  if (!validFileName(name)) {
    ElMessage.error('文件名需 1-255 字符且不含 / \\')
    return
  }
  submitting.value = true
  try {
    await fileApi.update(renameTarget.value.id, { fileName: name })
    renameVisible.value = false
    ElMessage.success('改名成功')
    await store.refresh()
  } finally {
    submitting.value = false
  }
}

async function confirmRemove(f: FileItem): Promise<void> {
  try {
    await ElMessageBox.confirm(`确定删除「${f.fileName}」吗？`, '删除', { type: 'warning' })
  } catch {
    return
  }
  await fileApi.remove(f.id)
  ElMessage.success('删除成功')
  await store.refresh()
}

async function onPageChange(p: number): Promise<void> {
  store.page = p
  await store.fetchList()
}
</script>

<template>
  <div class="explorer">
    <div class="explorer-bar">
      <el-breadcrumb separator="/">
        <el-breadcrumb-item
          v-for="c in store.crumbs"
          :key="c.id"
        >
          <span
            class="crumb"
            @click="store.goCrumb(c.id)"
          >{{ c.name }}</span>
        </el-breadcrumb-item>
      </el-breadcrumb>
      <div class="explorer-actions">
        <Uploader />
        <el-button
          type="primary"
          @click="openCreate"
        >
          新建文件夹
        </el-button>
      </div>
    </div>

    <FileList
      :items="store.items"
      :loading="store.loading"
      @enter="store.enterFolder"
      @rename="openRename"
      @remove="confirmRemove"
    />

    <div class="explorer-page">
      <el-pagination
        :current-page="store.page"
        :page-size="store.pageSize"
        :total="store.total"
        layout="total, prev, pager, next"
        @current-change="onPageChange"
      />
    </div>

    <el-dialog
      v-model="createVisible"
      title="新建文件夹"
      width="400px"
    >
      <el-input
        v-model="folderName"
        placeholder="请输入文件夹名"
        maxlength="255"
        clearable
      />
      <template #footer>
        <el-button @click="createVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="submitting"
          @click="submitCreate"
        >
          创建
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="renameVisible"
      title="重命名"
      width="400px"
    >
      <el-input
        v-model="renameName"
        placeholder="请输入新名称"
        maxlength="255"
        clearable
      />
      <template #footer>
        <el-button @click="renameVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="submitting"
          @click="submitRename"
        >
          保存
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.explorer {
  padding: 20px;
  background: var(--cb-canvas);
  border: 1px solid var(--cb-hairline);
  border-radius: var(--cb-radius-lg);
}

.explorer-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}

.explorer-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}

.crumb {
  cursor: pointer;
}

.explorer-page {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}
</style>
