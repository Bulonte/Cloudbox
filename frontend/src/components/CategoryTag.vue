<script setup lang="ts">
import { computed } from 'vue'
import { categoryKind, categoryLabel } from '@/components/category'

// S4 分类徽标：纯展示，9 大类轮换粉彩，空 category 显示“待确认”
const props = defineProps<{
  category: string
}>()

const kind = computed(() => categoryKind(props.category))
const label = computed(() => categoryLabel(props.category))
</script>

<template>
  <span
    class="cat-tag"
    :class="`cat-${kind}`"
  >
    {{ label }}
  </span>
</template>

<style scoped>
.cat-tag {
  display: inline-block;
  padding: 2px 10px;
  font-size: 13px;
  font-weight: 600;
  line-height: 20px;
  border-radius: var(--cb-radius-sm);
  white-space: nowrap;
}

.cat-purple {
  color: var(--cb-tag-purple-fg);
  background: var(--cb-tag-purple-bg);
}

.cat-orange {
  color: var(--cb-tag-orange-fg);
  background: var(--cb-tag-orange-bg);
}

.cat-green {
  color: var(--cb-tag-green-fg);
  background: var(--cb-tag-green-bg);
}

.cat-pending {
  color: var(--cb-slate);
  background: var(--cb-surface);
  border: 1px solid var(--cb-hairline);
}
</style>
