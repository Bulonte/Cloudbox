<script setup lang="ts">
import { useRouter } from 'vue-router'

const router = useRouter()

function goHome(): void {
  router.push('/files')
}
</script>

<template>
  <div class="not-found">
    <div class="card">
      <h1>404</h1>
      <p>页面不存在，可能已被移动或删除</p>
      <el-button
        type="primary"
        @click="goHome"
      >
        回文件页
      </el-button>
    </div>
  </div>
</template>

<style scoped>
.not-found {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 60vh;
  padding: 64px 24px;
  background: var(--cb-surface);
}

.card {
  padding: 48px 64px;
  text-align: center;
  background: var(--cb-canvas);
  border: 1px solid var(--cb-hairline);
  border-radius: var(--cb-radius-lg);
}

.card h1 {
  margin: 0 0 8px;
  color: var(--cb-charcoal);
  font-size: 48px;
}

.card p {
  margin: 0 0 24px;
  color: var(--cb-slate);
  font-size: 14px;
}
</style>
