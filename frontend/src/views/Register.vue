<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import type { FormInstance, FormRules } from 'element-plus'
import { useUserStore } from '@/store/user'
import { validPassword, validUsername } from '@/utils/validators'

const router = useRouter()
const store = useUserStore()
const formRef = ref<FormInstance>()
const form = reactive({ username: '', email: '', password: '', confirm: '' })
const loading = ref(false)

const validateUsername = (_r: unknown, v: string, cb: (e?: Error) => void): void => {
  if (!validUsername(v.trim())) {
    cb(new Error('用户名需 3-32 位字母数字下划线'))
    return
  }
  cb()
}

const validatePassword = (_r: unknown, v: string, cb: (e?: Error) => void): void => {
  if (!validPassword(v)) {
    cb(new Error('密码需 8 位以上且含字母和数字'))
    return
  }
  cb()
}

const validateConfirm = (_r: unknown, v: string, cb: (e?: Error) => void): void => {
  if (v !== form.password) {
    cb(new Error('两次输入的密码不一致'))
    return
  }
  cb()
}

const rules: FormRules = {
  username: [
    { required: true, message: '请输入用户名', trigger: 'blur' },
    { validator: validateUsername, trigger: 'blur' },
  ],
  email: [
    { required: true, message: '请输入邮箱', trigger: 'blur' },
    { type: 'email', message: '邮箱格式不正确', trigger: 'blur' },
  ],
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' },
    { validator: validatePassword, trigger: 'blur' },
  ],
  confirm: [
    { required: true, message: '请再次输入密码', trigger: 'blur' },
    { validator: validateConfirm, trigger: 'blur' },
  ],
}

async function submit(f: FormInstance | undefined): Promise<void> {
  if (!f) return
  await f.validate()
  loading.value = true
  try {
    // register 内部自动登录并跳转前的 me 拉取，成功后直达 /files
    await store.register(form.username.trim(), form.email.trim(), form.password)
    await router.push('/files')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="auth-page">
    <el-card class="auth-card">
      <h2>注册 CloudBox</h2>
      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        label-position="top"
      >
        <el-form-item
          label="用户名"
          prop="username"
        >
          <el-input
            v-model="form.username"
            autocomplete="username"
            maxlength="32"
            clearable
          />
        </el-form-item>
        <el-form-item
          label="邮箱"
          prop="email"
        >
          <el-input
            v-model="form.email"
            autocomplete="email"
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
            autocomplete="new-password"
          />
        </el-form-item>
        <el-form-item
          label="确认密码"
          prop="confirm"
        >
          <el-input
            v-model="form.confirm"
            type="password"
            show-password
            autocomplete="new-password"
          />
        </el-form-item>
        <el-button
          type="primary"
          class="submit-btn"
          :loading="loading"
          @click="submit(formRef)"
        >
          注册并登录
        </el-button>
        <router-link
          class="link"
          to="/login"
        >
          已有账号？去登录
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
