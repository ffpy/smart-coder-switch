package routing

import (
	"strings"
	"testing"
)

func TestBuildDirectPrompt(t *testing.T) {
	result := BuildDirectPrompt()

	if result.Kind != "direct_first_response" {
		t.Fatalf("expected kind 'direct_first_response', got %q", result.Kind)
	}

	if result.Content == "" {
		t.Fatal("expected non-empty content")
	}

	assertPhaseGuard(t, result.Content)
	// 最后一条用户消息为疑问句时，应按 PLAN 阶段处理
	if !strings.Contains(result.Content, "最后一条用户消息为疑问句时，判定为 PLAN 阶段") {
		t.Fatal("expected prompt to classify interrogative messages as PLAN")
	}
	if !strings.Contains(result.Content, "运行用于排查的只读或诊断命令") {
		t.Fatal("expected PLAN phase to allow diagnostic investigation commands")
	}
	if !strings.Contains(result.Content, "不得执行提交代码") {
		t.Fatal("expected PLAN phase to prohibit implementation actions")
	}
	if !strings.Contains(result.Content, "BUILD 无需说明阶段判断") {
		t.Fatal("expected prompt to avoid restating the phase during build")
	}

	// PLAN 判定后必须先声明当前阶段，再输出后续内容，让模型谨记处于 PLAN 阶段
	if !strings.Contains(result.Content, "当前处于 PLAN 阶段") {
		t.Fatal("expected prompt to require stating the current PLAN phase first")
	}
	if !strings.Contains(result.Content, "全程谨记") {
		t.Fatal("expected prompt to require keeping the PLAN phase in mind")
	}
	if !strings.Contains(result.Content, "先声明“当前处于 PLAN 阶段”后再继续") {
		t.Fatal("expected PLAN section to require declaring the phase before proceeding")
	}
	if !strings.Contains(result.Content, "当前按 PLAN 阶段处理") {
		t.Fatal("expected prompt to declare PLAN handling when phase is unknown")
	}

	for _, buildIntent := range []string{"执行计划", "提交代码", "运行测试", "构建部署"} {
		if !strings.Contains(result.Content, buildIntent) {
			t.Fatalf("expected prompt to classify %q as BUILD intent", buildIntent)
		}
	}

	if !strings.Contains(result.Content, "先") &&
		!strings.Contains(result.Content, "说明") {
		t.Fatal("expected prompt to ask for explanation first")
	}

	// 必须明确禁止强制使用固定标题
	if !strings.Contains(result.Content, "不要强制使用") {
		t.Fatal("expected prompt to forbid forcing fixed templates")
	}

	// 必须禁止虚假完成声明
	if !strings.Contains(result.Content, "声称") {
		t.Fatal("expected prompt to forbid false completion claims")
	}

	if !strings.Contains(result.Content, "检查当前上下文列出的 available_skills") {
		t.Fatal("expected DIRECT prompt to remind skill availability check before implementation")
	}
	if !strings.Contains(result.Content, "确实适用") {
		t.Fatal("expected DIRECT prompt to require skill applicability")
	}
	if !strings.Contains(result.Content, "不得强行加载") {
		t.Fatal("expected DIRECT prompt to forbid forced skill loading")
	}
	if !strings.Contains(result.Content, "已加载") {
		t.Fatal("expected DIRECT prompt to instruct skipping already-loaded skills")
	}
}

func TestBuildHighTierPrompt(t *testing.T) {
	result := BuildHighTierPrompt()

	if result.Kind != "high_tier_review" {
		t.Fatalf("expected kind 'high_tier_review', got %q", result.Kind)
	}

	if result.Content == "" {
		t.Fatal("expected non-empty content")
	}

	assertPhaseGuard(t, result.Content)
	// 最后一条用户消息为疑问句时，应按 PLAN 阶段处理
	if !strings.Contains(result.Content, "最后一条用户消息为疑问句时，判定为 PLAN 阶段") {
		t.Fatal("expected prompt to classify interrogative messages as PLAN")
	}
	assertPlanAllowsReadOnlyInvestigation(t, result.Content)
	if !strings.Contains(result.Content, "BUILD 阶段无需说明阶段判断") {
		t.Fatal("expected prompt to avoid restating the phase during build")
	}
	if !strings.Contains(result.Content, "无法可靠判断") {
		t.Fatal("expected prompt to treat an unknown phase conservatively")
	}

	if !strings.Contains(result.Content, "【Review】") {
		t.Fatal("expected build-phase review format")
	}

	if !strings.Contains(result.Content, "检查当前上下文列出的 available_skills") {
		t.Fatal("expected HIGH prompt to remind skill availability check before implementation")
	}
	if !strings.Contains(result.Content, "确实适用") {
		t.Fatal("expected HIGH prompt to require skill applicability")
	}
	if !strings.Contains(result.Content, "不得强行加载") {
		t.Fatal("expected HIGH prompt to forbid forced skill loading")
	}
	if !strings.Contains(result.Content, "已加载") {
		t.Fatal("expected HIGH prompt to instruct skipping already-loaded skills")
	}
}

func TestBuildDirectPromptWithImage(t *testing.T) {
	result := BuildDirectPromptWithImage()

	// 合并后仍作为 DIRECT 首轮提示，kind 保持一致
	if result.Kind != "direct_first_response" {
		t.Fatalf("expected kind 'direct_first_response', got %q", result.Kind)
	}

	if result.Content == "" {
		t.Fatal("expected non-empty content")
	}

	// 应使用统一的包裹标注
	if !strings.Contains(result.Content, "<smart-coder-switch-instruction>") {
		t.Fatal("expected merged prompt to use the unified instruction wrapper")
	}

	// 图片理解必须是强制第一步，使用强约束措辞，避免被忽略
	if !strings.Contains(result.Content, "不得跳过") {
		t.Fatal("expected image prompt to forbid skipping the image understanding step")
	}
	if !strings.Contains(result.Content, "最高优先级") {
		t.Fatal("expected image prompt to mark image understanding as highest priority")
	}
	if !strings.Contains(result.Content, "未完成图片转写输出前，禁止直接回答用户问题") {
		t.Fatal("expected image prompt to forbid answering before transcription")
	}

	// 统一包裹头声明本节步骤必须执行并输出（方案 B 措辞）
	if !strings.Contains(result.Content, "必须执行并输出") {
		t.Fatal("expected unified wrapper to mandate executing and outputting this section")
	}
	if !strings.Contains(result.Content, "不得跳过或忽略") {
		t.Fatal("expected unified wrapper to forbid skipping or ignoring this section")
	}

	// 应包含图片理解段落：完整表格转写要求
	if !strings.Contains(result.Content, "表格必须完整转写") {
		t.Fatal("expected prompt to require complete table transcription")
	}
	if !strings.Contains(result.Content, "全部可见的列、行、数值") {
		t.Fatal("expected prompt to require all visible table rows and values")
	}

	// 表格以外的无关页面内容允许省略
	if !strings.Contains(result.Content, "无关内容可以省略") {
		t.Fatal("expected prompt to allow omitting irrelevant non-table UI content")
	}

	// 应包含图片理解段落：相关视觉信息要求
	if !strings.Contains(result.Content, "相关视觉信息") {
		t.Fatal("expected prompt to mention relevant visual information")
	}

	// 应包含图片理解段落：结构化输出要求
	if !strings.Contains(result.Content, "结构化文字") {
		t.Fatal("expected prompt to mention structured output")
	}

	// 应明确图片理解结果供后续可能不支持多模态的模型接手
	if !strings.Contains(result.Content, "多模态") {
		t.Fatal("expected prompt to mention multimodal handoff")
	}
	if !strings.Contains(result.Content, "接手") && !strings.Contains(result.Content, "继续处理") {
		t.Fatal("expected prompt to mention handing off to later models")
	}
	if !strings.Contains(result.Content, "如图所示") {
		t.Fatal("expected prompt to forbid image-dependent references")
	}

	// 应同时包含原有 DIRECT 首轮提示内容（阶段判断等）
	assertPhaseGuard(t, result.Content)
	if !strings.Contains(result.Content, "最后一条用户消息为疑问句时，判定为 PLAN 阶段") {
		t.Fatal("expected merged prompt to keep the DIRECT interrogative PLAN rule")
	}
	if !strings.Contains(result.Content, "available_skills") {
		t.Fatal("expected merged prompt to keep the DIRECT skill availability reminder")
	}
}

func assertPlanAllowsReadOnlyInvestigation(t *testing.T, prompt string) {
	t.Helper()

	if !strings.Contains(prompt, "允许调用只读工具进行排查") {
		t.Fatal("expected PLAN phase to allow read-only investigation tools")
	}
}

func assertPhaseGuard(t *testing.T, prompt string) {
	t.Helper()

	for _, expected := range []string{
		"PLAN",
		"BUILD",
		"不得修改",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected phase guard to contain %q, got %s", expected, prompt)
		}
	}
}
