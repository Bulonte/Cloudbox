<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { DataAnalysis, Document, Share } from '@element-plus/icons-vue'
import { NAV_ITEMS, resolveActive } from '@/components/nav'

// S3 侧边栏：只渲染 NAV_ITEMS，选中态由路由推导，不另存状态
const route = useRoute()
const active = computed(() => resolveActive(route.path))

const ICONS = {
  '/files': Document,
  '/shares': Share,
  '/dashboard': DataAnalysis,
} as const
</script>

<template>
  <div class="sidebar">
    <div class="brand">
      CloudBox
    </div>
    <el-menu
      :default-active="active"
      router
      class="sidebar-menu"
    >
      <el-menu-item
        v-for="item in NAV_ITEMS"
        :key="item.index"
        :index="item.index"
        :disabled="item.disabled"
      >
        <el-icon>
          <component :is="ICONS[item.index as keyof typeof ICONS]" />
        </el-icon>
        <span>{{ item.label }}</span>
      </el-menu-item>
    </el-menu>
  </div>
</template>

<style scoped>
.sidebar {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.brand {
  padding: 16px 20px;
  border-bottom: 1px solid var(--cb-hairline);
  color: var(--cb-charcoal);
  font-size: 16px;
  font-weight: 700;
}

.sidebar-menu {
  flex: 1;
  border-right: none;
}
</style>
