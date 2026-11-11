// S3 主框架导航：纯函数可单测，Sidebar.vue 只做渲染
// W8 新增页面时只改 NAV_ITEMS，不动 Sidebar 模板逻辑

export interface NavItem {
  index: string
  label: string
  hint?: string
  disabled?: boolean
}

export const NAV_ITEMS: NavItem[] = [
  { index: '/files', label: '文件' },
  { index: '/shares', label: '分享（W8）', disabled: true },
  { index: '/dashboard', label: '仪表盘（W8）', disabled: true },
]

/** 由 route.path 推导菜单高亮，空路径兜底到 /files */
export function resolveActive(path: string): string {
  const seg = path.split('/')[1]
  return `/${seg || 'files'}`
}
