<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useFileStore } from '@/store/file'
import { useUserStore } from '@/store/user'

// S3 顶栏：只做展示 + 登出，改密页 W8 再做
const router = useRouter()
const store = useUserStore()

const displayName = computed(() => store.user?.username ?? '加载中')

async function onLogout(): Promise<void> {
  // store.logout 已先清本地（后端 500 也不回滚），此处只负责跳转；
  // S5 收官：顺手清文件 store，避免换号残留旧目录
  await store.logout()
  useFileStore().reset()
  await router.push('/login')
}
</script>

<template>
  <div class="topbar">
    <span class="crumb">文件</span>
    <div class="user">
      <span class="welcome">{{ displayName }}</span>
      <el-button
        link
        @click="onLogout"
      >
        退出
      </el-button>
    </div>
  </div>
</template>

<style scoped>
.topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 100%;
}

.crumb {
  color: var(--cb-charcoal);
  font-size: 14px;
  font-weight: 600;
}

.user {
  display: flex;
  align-items: center;
  gap: 12px;
}

.welcome {
  color: var(--cb-slate);
  font-size: 13px;
}
</style>
