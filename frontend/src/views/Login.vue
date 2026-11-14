<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import type { FormInstance, FormRules } from 'element-plus'
import { useUserStore } from '@/store/user'

const router = useRouter()
const store = useUserStore()
const formRef = ref<FormInstance>()
const form = reactive({ login: '', password: '' })
const loading = ref(false)

// 与后端同口径：login 非空即可，不预判用户名/邮箱；密码只判非空，复杂度由后端判
const rules: FormRules = {
  login: [{ required: true, message: '请输入用户名或邮箱', trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }],
}

async function submit(f: FormInstance | undefined): Promise<void> {
  if (!f) return
  await f.validate()
  loading.value = true
  try {
    await store.login(form.login.trim(), form.password)
    await router.push('/files')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="auth-page">
    <el-card class="auth-card">
      <h2>登录 CloudBox</h2>
      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        label-position="top"
      >
        <el-form-item
          label="用户名或邮箱"
          prop="login"
        >
          <el-input
            v-model="form.login"
            autocomplete="username"
            maxlength="64"
            clearable
          />
        </el-form-item>
        <el-form-item
          label="密码"
          prop="password"
        >
          <el-input
            v-model="form.password"
            type="password"
            show-password
            autocomplete="current-password"
          />
        </el-form-item>
        <el-button
          type="primary"
          class="submit-btn"
          :loading="loading"
          @click="submit(formRef)"
        >
          登录
        </el-button>
        <router-link
          class="link"
          to="/register"
        >
          没有账号？去注册
        </router-link>
      </el-form>
    </el-card>
  </div>
</template>

<style scoped>
.auth-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--cb-surface);
}
.auth-card {
  width: 400px;
  border-radius: var(--cb-radius-lg);
}
.auth-card h2 {
  margin: 0 0 20px;
  color: var(--cb-ink);
  font-size: 20px;
  font-weight: 600;
}
.submit-btn {
  width: 100%;
  margin-top: 8px;
}
.link {
  display: inline-block;
  margin-top: 12px;
  color: var(--cb-link);
  font-size: 14px;
}
</style>
