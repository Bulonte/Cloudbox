<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as echarts from 'echarts/core'
import { LineChart, PieChart } from 'echarts/charts'
import { GridComponent, LegendComponent, TitleComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'

// S5 图表容器：按需引入 ECharts（line+pie），ResizeObserver 自适应，不拖首屏
echarts.use([LineChart, PieChart, GridComponent, LegendComponent, TitleComponent, TooltipComponent, CanvasRenderer])

type Option = Parameters<echarts.ECharts['setOption']>[0]

const props = defineProps<{ option: Option; height?: string }>()
const el = ref<HTMLDivElement | null>(null)
let chart: echarts.ECharts | null = null
let ro: ResizeObserver | null = null

function render(): void {
  if (el.value === null) return
  if (chart === null) {
    chart = echarts.init(el.value)
    ro = new ResizeObserver(() => chart?.resize())
    ro.observe(el.value)
  }
  chart.setOption(props.option, true)
}

onMounted(render)
watch(
  () => props.option,
  () => render(),
  { deep: true },
)
onBeforeUnmount(() => {
  ro?.disconnect()
  ro = null
  chart?.dispose()
  chart = null
})
</script>

<template>
  <div
    ref="el"
    class="chart-view"
    :style="{ height: props.height ?? '260px' }"
  />
</template>

<style scoped>
.chart-view {
  width: 100%;
}
</style>
