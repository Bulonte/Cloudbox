import { ref } from 'vue'
import { defineStore } from 'pinia'
import { fileApi, type FileItem } from '@/api/file'

// S4 文件状态：唯一可信源（items/total/page/parentId/crumbs），选中态等派生状态由组件本地维护
export interface Crumb {
  id: number
  name: string
}

export const useFileStore = defineStore('file', () => {
  const items = ref<FileItem[]>([])
  const total = ref(0)
  const page = ref(1)
  const pageSize = ref(20)
  const parentId = ref(0)
  const loading = ref(false)
  const uploading = ref(false)
  const progress = ref(0)
  const crumbs = ref<Crumb[]>([{ id: 0, name: '根目录' }])

  async function fetchList(): Promise<void> {
    loading.value = true
    try {
      const res = await fileApi.list(parentId.value, page.value, pageSize.value)
      total.value = res.total
      page.value = res.page
      pageSize.value = res.pageSize
      items.value = res.items
      // 越界兜底：删空页时回退一页重拉（后端分页已归一，前端只处理越界）
      if (items.value.length === 0 && total.value > 0 && page.value > 1) {
        page.value -= 1
        const retry = await fileApi.list(parentId.value, page.value, pageSize.value)
        total.value = retry.total
        page.value = retry.page
        pageSize.value = retry.pageSize
        items.value = retry.items
      }
    } finally {
      loading.value = false
    }
  }

  async function enterFolder(f: FileItem): Promise<void> {
    crumbs.value.push({ id: f.id, name: f.fileName })
    parentId.value = f.id
    page.value = 1
    await fetchList()
  }

  async function goCrumb(id: number): Promise<void> {
    const idx = crumbs.value.findIndex((c) => c.id === id)
    if (idx < 0) {
      return
    }
    crumbs.value = crumbs.value.slice(0, idx + 1)
    parentId.value = id
    page.value = 1
    await fetchList()
  }

  async function refresh(): Promise<void> {
    await fetchList()
  }

  // S5 简单上传：进度经回调回写，成功刷列表；失败由 request 统一弹窗，uploading 必回 false
  async function uploadOne(f: File): Promise<void> {
    uploading.value = true
    progress.value = 0
    try {
      await fileApi.upload(f, parentId.value, (p) => {
        progress.value = p
      })
      await fetchList()
    } finally {
      uploading.value = false
    }
  }

  function reset(): void {
    items.value = []
    total.value = 0
    page.value = 1
    pageSize.value = 20
    parentId.value = 0
    uploading.value = false
    progress.value = 0
    crumbs.value = [{ id: 0, name: '根目录' }]
  }

  return { items, total, page, pageSize, parentId, loading, uploading, progress, crumbs, fetchList, enterFolder, goCrumb, refresh, uploadOne, reset }
})
