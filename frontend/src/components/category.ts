// S4 分类徽标映射：9 大类轮换 3 套粉彩（只用 tokens.css 现有变量，不自创色）
// 后端实际只返 category（无 confidence），空串前端显示“待确认”
export type TagKind = 'purple' | 'orange' | 'green' | 'pending'

/** 9 大类 → 粉彩种类；空/未知 → pending（待确认灰） */
export function categoryKind(c: string): TagKind {
  switch (c) {
    case '合同':
    case '图片':
    case '文档':
      return 'purple'
    case '简历':
    case '视频':
    case '压缩包':
      return 'orange'
    case '报表':
    case '音频':
    case '其他':
      return 'green'
    default:
      return 'pending'
  }
}

/** 展示文案：空 category 显示“待确认” */
export function categoryLabel(c: string): string {
  return c === '' ? '待确认' : c
}
