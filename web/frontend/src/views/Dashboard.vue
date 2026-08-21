<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch, computed } from 'vue'
import {
  NAlert,
  NButton,
  NCard,
  NDescriptions,
  NDescriptionsItem,
  NDivider,
  NDrawer,
  NDrawerContent,
  NEmpty,
  NInput,
  NInputNumber,
  NModal,
  NSpin,
  NSwitch,
  NTable,
  NTag,
  useThemeVars,
  useMessage,
} from 'naive-ui'
import { decisionApi, traceApi, type DecisionRecord, type DistributionTier, type TraceDetail } from '../api'
import VueJsonPretty from 'vue-json-pretty'
import 'vue-json-pretty/esm/styles.css'
import type { InputInst } from 'naive-ui'

const themeVars = useThemeVars()
const message = useMessage()

// --- 移动端检测 ---
const MOBILE_BREAKPOINT = 768
const isMobile = ref(window.innerWidth < MOBILE_BREAKPOINT)
function onResize() {
  isMobile.value = window.innerWidth < MOBILE_BREAKPOINT
}
onMounted(() => window.addEventListener('resize', onResize))
onUnmounted(() => window.removeEventListener('resize', onResize))

// 复制文本弹窗（非安全上下文时使用）
const showCopyModal = ref(false)
const copyModalText = ref('')
const copyModalTextarea = ref<InputInst | null>(null)

// 判断是否为安全上下文（HTTPS/localhost/127.0.0.1）
function isSecureContextForCopy(): boolean {
  if (typeof window === 'undefined') return false
  if (window.isSecureContext) return true
  const proto = location.protocol
  if (proto === 'https:') return true
  const host = location.hostname
  if (host === 'localhost' || host === '127.0.0.1') return true
  return false
}

// 在弹窗打开后自动全选文本
function handleCopyModalAfterEnter() {
  const ta = copyModalTextarea.value?.textareaElRef
  if (ta) {
    ta.focus()
    ta.select()
    ta.setSelectionRange(0, copyModalText.value.length)
  }
}

// 复制文本到剪贴板
// - 安全上下文（HTTPS/localhost/127.0.0.1）：优先使用 Clipboard API，结果可信
// - 非安全上下文（HTTP + 非 localhost）：弹出对话框展示文本，由用户手动选择并 Ctrl+C
async function copyTextViaClipboardApi(text: string): Promise<boolean> {
  if (!navigator.clipboard?.writeText) return false
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    return false
  }
}

// 复制路由决策字段到剪贴板
function copyDecisionFields() {
  const text = decisionFields.value.map((f) => `${f.label}: ${f.value}`).join('\n')
  if (isSecureContextForCopy()) {
    copyTextViaClipboardApi(text).then((ok) => {
      if (ok) {
        message.success('已复制路由决策')
      } else {
        message.error('复制失败：浏览器未授予剪贴板写入权限')
      }
    })
    return
  }
  // 非安全上下文：弹窗展示文本，引导用户手动复制
  copyModalText.value = text
  showCopyModal.value = true
}

// 决策详情中展示的字段（按日志输出顺序）
const DECISION_FIELD_DEFS: { key: string; label: string }[] = [
  { key: 'requestId', label: '请求ID' },
  { key: 'time', label: '时间' },
  { key: 'logicalModel', label: '逻辑模型' },
  { key: 'selectedTier', label: '档位' },
  { key: 'selectedModel', label: '实际模型' },
  { key: 'routeReason', label: '路由原因' },
  { key: 'bodySize', label: '请求体大小' },
  { key: 'mediumProbability', label: 'MEDIUM 概率' },
  { key: 'highProbability', label: 'HIGH 概率' },
  { key: 'randomValue', label: '随机值' },
  { key: 'assistantCount', label: 'assistant 轮次' },
  { key: 'highRounds', label: 'HIGH 轮次' },
  { key: 'mediumRounds', label: 'MEDIUM 轮次' },
  { key: 'promptInjected', label: '提示注入' },
  { key: 'promptInjectionKind', label: '提示注入类型' },
  { key: 'imagePromptInjected', label: '图片理解提示注入' },
  { key: 'imagePartsStripped', label: '图片内容已过滤' },
  { key: 'compatInjected', label: '兼容注入' },
  { key: 'compatKind', label: '兼容类型' },
  { key: 'compatToolCallIdPrefix', label: '工具调用前缀' },
  { key: 'continuationSkipped', label: '续写跳过' },
  { key: 'guidanceHistoryDetected', label: '检测历史指引' },
  { key: 'guidanceFollowupInjected', label: '指引跟进注入' },
  { key: 'guidanceMarkerKinds', label: '指引标记类型' },
]

const decisions = ref<DecisionRecord[]>([])
const distribution = ref<DistributionTier[]>([])
const loading = ref(false)
const error = ref('')
const selectedLogicalModel = ref('')
const selectedMinutes = ref(60)
const autoRefresh = ref(true)
let autoRefreshTimer: ReturnType<typeof setInterval> | null = null

// 详情抽屉
const drawerOpen = ref(false)
const drawerLoading = ref(false)
const drawerError = ref('')
const detail = ref<TraceDetail | null>(null)
// 当前选中的决策记录（含 status_code / error_message 等上游结果）
const selectedRecord = ref<DecisionRecord | null>(null)
// 原始请求仅提供下载，不在详情抽屉内展开

function statusOfSelectedRecord(): { type: 'default' | 'success' | 'error'; label: string; code: number; message: string } {
  const d = selectedRecord.value
  if (!d) return { type: 'default', label: '未知', code: 0, message: '' }
  return {
    type: statusTagType(d.status_code),
    label: statusLabel(d.status_code),
    code: d.status_code,
    message: d.error_message || '',
  }
}

async function loadDecisions() {
  loading.value = true
  error.value = ''
  try {
    const params: Record<string, unknown> = { limit: 50 }
    if (selectedLogicalModel.value) {
      params.logical_model = selectedLogicalModel.value
    }
    const res = await decisionApi.list(params)
    decisions.value = res.data.items || []
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    loading.value = false
  }
}

async function loadDistribution() {
  try {
    const params: Record<string, unknown> = { minutes: selectedMinutes.value }
    if (selectedLogicalModel.value) {
      params.logical_model = selectedLogicalModel.value
    }
    const res = await decisionApi.distribution(params)
    distribution.value = res.data.tiers || []
  } catch (e: unknown) {
    console.error('Failed to load distribution:', e)
  }
}

function tierTagType(tier: string): 'default' | 'primary' | 'info' | 'warning' | 'error' {
  const types: Record<string, 'default' | 'primary' | 'info' | 'warning' | 'error'> = {
    DIRECT: 'primary',
    LOW: 'default',
    MEDIUM: 'info',
    HIGH: 'warning',
  }
  return types[tier] || 'default'
}

// 上游请求结果状态：2xx 成功，非 2xx 失败，0 未知（旧记录或 Trace 未开启）
function statusTagType(statusCode: number): 'default' | 'success' | 'error' {
  if (statusCode === 0) return 'default'
  return statusCode >= 200 && statusCode < 300 ? 'success' : 'error'
}

function statusLabel(statusCode: number): string {
  if (statusCode === 0) return '未知'
  return statusCode >= 200 && statusCode < 300 ? `成功 (${statusCode})` : `失败 (${statusCode})`
}

function formatTime(ts: number): string {
  return new Date(ts * 1000).toLocaleString('zh-CN')
}

function formatPercentage(ratio: number): string {
  return (ratio * 100).toFixed(1)
}

function formatTraceValue(v: unknown): string {
  if (v === null || v === undefined) return '-'
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}

const decisionFields = computed(() => {
  const d = detail.value?.decision
  if (!d || typeof d !== 'object') return []
  const rec = d as Record<string, unknown>
  return DECISION_FIELD_DEFS.map((def) => ({
    key: def.key,
    label: def.label,
    value: formatTraceValue(rec[def.key]),
  }))
})

// 最后一条消息由详情接口直接返回（不限 role）
const lastMessage = computed(() => {
  const last = detail.value?.last_message
  return last && typeof last === 'object' ? (last as Record<string, unknown>) : null
})

// 请求头由详情接口直接返回
const requestHeaders = computed(() => {
  const h = detail.value?.headers
  return h && typeof h === 'object' ? (h as Record<string, string[]>) : null
})

// 请求头条目列表（按键名排序展示）
const requestHeaderEntries = computed(() => {
  if (!requestHeaders.value) return []
  return Object.entries(requestHeaders.value).sort((a, b) => a[0].localeCompare(b[0]))
})

function rawRequestDownloadUrl(): string {
  const traceDir = selectedRecord.value?.trace_dir
  return traceDir ? traceApi.requestDownloadUrl(traceDir) : ''
}

async function openDetail(d: DecisionRecord) {
  selectedRecord.value = d
  if (!d.trace_dir) {
    drawerError.value = '该记录没有关联 Trace 目录'
    drawerOpen.value = true
    drawerLoading.value = false
    return
  }
  drawerOpen.value = true
  drawerLoading.value = true
  drawerError.value = ''
  detail.value = null
  try {
    const res = await traceApi.get(d.trace_dir)
    detail.value = res.data
  } catch (e: unknown) {
    drawerError.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    drawerLoading.value = false
  }
}

function closeDetail() {
  drawerOpen.value = false
  selectedRecord.value = null
}

function refresh() {
  loadDecisions()
  loadDistribution()
}

function stopAutoRefresh() {
  if (autoRefreshTimer !== null) {
    clearInterval(autoRefreshTimer)
    autoRefreshTimer = null
  }
}

function startAutoRefresh() {
  stopAutoRefresh()
  autoRefreshTimer = setInterval(refresh, 5000)
}

watch(
  autoRefresh,
  (enabled) => {
    if (enabled) {
      startAutoRefresh()
    } else {
      stopAutoRefresh()
    }
  },
  { immediate: true },
)

onMounted(refresh)
onUnmounted(stopAutoRefresh)
</script>

<template>
  <div>
    <h2 class="page-title">决策监控</h2>

    <!-- 过滤器 -->
    <n-card class="mb-4" :bordered="false" :content-style="{ padding: '16px 20px' }">
      <div :class="isMobile ? 'filter-mobile' : 'filter-desktop'">
        <div>
          <label class="field-label" :style="{ color: themeVars.textColor3 }">逻辑模型</label>
          <n-input
            v-model:value="selectedLogicalModel"
            placeholder="例如: coder1"
            :style="{ width: isMobile ? '100%' : '200px' }"
            clearable
            @keyup.enter="refresh"
          />
        </div>
        <div>
          <label class="field-label" :style="{ color: themeVars.textColor3 }">时间窗口（分钟）</label>
          <n-input-number
            :value="selectedMinutes"
            :min="1"
            :style="{ width: isMobile ? '100%' : '140px' }"
            @update:value="(v) => (selectedMinutes = v ?? 60)"
          />
        </div>
        <div>
          <n-button type="primary" @click="refresh" :block="isMobile">刷新</n-button>
        </div>
        <div>
          <label class="field-label" :style="{ color: themeVars.textColor3 }">自动刷新（5秒）</label>
          <n-switch v-model:value="autoRefresh" />
        </div>
      </div>
    </n-card>

    <!-- 错误提示 -->
    <n-alert v-if="error" type="error" class="mb-4" :show-icon="false">
      {{ error }}
    </n-alert>

    <!-- 分布统计 -->
    <n-card class="mb-4" :bordered="false" v-if="distribution.length > 0">
      <template #header>决策分布</template>
      <div class="distribution-grid">
        <div
          v-for="item in distribution"
          :key="item.name"
          class="distribution-item"
          :style="{ borderColor: themeVars.borderColor }"
        >
          <div class="distribution-name" :style="{ color: themeVars.textColor3 }">{{ item.name }}</div>
          <div class="distribution-count">{{ item.count }}</div>
          <div class="distribution-ratio" :style="{ color: themeVars.textColor3 }">{{ formatPercentage(item.ratio) }}%</div>
        </div>
      </div>
    </n-card>

    <!-- 决策列表 -->
    <n-card :bordered="false">
      <n-spin :show="loading">
        <!-- 桌面端：完整表格 -->
        <n-table v-if="!isMobile" :bordered="false" size="small">
          <thead>
            <tr>
              <th>时间</th>
              <th>逻辑模型</th>
              <th>档位</th>
              <th>实际模型</th>
              <th>轮次</th>
              <th>结果</th>
              <th>原因</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="d in decisions" :key="d.id">
              <td style="white-space: nowrap">{{ formatTime(d.timestamp) }}</td>
              <td style="white-space: nowrap">{{ d.logical_model }}</td>
              <td style="white-space: nowrap">
                <n-tag :type="tierTagType(d.selected_tier)" size="small">
                  {{ d.selected_tier }}
                </n-tag>
              </td>
              <td style="white-space: nowrap">{{ d.selected_model }}</td>
              <td>{{ d.assistant_count }}</td>
              <td style="white-space: nowrap">
                <n-tag :type="statusTagType(d.status_code)" size="small">
                  {{ statusLabel(d.status_code) }}
                </n-tag>
              </td>
              <td style="max-width: 320px">{{ d.reason }}</td>
              <td style="white-space: nowrap">
                <n-button size="tiny" secondary type="primary" @click="openDetail(d)">
                  详情
                </n-button>
              </td>
            </tr>
            <tr v-if="!loading && decisions.length === 0">
              <td colspan="8">
                <n-empty description="暂无决策记录" style="padding: 24px" />
              </td>
            </tr>
          </tbody>
        </n-table>

        <!-- 移动端：卡片列表 -->
        <div v-else class="mobile-card-list">
          <div
            v-for="d in decisions"
            :key="d.id"
            class="mobile-card"
            :style="{ borderColor: themeVars.borderColor }"
            @click="openDetail(d)"
          >
            <div class="mobile-card-header">
              <n-tag :type="tierTagType(d.selected_tier)" size="small">
                {{ d.selected_tier }}
              </n-tag>
              <n-tag :type="statusTagType(d.status_code)" size="small">
                {{ statusLabel(d.status_code) }}
              </n-tag>
            </div>
            <div class="mobile-card-model">{{ d.logical_model }}</div>
            <div class="mobile-card-meta">
              <span>轮次 {{ d.assistant_count }}</span>
              <span>{{ formatTime(d.timestamp) }}</span>
            </div>
          </div>
          <n-empty v-if="!loading && decisions.length === 0" description="暂无决策记录" style="padding: 24px" />
        </div>
      </n-spin>
    </n-card>

    <!-- 详情抽屉 -->
    <n-drawer
      v-model:show="drawerOpen"
      :width="isMobile ? undefined : 860"
      :style="isMobile ? {} : {}"
      placement="right"
      @update:show="(v) => !v && closeDetail()"
    >
      <n-drawer-content title="决策详情" closable>
        <n-spin :show="drawerLoading">
          <n-alert v-if="drawerError" type="error" class="mb-4" :show-icon="false">
            {{ drawerError }}
          </n-alert>
          <template v-if="detail">
            <div class="detail-block">
              <div class="detail-title">上游结果</div>
              <n-descriptions :column="isMobile ? 1 : 2" size="small" bordered :label-style="{ width: isMobile ? '100px' : '140px' }">
                <n-descriptions-item label="状态">
                  <n-tag :type="statusOfSelectedRecord().type" size="small">
                    {{ statusOfSelectedRecord().label }}
                  </n-tag>
                </n-descriptions-item>
                <n-descriptions-item label="状态码">
                  {{ statusOfSelectedRecord().code || '-' }}
                </n-descriptions-item>
                <n-descriptions-item v-if="statusOfSelectedRecord().message" label="错误摘要" :span="isMobile ? 1 : 2">
                  <div class="error-message">{{ statusOfSelectedRecord().message }}</div>
                </n-descriptions-item>
              </n-descriptions>
            </div>

            <n-divider />

            <div class="detail-block">
              <div class="detail-title" style="display: flex; align-items: center; justify-content: space-between">
                <span>路由决策</span>
                <n-button size="tiny" secondary type="primary" @click="copyDecisionFields">复制</n-button>
              </div>
              <n-descriptions :column="isMobile ? 1 : 2" size="small" bordered :label-style="{ width: isMobile ? '100px' : '140px' }">
                <n-descriptions-item v-for="f in decisionFields" :key="f.key" :label="f.label">
                  {{ f.value }}
                </n-descriptions-item>
              </n-descriptions>
            </div>

            <template v-if="lastMessage">
              <n-divider />
              <div class="detail-block">
                <div class="detail-title">最后消息 <n-tag size="small" :type="lastMessage.role === 'user' ? 'info' : lastMessage.role === 'assistant' ? 'success' : 'default'" style="margin-left: 6px">{{ lastMessage.role }}</n-tag></div>
                <div class="json-box" :style="{ borderColor: themeVars.borderColor, backgroundColor: themeVars.cardColor }">
                  <VueJsonPretty
                    :data="lastMessage"
                    :deep="3"
                    theme="dark"
                    virtual
                    :height="300"
                    show-line-number
                  />
                </div>
              </div>
            </template>

            <template v-if="requestHeaderEntries.length > 0">
              <n-divider />
              <div class="detail-block">
                <div class="detail-title">请求头</div>
                <n-descriptions :column="1" size="small" bordered :label-style="{ width: isMobile ? '120px' : '180px' }">
                  <n-descriptions-item v-for="[key, values] in requestHeaderEntries" :key="key" :label="key">
                    <span v-if="values.length === 1">{{ values[0] }}</span>
                    <div v-else>
                      <div v-for="(val, idx) in values" :key="idx" style="margin: 2px 0">{{ val }}</div>
                    </div>
                  </n-descriptions-item>
                </n-descriptions>
              </div>
            </template>

            <n-divider />

            <div class="detail-block">
              <div class="detail-title" style="display: flex; align-items: center; justify-content: space-between">
                <span>原始请求</span>
                <n-button
                  size="tiny"
                  secondary
                  type="primary"
                  tag="a"
                  :href="rawRequestDownloadUrl() || undefined"
                  :disabled="!rawRequestDownloadUrl()"
                  download
                >
                  下载 JSON
                </n-button>
              </div>
              <div class="raw-request-placeholder" :style="{ borderColor: themeVars.borderColor, color: themeVars.textColor3 }">
                原始请求内容较大，请下载 JSON 文件查看
              </div>
            </div>
          </template>
        </n-spin>
      </n-drawer-content>
    </n-drawer>

    <!-- 非 HTTPS 手动复制弹窗 -->
    <n-modal
      v-model:show="showCopyModal"
      preset="dialog"
      title="手动复制"
      :show-icon="false"
      positive-text="关闭"
      @after-enter="handleCopyModalAfterEnter"
    >
      <p style="margin-bottom: 12px; font-size: 13px; color: #999">
        当前页面非 HTTPS，浏览器禁止自动写入剪贴板。请选择下方文本后按 Ctrl+C 复制：
      </p>
      <n-input
        ref="copyModalTextarea"
        :value="copyModalText"
        type="textarea"
        :autosize="{ minRows: 6, maxRows: 12 }"
        readonly
      />
    </n-modal>
  </div>
</template>

<style scoped>
.page-title {
  margin: 0 0 16px;
  font-size: 20px;
  font-weight: 600;
  color: v-bind('themeVars.textColor1');
}
.field-label {
  display: block;
  margin-bottom: 6px;
  font-size: 13px;
}
.mb-4 {
  margin-bottom: 16px;
}

/* 桌面端筛选栏：横向排列 */
.filter-desktop {
  display: flex;
  align-items: flex-end;
  flex-wrap: wrap;
  gap: 36px;
}

/* 移动端筛选栏：纵向堆叠 */
.filter-mobile {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

/* 移动端卡片列表 */
.mobile-card-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.mobile-card {
  border: 1px solid;
  border-radius: 8px;
  padding: 12px;
  cursor: pointer;
  transition: background 0.15s;
}
.mobile-card:active {
  background: rgba(255, 255, 255, 0.05);
}
.mobile-card-header {
  display: flex;
  gap: 8px;
  margin-bottom: 6px;
}
.mobile-card-model {
  font-weight: 500;
  font-size: 14px;
  margin-bottom: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mobile-card-meta {
  display: flex;
  justify-content: space-between;
  font-size: 12px;
  opacity: 0.6;
}

.distribution-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
}
.distribution-item {
  border: 1px solid v-bind('themeVars.borderColor');
  border-radius: 6px;
  padding: 12px;
}
.distribution-name {
  font-size: 13px;
}
.distribution-count {
  margin-top: 4px;
  font-size: 24px;
  font-weight: 600;
}
.distribution-ratio {
  margin-top: 2px;
  font-size: 13px;
}
.detail-block {
  margin-bottom: 4px;
}
.detail-title {
  font-size: 15px;
  font-weight: 600;
  margin-bottom: 12px;
}
.json-box {
  border: 1px solid v-bind('themeVars.borderColor');
  border-radius: 6px;
  padding: 12px;
  max-height: 60vh;
  overflow: auto;
  margin-top: 12px;
}
.error-message {
  white-space: pre-wrap;
  word-break: break-all;
  font-family: monospace;
  font-size: 13px;
  color: v-bind('themeVars.errorColor');
}
.raw-request-placeholder {
  border: 1px dashed v-bind('themeVars.borderColor');
  border-radius: 6px;
  padding: 12px;
  font-size: 13px;
  margin-top: 12px;
}

/* 移动端：筛选栏 label 不换行 */
@media (max-width: 767px) {
  .filter-mobile .field-label {
    margin-bottom: 4px;
  }
}
</style>
