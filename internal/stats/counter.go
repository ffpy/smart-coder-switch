// Package stats 提供并发安全的模型调用计数器。
//
// 计数器是进程内的累计值，服务重启后归零。
// 只统计已配置逻辑模型的请求，未知模型原样转发且不计数。
// 热重载不会清零计数器。
package stats

import (
	"sort"
	"sync"
)

// Counter 是并发安全的模型调用计数器。
//
// 零值不可用，必须通过 NewCounter 创建。
type Counter struct {
	mu              sync.RWMutex
	total           int
	success         int
	failure         int
	perModel        map[string]*modelCounters
	perLogicalModel map[string]*modelCounters
}

type modelCounters struct {
	total   int
	success int
	failure int
}

// Snapshot 是计数器的只读快照。
type Snapshot struct {
	Total         int          `json:"total"`
	Success       int          `json:"success"`
	Failure       int          `json:"failure"`
	Models        []ModelStats `json:"models"`
	LogicalModels []ModelStats `json:"logical_models"`
}

// ModelStats 是单个模型的累计调用统计。
type ModelStats struct {
	Model   string `json:"model"`
	Total   int    `json:"total"`
	Success int    `json:"success"`
	Failure int    `json:"failure"`
}

// NewCounter 创建并返回一个新的计数器。
func NewCounter() *Counter {
	return &Counter{
		perModel:        make(map[string]*modelCounters),
		perLogicalModel: make(map[string]*modelCounters),
	}
}

// Record 记录一次模型调用结果。
// model 是实际上游模型名，logicalModel 是客户端请求的逻辑模型名。
// success 为 true 表示请求成功（HTTP 2xx），false 表示失败。
//
// Record 是并发安全的。
func (c *Counter) Record(model string, logicalModel string, success bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.total++
	if success {
		c.success++
	} else {
		c.failure++
	}

	mc := c.perModel[model]
	if mc == nil {
		mc = &modelCounters{}
		c.perModel[model] = mc
	}

	mc.total++
	if success {
		mc.success++
	} else {
		mc.failure++
	}

	lmc := c.perLogicalModel[logicalModel]
	if lmc == nil {
		lmc = &modelCounters{}
		c.perLogicalModel[logicalModel] = lmc
	}

	lmc.total++
	if success {
		lmc.success++
	} else {
		lmc.failure++
	}
}

// Snapshot 返回当前计数的一个只读快照。
// 快照中的模型按模型名排序，确保输出稳定。
//
// Snapshot 是并发安全的。
func (c *Counter) Snapshot() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	snapshot := Snapshot{
		Total:   c.total,
		Success: c.success,
		Failure: c.failure,
	}

	snapshot.Models = make([]ModelStats, 0, len(c.perModel))

	for model, mc := range c.perModel {
		snapshot.Models = append(snapshot.Models, ModelStats{
			Model:   model,
			Total:   mc.total,
			Success: mc.success,
			Failure: mc.failure,
		})
	}

	sort.Slice(snapshot.Models, func(i, j int) bool {
		return snapshot.Models[i].Model < snapshot.Models[j].Model
	})

	snapshot.LogicalModels = make([]ModelStats, 0, len(c.perLogicalModel))

	for model, mc := range c.perLogicalModel {
		snapshot.LogicalModels = append(snapshot.LogicalModels, ModelStats{
			Model:   model,
			Total:   mc.total,
			Success: mc.success,
			Failure: mc.failure,
		})
	}

	sort.Slice(snapshot.LogicalModels, func(i, j int) bool {
		return snapshot.LogicalModels[i].Model < snapshot.LogicalModels[j].Model
	})

	return snapshot
}

// Reset 将所有计数归零。
//
// Reset 是并发安全的。
func (c *Counter) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.total = 0
	c.success = 0
	c.failure = 0
	c.perModel = make(map[string]*modelCounters)
	c.perLogicalModel = make(map[string]*modelCounters)
}
