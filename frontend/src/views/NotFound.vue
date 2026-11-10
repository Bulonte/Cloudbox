<script setup lang="ts">
import { useRouter } from 'vue-router'

const router = useRouter()

function goHome(): void {
  router.push('/files')
}
</script>

<template>
  <div class="not-found">
    <h1>404</h1>
    <p>页面不存在</p>
    <el-button
      type="primary"
      @click="goHome"
    >
      回文件页
    </el-button>
  </div>
</template>

<style scoped>
.not-found {
  padding: 64px 24px;
  text-align: center;
  color: var(--cb-slate);
}
</style>
