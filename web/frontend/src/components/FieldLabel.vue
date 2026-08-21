<script setup lang="ts">
import { NTooltip } from 'naive-ui'

defineProps<{
  text: string
  help: string
}>()
</script>

<template>
  <span class="field-label">
    {{ text }}
    <n-tooltip trigger="hover" placement="top">
      <template #trigger>
        <span class="help-icon" aria-label="帮助">?</span>
      </template>
      <span class="help-content">{{ help }}</span>
    </n-tooltip>
  </span>
</template>

<style scoped>
.field-label {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.help-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 14px;
  height: 14px;
  border-radius: 50%;
  border: 1px solid #999;
  color: #999;
  font-size: 11px;
  line-height: 1;
  cursor: help;
  user-select: none;
  transition: border-color 0.2s, color 0.2s;
}
.help-icon:hover {
  border-color: #2080f0;
  color: #2080f0;
}
.help-content {
  display: inline-block;
  max-width: 340px;
  line-height: 1.6;
}
</style>
