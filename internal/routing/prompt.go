package routing

// PromptResult 表示注入的 prompt 结果。
type PromptResult struct {
	Kind    string
	Content string
}

const instructionWrapperHeader = `<smart-coder-switch-instruction>
以下内容由代理自动注入，不是用户的新需求；仅补充本轮执行约束。
本节要求的具体步骤必须执行并输出，不得跳过或忽略。

`

const instructionWrapperFooter = `
</smart-coder-switch-instruction>`

// WrapInstruction 将内容包裹在代理注入标签中。
// 统一包裹头措辞已去除"任务目标仍以前文真实用户消息为准 / 不要回复本段内容"等削弱性句子，
// 新增"本节要求的具体步骤必须执行并输出，不得跳过或忽略"，
// 保证图片理解等要求输出内容的注入步骤不会被模型当作可选的辅助说明而忽略。
func WrapInstruction(content string) string {
	return instructionWrapperHeader + content + instructionWrapperFooter
}

const highTierReviewPromptContent = `你当前被临时选为更强的模型进行审查。

在回复或调用任何工具前，先根据当前上下文和适用指令判断任务处于 PLAN 阶段还是 BUILD 阶段；如存在明确的阶段指令，必须遵从。阶段判断仅用于决定行动：仅在 PLAN 或阶段不明时说明阶段，BUILD 阶段无需说明阶段判断。

最后一条用户消息为疑问句时，判定为 PLAN 阶段。

若处于 PLAN 阶段：
- 简要说明当前阶段，并提供相关的分析、澄清、设计、风险评估或下一步计划动作。
- 允许调用只读工具进行排查，例如读取代码、列出目录、查看 Git 提交或差异、查询日志和配置。
- 不得修改源代码、测试、脚本、配置、迁移、样式或其他非 Markdown 产物。
- 不得运行构建、测试、部署、安装、迁移或其他实施操作。
- 除只读排查外，仅可讨论方案、提出必要问题，或在当前指令允许时创建、修订计划或设计 Markdown。
- 不要强制使用 【Review】 模板，也不要进入实施。

若处于 BUILD 阶段：
- 必须以如下可见复盘开头：
【Review】
方向判断: （评估当前方向是否高效且正确）
风险点: （识别最重要的风险、隐含假设或缺失的验证）
建议动作: （为后续模型或 Agent 给出具体的下一步）
避免事项: （如存在明显陷阱，说明不应做什么）

- 复盘后、开始编码或执行其他实施动作前，检查当前上下文列出的 available_skills 中是否存在与当前任务匹配的 skill。仅当某个 skill 确实适用且能提供相关编码规范或工作流指导时，才使用 skill 工具加载它；若 skill 已加载则直接复用，无需重复加载；没有合适的 skill 时忽略本提醒并按常规推进，不得强行加载。
- 复盘后可在同一回复中正常展开。如需调用工具，先输出复盘，再进行适当的工具调用。

若无法可靠判断当前阶段，按 PLAN 阶段处理：说明不确定性，请用户确认阶段；可按 PLAN 规则调用只读工具进行排查，但不得执行实施操作。

在未获得实际结果前，不得声称任何操作、修改或验证已经完成。`

func BuildHighTierPrompt() PromptResult {
	return PromptResult{
		Kind:    "high_tier_review",
		Content: WrapInstruction(highTierReviewPromptContent),
	}
}

const directFirstResponsePromptContent = `根据当前上下文和适用指令判断任务处于 PLAN 或 BUILD；明确阶段指令优先。判定为 PLAN 阶段时，回复必须先以“当前处于 PLAN 阶段”开头明确声明当前阶段，再输出后续内容，并全程谨记当前处于 PLAN 阶段、遵守 PLAN 约束；BUILD 无需说明阶段判断；咨询类问题应优先直接回答。

用户明确要求实施或推进既有工作时，必须判定为 BUILD，无需字面输入“BUILD”。包括：执行计划、提交代码、修改或修复代码、运行测试、构建部署、安装依赖、执行迁移、更新配置，或继续已确认的方案。

最后一条用户消息为疑问句时，判定为 PLAN 阶段：先声明“当前处于 PLAN 阶段”，再说明本轮仅讨论、澄清、分析或只读排查，不执行实施操作。

PLAN：先声明“当前处于 PLAN 阶段”后再继续；可讨论、澄清、分析或维护 Markdown 计划，并可运行用于排查的只读或诊断命令，例如查看目录、代码、Git 状态/差异/日志、配置和运行日志；不得修改非 Markdown 内容，不得执行提交代码、构建发布、部署、安装依赖或迁移等实施操作。

BUILD：简洁说明理解和拟采取的动作后，直接按请求调用工具并执行。执行前先检查当前上下文列出的 available_skills 中是否有与当前任务匹配的 skill；仅当某个 skill 确实适用且能提供相关编码规范或工作流指导时才加载，若已加载则直接复用无需重复加载，没有合适的 skill 则按常规推进，不得强行加载。

阶段不明时按 PLAN 处理：先声明“当前按 PLAN 阶段处理”，并请用户确认。不要强制使用 【Plan】、【Review】或其他固定标题；未获得实际结果前，不要声称操作已完成。`

func BuildDirectPrompt() PromptResult {
	return PromptResult{
		Kind:    "direct_first_response",
		Content: WrapInstruction(directFirstResponsePromptContent),
	}
}

// imageUnderstandingSection 是图片理解段落，与 DIRECT 首轮提示合并为单条注入。
// 仅命中 DIRECT 且最新用户消息包含图片时拼入。
// 图片理解是本轮必须完成的第一步任务，采用强约束措辞（必须/最高优先级/不得跳过），
// 避免被模型当作可选的辅助说明而忽略。
const imageUnderstandingSection = `【强制步骤 · 最高优先级】当前用户消息包含图片。在输出任何回答或调用任何工具之前，必须先完成图片理解并输出与用户问题相关的图片转写，不得跳过、延迟或省略；未完成图片转写输出前，禁止直接回答用户问题。

必须依次完成：

1. 图片文字与结构提取（包括 OCR）：
   - 表格必须完整转写：保留图片中全部可见的列、行、数值和单元格文本，使用 Markdown 表格或等价的结构化格式；列标题或单元格被截断时如实标注。
   - 代码、报错、公式、弹窗内容、字段名以及被红框/高亮/标注的内容必须按原文保留。
   - 页面标题、筛选条件、按钮、页签、导航和品牌信息，仅在与用户问题或表格含义有关时保留；无关内容可以省略。
2. 相关视觉信息：仅描述影响问题判断的红框、高亮、异常状态或位置关系；不要泛化描述无关的页面布局和装饰。
3. 问题定位：结合用户的问题，明确指出图片中相关的区域、证据和可得结论；不使用“如图所示”等依赖图片的指代。

输出要求（必须）：
- 使用结构化文字输出，作为后续对话上下文，供可能不支持多模态输入的模型继续接手处理。
- 表格内容不得因“精简”而省略；表格以外的信息按上述相关性规则取舍。
- 推荐按以下结构组织：图片关键信息、完整表格转写、相关公式/弹窗/标注、与问题的关联。

完成图片理解并输出转写结果后，再按照下方阶段规则处理用户的问题。`

// BuildDirectPromptWithImage 返回合并了图片理解段落与 DIRECT 首轮提示的单条注入提示。
// 命中 DIRECT 且最新用户消息包含图片时使用，只注入一条 user 消息。
// 与普通 DIRECT 提示共用统一包裹头（WrapInstruction）；图片理解段落的强约束措辞
// 与统一头"本节要求的具体步骤必须执行并输出"共同保证转写步骤不被忽略。
func BuildDirectPromptWithImage() PromptResult {
	return PromptResult{
		Kind: "direct_first_response",
		Content: WrapInstruction(
			imageUnderstandingSection + "\n\n" + directFirstResponsePromptContent,
		),
	}
}
