<script setup lang="ts">
import { ref, onMounted, computed, onUnmounted } from 'vue'
import {
  NButton,
  NCard,
  NEmpty,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NModal,
  NSpin,
  NSwitch,
  NTag,
  useDialog,
  useMessage,
  useThemeVars,
} from 'naive-ui'
import { configApi, type Config } from '../api'
import FieldLabel from '../components/FieldLabel.vue'

const themeVars = useThemeVars()

// --- 移动端检测 ---
const MOBILE_BREAKPOINT = 768
const isMobile = ref(window.innerWidth < MOBILE_BREAKPOINT)
function onResize() {
  isMobile.value = window.innerWidth < MOBILE_BREAKPOINT
}
onMounted(() => window.addEventListener('resize', onResize))
onUnmounted(() => window.removeEventListener('resize', onResize))

const config = ref<Config | null>(null)
const loading = ref(false)
const saving = ref(false)

const dialog = useDialog()
const message = useMessage()

const logicalModels = computed(() => {
  if (!config.value?.models) return []
  return Object.keys(config.value.models)
})

// 添加模型弹窗状态
const showAddModal = ref(false)
const newModelName = ref('')

function openAddModal() {
  newModelName.value = ''
  showAddModal.value = true
}

function confirmAddModel() {
  if (!config.value?.models) return
  const name = newModelName.value.trim()
  if (!name) {
    message.warning('请输入模型名称')
    return false
  }
  if (config.value.models[name]) {
    message.warning(`逻辑模型 "${name}" 已存在`)
    return false
  }
  config.value.models[name] = {
    low_model: '',
    medium_model: '',
    medium_probability: 0,
    high_model: '',
    high_probability: 0,
  }
  message.success(`已添加逻辑模型 "${name}"`)
  return true
}

function removeModel(name: string) {
  if (!config.value?.models) return
  dialog.warning({
    title: '删除确认',
    content: `确定删除逻辑模型 "${name}" 吗？`,
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: () => {
      delete config.value!.models![name]
      message.success(`已删除逻辑模型 "${name}"`)
    },
  })
}

async function loadConfig() {
  loading.value = true
  try {
    const res = await configApi.get()
    const raw = res.data
    // Normalize nested objects so template can safely access fields
    raw.listen = raw.listen ?? { address: '' }
    raw.upstream = raw.upstream ?? { base_url: '' }
    raw.models = raw.models ?? {}
    raw.sqlite = raw.sqlite ?? { path: '', max_records: 1000 }
    raw.trace = raw.trace ?? { directory: '', max_records: 100 }
    config.value = raw
  } catch (e: unknown) {
    message.error(e instanceof Error ? e.message : '加载配置失败')
  } finally {
    loading.value = false
  }
}

async function saveConfig() {
  if (!config.value || saving.value) return
  saving.value = true
  try {
    await configApi.save(config.value)
    message.success('配置已保存并重新加载')
  } catch (e: unknown) {
    message.error(e instanceof Error ? e.message : '保存配置失败')
  } finally {
    saving.value = false
  }
}

onMounted(loadConfig)
</script>

<template>
  <div>
    <div class="page-header">
      <h2 class="page-title">配置管理</h2>
      <n-button
        type="primary"
        :loading="saving"
        :disabled="!config"
        @click="saveConfig"
      >
        {{ saving ? '保存中...' : '保存并重载' }}
      </n-button>
    </div>

    <n-spin :show="loading">
      <n-form v-if="config" :label-placement="isMobile ? 'top' : 'left'" label-width="auto" class="config-form">
        <!-- 服务配置 -->
        <n-card :bordered="false" class="config-card" title="服务配置" :style="{ borderColor: themeVars.borderColor }">
          <n-form-item label="监听地址">
            <n-input v-model:value="config.listen.address" placeholder="例如: 0.0.0.0:18082" />
          </n-form-item>
          <n-form-item label="上游地址">
            <n-input v-model:value="config.upstream.base_url" placeholder="https://your-provider.example.com/" />
          </n-form-item>
        </n-card>

        <!-- 模型配置 -->
        <n-card :bordered="false" class="config-card" :style="{ borderColor: themeVars.borderColor }">
          <template #header>
            <div class="model-header">
              <span>模型路由配置</span>
              <n-button size="small" @click="openAddModal">添加逻辑模型</n-button>
            </div>
          </template>

          <div class="model-list">
            <n-card
              v-for="modelName in logicalModels"
              :key="modelName"
              :bordered="true"
              size="small"
              class="model-card"
            >
              <template #header>
                <div class="model-header">
                  <n-tag type="primary" size="small">{{ modelName }}</n-tag>
                  <n-button size="tiny" type="error" text @click="removeModel(modelName)">
                    删除
                  </n-button>
                </div>
              </template>

              <div class="model-grid">
                <n-form-item label-placement="top">
                  <template #label>
                    <FieldLabel
                      text="LOW 模型"
                      help="默认档位模型，处理常规编码任务，成本更低。请求未命中 DIRECT、固定轮次或概率档位时，使用该模型转发。"
                    />
                  </template>
                  <n-input
                    :value="config!.models[modelName].low_model"
                    @update:value="(v: string) => (config!.models[modelName].low_model = v)"
                  />
                </n-form-item>
                <n-form-item label-placement="top">
                  <template #label>
                    <FieldLabel
                      text="MEDIUM 模型"
                      help="中档模型。概率路由命中 MEDIUM 档（或命中 MEDIUM 固定轮次）时切换使用，只改模型、不注入提示。可与 LOW 配置为同一模型。"
                    />
                  </template>
                  <n-input
                    :value="config!.models[modelName].medium_model"
                    @update:value="(v: string) => (config!.models[modelName].medium_model = v)"
                    placeholder="留空则不启用该档位"
                  />
                </n-form-item>
                <n-form-item label-placement="top">
                  <template #label>
                    <FieldLabel
                      text="HIGH 模型"
                      help="高档模型。概率路由命中 HIGH 档（或命中 HIGH 固定轮次）时切换使用，并自动注入复盘提示。"
                    />
                  </template>
                  <n-input
                    :value="config!.models[modelName].high_model"
                    @update:value="(v: string) => (config!.models[modelName].high_model = v)"
                    placeholder="留空则不启用该档位"
                  />
                </n-form-item>
                <n-form-item label-placement="top">
                  <template #label>
                    <FieldLabel
                      text="MEDIUM 概率"
                      help="概率路由时命中 MEDIUM 档的概率。判定区间为 [HIGH 概率, HIGH 概率 + MEDIUM 概率)，即随机值落在此区间时选中 MEDIUM。"
                    />
                  </template>
                  <n-input-number
                    :value="config!.models[modelName].medium_probability"
                    :min="0"
                    :max="1"
                    :step="0.01"
                    style="width: 100%"
                    @update:value="(v: number | null) => (config!.models[modelName].medium_probability = v ?? 0)"
                  />
                </n-form-item>
                <n-form-item label-placement="top">
                  <template #label>
                    <FieldLabel
                      text="HIGH 概率"
                      help="概率路由时命中 HIGH 档的概率。随机值落在 [0, HIGH 概率) 区间时选中 HIGH，并注入复盘提示。"
                    />
                  </template>
                  <n-input-number
                    :value="config!.models[modelName].high_probability"
                    :min="0"
                    :max="1"
                    :step="0.01"
                    style="width: 100%"
                    @update:value="(v: number | null) => (config!.models[modelName].high_probability = v ?? 0)"
                  />
                </n-form-item>
                <n-form-item label-placement="top">
                  <template #label>
                    <FieldLabel
                      text="HIGH 固定轮次"
                      help="可选。assistant 消息数能被该值整除时，强制切换 HIGH 模型并注入复盘提示。留空或 0 表示不启用固定轮次触发。"
                    />
                  </template>
                  <n-input-number
                    :value="config!.models[modelName].high_rounds ?? null"
                    :min="0"
                    style="width: 100%"
                    @update:value="(v: number | null) => (config!.models[modelName].high_rounds = v ?? undefined)"
                  />
                </n-form-item>
                <n-form-item label-placement="top">
                  <template #label>
                    <FieldLabel
                      text="MEDIUM 固定轮次"
                      help="可选。assistant 消息数能被该值整除（且未命中 HIGH 固定轮次）时，强制切换 MEDIUM 模型，不注入提示。留空或 0 表示不启用。"
                    />
                  </template>
                  <n-input-number
                    :value="config!.models[modelName].medium_rounds ?? null"
                    :min="0"
                    style="width: 100%"
                    @update:value="(v: number | null) => (config!.models[modelName].medium_rounds = v ?? undefined)"
                  />
                </n-form-item>
                <n-form-item label-placement="top">
                  <template #label>
                    <FieldLabel
                      text="DIRECT 模型"
                      help="可选。配置后，最新用户消息为真实输入时强制路由到该模型（优先级最高）。通常配置能力更强或支持多模态输入的模型。"
                    />
                  </template>
                  <n-input
                    :value="config!.models[modelName].direct_model"
                    @update:value="(v: string) => (config!.models[modelName].direct_model = v || undefined)"
                    placeholder="新任务强制路由模型"
                  />
                </n-form-item>
                <n-form-item label-placement="top">
                  <template #label>
                    <FieldLabel
                      text="DIRECT 注入首轮提示"
                      help="可选，默认开启。控制 DIRECT 档命中时是否注入首轮说明提示：先说明对任务的理解与判断，再允许工具调用。关闭后 DIRECT 命中时不注入该提示。"
                    />
                  </template>
                  <n-switch
                    :value="config!.models[modelName].direct_prompt_enabled !== false"
                    @update:value="(v: boolean) => (config!.models[modelName].direct_prompt_enabled = v)"
                  />
                </n-form-item>
                <n-form-item label-placement="top">
                  <template #label>
                    <FieldLabel
                      text="防复述注入"
                      help="可选，默认关闭。开启后，仅当最新一条 AI 消息的文本包含【Review】/【Plan】标记时，追加防复述指令，要求直接执行历史指导、避免机械复述模板标题。更早的历史标记不触发。"
                    />
                  </template>
                  <n-switch
                    :value="config!.models[modelName].anti_repetition_prompt_enabled === true"
                    @update:value="(v: boolean) => (config!.models[modelName].anti_repetition_prompt_enabled = v ? true : undefined)"
                  />
                </n-form-item>
                <n-form-item label-placement="top">
                  <template #label>
                    <FieldLabel
                      text="图片理解提示注入"
                      help="可选，默认开启。DIRECT 档命中且最新消息含图片时，注入合并提示（图片理解 + 首轮说明，单条消息带包裹标注），供后续不支持多模态输入的模型接手处理。关闭时退回普通首轮说明提示；同时路由到非多模态模型时不再过滤历史图片，保留原样转发。"
                    />
                  </template>
                  <n-switch
                    :value="config!.models[modelName].image_prompt_enabled !== false"
                    @update:value="(v: boolean) => (config!.models[modelName].image_prompt_enabled = v)"
                  />
                </n-form-item>
              </div>
            </n-card>

            <n-empty v-if="logicalModels.length === 0" description="暂无模型配置，点击上方按钮添加" />
          </div>
        </n-card>

        <!-- Trace 配置 -->
        <n-card v-if="config.trace" :bordered="false" class="config-card" title="Trace 配置" :style="{ borderColor: themeVars.borderColor }">
          <div class="trace-grid">
            <div>
              <n-form-item label="Trace 目录">
                <n-input
                  :value="config!.trace!.directory"
                  @update:value="(v: string) => (config!.trace!.directory = v)"
                  placeholder="./log/traces"
                />
              </n-form-item>
            </div>
            <div>
              <n-form-item label="最大记录数">
                <n-input-number
                  :value="config!.trace!.max_records ?? null"
                  :min="1"
                  style="width: 100%"
                  @update:value="(v: number | null) => (config!.trace!.max_records = v ?? undefined)"
                />
              </n-form-item>
            </div>
            <div>
              <n-form-item label="最大请求体大小">
                <n-input-number
                  :value="config!.trace!.max_body_size ?? null"
                  :min="1"
                  style="width: 100%"
                  @update:value="(v: number | null) => (config!.trace!.max_body_size = v ?? undefined)"
                />
              </n-form-item>
            </div>
          </div>
        </n-card>

        <!-- SQLite 配置 -->
        <n-card :bordered="false" class="config-card" title="SQLite 配置" :style="{ borderColor: themeVars.borderColor }">
          <div class="sqlite-grid">
            <n-form-item label="数据库路径">
              <n-input v-model:value="config.sqlite.path" placeholder="./data/decisions.db" />
            </n-form-item>
            <n-form-item label="最大记录数">
              <n-input-number
                :value="config!.sqlite.max_records"
                :min="1"
                style="width: 100%"
                @update:value="(v: number | null) => (config!.sqlite.max_records = v ?? 0)"
              />
            </n-form-item>
          </div>
        </n-card>
      </n-form>
    </n-spin>

    <!-- 添加模型弹窗 -->
    <n-modal
      v-model:show="showAddModal"
      preset="dialog"
      title="添加逻辑模型"
      positive-text="添加"
      negative-text="取消"
      :on-positive-click="confirmAddModel"
    >
      <n-input
        v-model:value="newModelName"
        placeholder="输入新逻辑模型名称"
        @keyup.enter="showAddModal = false"
      />
    </n-modal>
  </div>
</template>

<style scoped>
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
  gap: 12px;
}
.page-title {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
  color: v-bind('themeVars.textColor1');
}
.config-form {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.config-card {
  border: 1px solid v-bind('themeVars.borderColor');
  border-radius: 6px;
}
.trace-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 0 16px;
}
.sqlite-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 0 16px;
}
.model-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  width: 100%;
  gap: 8px;
}
.model-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.model-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 0 16px;
}

/* 移动端适配 */
@media (max-width: 767px) {
  .page-header {
    flex-wrap: wrap;
  }
  .trace-grid,
  .sqlite-grid {
    grid-template-columns: 1fr;
  }
  .model-grid {
    grid-template-columns: 1fr;
  }
}
</style>
