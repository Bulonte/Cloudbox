<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { smartApi, type SmartStats } from '@/api/smart'
import {
  categoryPieOption,
  duplicateRows,
  formatBytes,
  storageRingOption,
  timelineOption,
  usageRate,
} from '@/utils/dashboard'
import ChartView from '@/components/ChartView.vue'

// S5 仪表盘：4 KPI + 2x2 图表 + 重复明细表；days 7/14/30 切换，空数据占位不断线
const DAYS_OPTIONS = [7, 14, 30]
const days = ref(14)
const loading = ref(false)
const stats = ref<SmartStats | null>(null)

async function fetchStats(): Promise<void> {
  loading.value = true
  try {
    stats.value = await smartApi.stats(days.value)
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  fetchStats().catch(() => undefined)
})

async function onDaysChange(): Promise<void> {
  await fetchStats().catch(() => undefined)
}

const usedText = computed(() => formatBytes(stats.value?.storage.usedBytes ?? 0))
const quotaText = computed(() => formatBytes(stats.value?.storage.quotaBytes ?? 0))
const rateText = computed(() => {
  const s = stats.value?.storage
  return `${(usageRate(s?.usedBytes ?? 0, s?.quotaBytes ?? 0) * 100).toFixed(1)}%`
})
const fileCount = computed(() => stats.value?.storage.fileCount ?? 0)

const ringOption = computed(() => {
  const s = stats.value?.storage
  const base = storageRingOption({
    usedBytes: s?.usedBytes ?? 0,
    quotaBytes: s?.quotaBytes ?? 0,
    fileCount: s?.fileCount ?? 0,
    byType: s?.byType ?? [],
  })
  return {
    tooltip: { trigger: 'item' },
    legend: { bottom: 0 },
    series: [{ ...base.series[0], type: 'pie', radius: ['45%', '70%'] }],
  }
})

const lineOption = computed(() => {
  const base = timelineOption(stats.value?.timeline ?? [])
  return {
    tooltip: { trigger: 'axis' },
    xAxis: { type: 'category', boundaryGap: false, data: base.xAxis.data },
    yAxis: { type: 'value' },
    series: [{ ...base.series[0], type: 'line', smooth: true, areaStyle: {} }],
  }
})

const pieOption = computed(() => {
  const base = categoryPieOption(stats.value?.category ?? [])
  return {
    tooltip: { trigger: 'item' },
    legend: { bottom: 0 },
    series: [{ ...base.series[0], type: 'pie', radius: '60%' }],
  }
})

const dupRows = computed(() => duplicateRows(stats.value?.duplicates ?? {
  exactGroups: 0,
  exactFiles: 0,
  exactSavedBytes: 0,
  similarGroups: 0,
  similarFiles: 0,
  items: [],
}))
const exactGroups = computed(() => stats.value?.duplicates.exactGroups ?? 0)
const similarGroups = computed(() => stats.value?.duplicates.similarGroups ?? 0)
</script>

<template>
  <div
    v-loading="loading"
    class="dashboard"
  >
    <div class="dashboard-bar">
      <el-radio-group
        v-model="days"
        size="small"
        @change="onDaysChange"
      >
        <el-radio-button
          v-for="d in DAYS_OPTIONS"
          :key="d"
          :label="d"
        >
          近{{ d }}天
        </el-radio-button>
      </el-radio-group>
    </div>

    <el-row :gutter="16">
      <el-col :span="6">
        <el-card>
          <el-statistic
            title="已用空间"
            :value="usedText"
          />
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <el-statistic
            title="配额"
            :value="quotaText"
          />
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <el-statistic
            title="使用率"
            :value="rateText"
          />
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card>
          <el-statistic
            title="文件数"
            :value="fileCount"
          />
        </el-card>
      </el-col>
    </el-row>

    <el-row
      :gutter="16"
      class="dashboard-grid"
    >
      <el-col :span="12">
        <el-card header="存储分布">
          <ChartView :option="ringOption" />
        </el-card>
      </el-col>
      <el-col :span="12">
        <el-card header="上传时间线">
          <ChartView :option="lineOption" />
        </el-card>
      </el-col>
      <el-col :span="12">
        <el-card header="分类分布">
          <ChartView :option="pieOption" />
        </el-card>
      </el-col>
      <el-col :span="12">
        <el-card :header="`重复报告（精确${exactGroups}组 / 相似${similarGroups}组）`">
          <el-table :data="dupRows">
            <el-table-column
              prop="kind"
              label="类型"
              width="100"
            />
            <el-table-column
              prop="names"
              label="文件"
              min-width="180"
              show-overflow-tooltip
            />
            <el-table-column
              prop="count"
              label="数量"
              width="70"
            />
            <el-table-column
              prop="saved"
              label="可节省"
              width="110"
            />
            <template #empty>
              <el-empty description="暂无重复文件" />
            </template>
          </el-table>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<style scoped>
.dashboard-bar {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 16px;
}

.dashboard-grid {
  margin-top: 16px;
  row-gap: 16px;
}
</style>
