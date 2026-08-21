<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import {
  NButton,
  NCard,
  NEmpty,
  NSpace,
  NSpin,
  NTable,
  NText,
  useMessage,
  useThemeVars,
} from 'naive-ui'
import { statsApi, type StatsSnapshot } from '../api'

const themeVars = useThemeVars()

// --- 移动端检测 ---
const MOBILE_BREAKPOINT = 768
const isMobile = ref(window.innerWidth < MOBILE_BREAKPOINT)
function onResize() {
  isMobile.value = window.innerWidth < MOBILE_BREAKPOINT
}
onMounted(() => window.addEventListener('resize', onResize))
onUnmounted(() => window.removeEventListener('resize', onResize))

const stats = ref<StatsSnapshot | null>(null)
const loading = ref(false)

const message = useMessage()

async function loadStats() {
  loading.value = true
  try {
    const res = await statsApi.get()
    stats.value = res.data
  } catch (e: unknown) {
    message.error(e instanceof Error ? e.message : '加载统计失败')
  } finally {
    loading.value = false
  }
}

function percent(count: number): string {
  const total = stats.value?.total ?? 0
  if (total <= 0) return '0.0%'
  return ((count / total) * 100).toFixed(1) + '%'
}

onMounted(loadStats)
</script>

<template>
  <div>
    <div class="page-header">
      <h2 class="page-title">模型统计</h2>
      <n-space>
        <n-button @click="loadStats" :loading="loading">刷新</n-button>
      </n-space>
    </div>

    <n-spin :show="loading">
      <!-- 总览 -->
      <n-card :bordered="false" class="overview-card" title="总览" :style="{ borderColor: themeVars.borderColor }">
        <div class="overview-stats">
          <div class="overview-stat">
            <span class="overview-value">{{ stats?.total ?? 0 }}</span>
            <span class="overview-label" :style="{ color: themeVars.textColor3 }">次调用</span>
          </div>
          <div class="overview-sub">
            <span class="overview-label" :style="{ color: themeVars.textColor3 }">
              成功 {{ stats?.success ?? 0 }} / 失败 {{ stats?.failure ?? 0 }}
            </span>
          </div>
        </div>
        <div style="margin-top: 8px">
          <n-text :depth="3">仅统计最近 1000 次调用</n-text>
        </div>
      </n-card>

      <!-- 按逻辑模型分组 -->
      <n-card :bordered="false" class="overview-card" style="margin-top: 16px" title="按逻辑模型">
        <div :class="isMobile ? 'table-scroll-wrapper' : ''">
          <n-table :bordered="false" size="small">
            <thead>
              <tr>
                <th>逻辑模型</th>
                <th>调用次数</th>
                <th>成功</th>
                <th>失败</th>
                <th>占比</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in stats?.logical_models ?? []" :key="item.model">
                <td style="font-weight: 500">{{ item.model }}</td>
                <td>{{ item.total }}</td>
                <td>{{ item.success }}</td>
                <td>{{ item.failure }}</td>
                <td>{{ percent(item.total) }}</td>
              </tr>
              <tr v-if="!stats?.logical_models?.length">
                <td colspan="5">
                  <n-empty description="暂无统计数据" style="padding: 24px" />
                </td>
              </tr>
            </tbody>
          </n-table>
        </div>
      </n-card>

      <!-- 按实际模型分组 -->
      <n-card :bordered="false" class="overview-card" style="margin-top: 16px" title="按实际模型">
        <div :class="isMobile ? 'table-scroll-wrapper' : ''">
          <n-table :bordered="false" size="small">
            <thead>
              <tr>
                <th>实际模型</th>
                <th>调用次数</th>
                <th>成功</th>
                <th>失败</th>
                <th>占比</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in stats?.models ?? []" :key="item.model">
                <td style="font-weight: 500">{{ item.model }}</td>
                <td>{{ item.total }}</td>
                <td>{{ item.success }}</td>
                <td>{{ item.failure }}</td>
                <td>{{ percent(item.total) }}</td>
              </tr>
              <tr v-if="!stats?.models?.length">
                <td colspan="5">
                  <n-empty description="暂无统计数据" style="padding: 24px" />
                </td>
              </tr>
            </tbody>
          </n-table>
        </div>
      </n-card>
    </n-spin>
  </div>
</template>

<style scoped>
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}
.page-title {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
  color: v-bind('themeVars.textColor1');
}
.overview-card {
  border: 1px solid v-bind('themeVars.borderColor');
  border-radius: 6px;
}

.overview-stats {
  display: flex;
  align-items: baseline;
  gap: 16px;
}
.overview-stat {
  display: flex;
  align-items: baseline;
  gap: 8px;
}
.overview-value {
  font-size: 32px;
  font-weight: 700;
  color: #18a058;
}
.overview-label {
  font-size: 14px;
}

/* 移动端：表格横向滚动 */
.table-scroll-wrapper {
  overflow-x: auto;
  -webkit-overflow-scrolling: touch;
}
</style>
