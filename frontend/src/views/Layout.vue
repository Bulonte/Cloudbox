<script setup lang="ts">
import { onMounted } from 'vue'
import Sidebar from '@/components/Sidebar.vue'
import Topbar from '@/components/Topbar.vue'
import { getToken } from '@/api/request'
import { useUserStore } from '@/store/user'

// S3 主框架：只做 aside + header + main 排版，不写业务逻辑
const store = useUserStore()

onMounted(() => {
  // 刷新后 store.user 为空但 token 还在，补拉一次，避免顶栏名字空白
  if (getToken() && !store.user) {
    store.fetchMe().catch(() => undefined)
  }
})
</script>

<template>
  <el-container class="layout">
    <el-aside
      width="220px"
      class="layout-aside"
    >
      <Sidebar />
    </el-aside>
    <el-container>
      <el-header
        height="56px"
        class="layout-header"
      >
        <Topbar />
      </el-header>
      <el-main class="layout-main">
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>

<style scoped>
.layout {
  min-height: 100vh;
  background: var(--cb-surface);
}

.layout-aside {
  background: var(--cb-canvas);
  border-right: 1px solid var(--cb-hairline);
}

.layout-header {
  padding: 0 20px;
  background: var(--cb-canvas);
  border-bottom: 1px solid var(--cb-hairline);
}

.layout-main {
  padding: 20px;
}
</style>
