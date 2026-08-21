<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  NConfigProvider,
  NDialogProvider,
  NDrawer,
  NDrawerContent,
  NLayout,
  NLayoutContent,
  NLayoutHeader,
  NLayoutSider,
  NMenu,
  NMessageProvider,
  darkTheme,
  zhCN,
  dateZhCN,
} from 'naive-ui'
import type { MenuOption } from 'naive-ui'

const route = useRoute()
const router = useRouter()

const MOBILE_BREAKPOINT = 768
const isMobile = ref(window.innerWidth < MOBILE_BREAKPOINT)
const mobileMenuOpen = ref(false)

function onResize() {
  const mobile = window.innerWidth < MOBILE_BREAKPOINT
  if (isMobile.value !== mobile) {
    isMobile.value = mobile
    if (!mobile) mobileMenuOpen.value = false
  }
}

onMounted(() => window.addEventListener('resize', onResize))
onUnmounted(() => window.removeEventListener('resize', onResize))

const activeKey = computed(() => route.path)

const menuOptions: MenuOption[] = [
  { label: '决策监控', key: '/dashboard' },
  { label: '配置管理', key: '/config' },
  { label: '模型统计', key: '/stats' },
]

function handleMenuSelect(key: string) {
  router.push(key)
  if (isMobile.value) mobileMenuOpen.value = false
}

// 移动端路由变化时关闭菜单
watch(() => route.path, () => {
  if (isMobile.value) mobileMenuOpen.value = false
})
</script>

<template>
  <n-config-provider :locale="zhCN" :date-locale="dateZhCN" :theme="darkTheme">
    <n-message-provider>
      <n-dialog-provider>
        <!-- 移动端：汉堡菜单按钮 -->
        <button v-if="isMobile" class="mobile-hamburger" @click="mobileMenuOpen = true">
          <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <line x1="3" y1="6" x2="21" y2="6" />
            <line x1="3" y1="12" x2="21" y2="12" />
            <line x1="3" y1="18" x2="21" y2="18" />
          </svg>
        </button>

        <!-- 移动端：侧边栏 Drawer -->
        <n-drawer v-if="isMobile" v-model:show="mobileMenuOpen" :width="240" placement="left">
          <n-drawer-content body-content-style="padding: 0; display: flex; flex-direction: column">
            <div class="mobile-logo">Smart Coder Switch</div>
            <n-menu
              :value="activeKey"
              :options="menuOptions"
              style="flex: 1"
              @update:value="handleMenuSelect"
            />
          </n-drawer-content>
        </n-drawer>

        <!-- 桌面端：固定侧边栏 -->
        <n-layout v-else has-sider style="min-height: 100vh">
          <n-layout-sider bordered :width="220" content-style="display: flex; flex-direction: column">
            <n-layout-header
              bordered
              style="height: 56px; line-height: 56px; padding: 0 20px; font-weight: 700; font-size: 15px"
            >
              Smart Coder Switch
            </n-layout-header>
            <n-menu
              :value="activeKey"
              :options="menuOptions"
              style="flex: 1"
              @update:value="handleMenuSelect"
            />
          </n-layout-sider>
          <n-layout-content content-style="padding: 24px">
            <RouterView />
          </n-layout-content>
        </n-layout>

        <!-- 移动端：内容区 -->
        <div v-if="isMobile" class="mobile-content">
          <RouterView />
        </div>
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>

<style>
/* 全局：移动端根布局 */
* {
  box-sizing: border-box;
}

html, body, #app {
  margin: 0;
  padding: 0;
  width: 100%;
  overflow-x: hidden;
}
</style>

<style scoped>
.mobile-hamburger {
  position: fixed;
  top: 12px;
  left: 12px;
  z-index: 1001;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 40px;
  border: none;
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.08);
  color: #fff;
  cursor: pointer;
  backdrop-filter: blur(8px);
  transition: background 0.2s;
}

.mobile-hamburger:active {
  background: rgba(255, 255, 255, 0.16);
}

.mobile-logo {
  height: 56px;
  line-height: 56px;
  padding: 0 20px;
  font-weight: 700;
  font-size: 15px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.09);
}

.mobile-content {
  min-height: 100vh;
  padding: 56px 16px 24px;
}
</style>
