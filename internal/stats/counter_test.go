package stats

import (
	"sync"
	"testing"
)

func TestNewCounter(t *testing.T) {
	c := NewCounter()
	snap := c.Snapshot()

	if snap.Total != 0 {
		t.Fatalf("expected total 0, got %d", snap.Total)
	}

	if snap.Success != 0 {
		t.Fatalf("expected success 0, got %d", snap.Success)
	}

	if snap.Failure != 0 {
		t.Fatalf("expected failure 0, got %d", snap.Failure)
	}

	if len(snap.Models) != 0 {
		t.Fatalf("expected 0 models, got %d", len(snap.Models))
	}

	if len(snap.LogicalModels) != 0 {
		t.Fatalf("expected 0 logical models, got %d", len(snap.LogicalModels))
	}
}

func TestCounterRecord(t *testing.T) {
	c := NewCounter()

	c.Record("model-a", "model-a", true)
	c.Record("model-a", "model-a", true)
	c.Record("model-a", "model-a", false)
	c.Record("model-b", "model-b", true)

	snap := c.Snapshot()

	if snap.Total != 4 {
		t.Fatalf("expected total 4, got %d", snap.Total)
	}

	if snap.Success != 3 {
		t.Fatalf("expected success 3, got %d", snap.Success)
	}

	if snap.Failure != 1 {
		t.Fatalf("expected failure 1, got %d", snap.Failure)
	}

	if len(snap.Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(snap.Models))
	}

	// model-a first (sorted)
	if snap.Models[0].Model != "model-a" {
		t.Fatalf("expected first model model-a, got %s", snap.Models[0].Model)
	}

	if snap.Models[0].Total != 3 {
		t.Fatalf("expected model-a total 3, got %d", snap.Models[0].Total)
	}

	if snap.Models[0].Success != 2 {
		t.Fatalf("expected model-a success 2, got %d", snap.Models[0].Success)
	}

	if snap.Models[0].Failure != 1 {
		t.Fatalf("expected model-a failure 1, got %d", snap.Models[0].Failure)
	}

	if snap.Models[1].Model != "model-b" {
		t.Fatalf("expected second model model-b, got %s", snap.Models[1].Model)
	}

	if snap.Models[1].Total != 1 {
		t.Fatalf("expected model-b total 1, got %d", snap.Models[1].Total)
	}

	if snap.Models[1].Success != 1 {
		t.Fatalf("expected model-b success 1, got %d", snap.Models[1].Success)
	}

	if snap.Models[1].Failure != 0 {
		t.Fatalf("expected model-b failure 0, got %d", snap.Models[1].Failure)
	}

	// LogicalModels mirror Models since logical names match actual names
	if len(snap.LogicalModels) != 2 {
		t.Fatalf("expected 2 logical models, got %d", len(snap.LogicalModels))
	}

	if snap.LogicalModels[0].Model != "model-a" {
		t.Fatalf("expected first logical model model-a, got %s", snap.LogicalModels[0].Model)
	}

	if snap.LogicalModels[0].Total != 3 {
		t.Fatalf("expected logical model-a total 3, got %d", snap.LogicalModels[0].Total)
	}

	if snap.LogicalModels[1].Model != "model-b" {
		t.Fatalf("expected second logical model model-b, got %s", snap.LogicalModels[1].Model)
	}

	if snap.LogicalModels[1].Total != 1 {
		t.Fatalf("expected logical model-b total 1, got %d", snap.LogicalModels[1].Total)
	}
}

func TestCounterSnapshotIsolation(t *testing.T) {
	c := NewCounter()
	c.Record("model-a", "model-a", true)

	snap := c.Snapshot()

	// mutate after snapshot
	c.Record("model-b", "model-b", false)

	if snap.Total != 1 {
		t.Fatalf("expected snapshot total 1, got %d", snap.Total)
	}

	if len(snap.Models) != 1 {
		t.Fatalf("expected snapshot 1 model, got %d", len(snap.Models))
	}

	if len(snap.LogicalModels) != 1 {
		t.Fatalf("expected snapshot 1 logical model, got %d", len(snap.LogicalModels))
	}
}

func TestCounterReset(t *testing.T) {
	c := NewCounter()
	c.Record("model-a", "model-a", true)
	c.Record("model-b", "model-b", false)

	c.Reset()

	snap := c.Snapshot()

	if snap.Total != 0 {
		t.Fatalf("expected total 0 after reset, got %d", snap.Total)
	}

	if snap.Success != 0 {
		t.Fatalf("expected success 0 after reset, got %d", snap.Success)
	}

	if snap.Failure != 0 {
		t.Fatalf("expected failure 0 after reset, got %d", snap.Failure)
	}

	if len(snap.Models) != 0 {
		t.Fatalf("expected 0 models after reset, got %d", len(snap.Models))
	}

	if len(snap.LogicalModels) != 0 {
		t.Fatalf("expected 0 logical models after reset, got %d", len(snap.LogicalModels))
	}
}

func TestCounterConcurrentSafety(t *testing.T) {
	c := NewCounter()

	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()
			c.Record("model-a", "model-a", true)
			c.Record("model-b", "model-b", false)
			c.Snapshot()
		}()
	}

	wg.Wait()

	snap := c.Snapshot()

	if snap.Total != 200 {
		t.Fatalf("expected total 200, got %d", snap.Total)
	}

	if snap.Success != 100 {
		t.Fatalf("expected success 100, got %d", snap.Success)
	}

	if snap.Failure != 100 {
		t.Fatalf("expected failure 100, got %d", snap.Failure)
	}
}

func TestCounterConcurrentReset(t *testing.T) {
	c := NewCounter()

	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for j := 0; j < 10; j++ {
				c.Record("model-a", "model-a", true)
				c.Snapshot()
			}
		}()
	}

	wg.Add(1)

	go func() {
		defer wg.Done()
		c.Reset()
	}()

	wg.Wait()

	// no panic
	_ = c.Snapshot()
}

func TestCounterLogicalModelIndependence(t *testing.T) {
	c := NewCounter()

	// 同一个逻辑模型 "smart-coder" 对应两个不同实际模型
	c.Record("gpt-4o", "smart-coder", true)
	c.Record("deepseek-v4-flash", "smart-coder", false)
	c.Record("gpt-4o", "smart-coder", true)

	snap := c.Snapshot()

	// 实际模型统计
	if len(snap.Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(snap.Models))
	}

	if snap.Models[0].Model != "deepseek-v4-flash" {
		t.Fatalf("expected first model deepseek-v4-flash, got %s", snap.Models[0].Model)
	}

	if snap.Models[0].Total != 1 {
		t.Fatalf("expected deepseek-v4-flash total 1, got %d", snap.Models[0].Total)
	}

	if snap.Models[0].Failure != 1 {
		t.Fatalf("expected deepseek-v4-flash failure 1, got %d", snap.Models[0].Failure)
	}

	if snap.Models[1].Model != "gpt-4o" {
		t.Fatalf("expected second model gpt-4o, got %s", snap.Models[1].Model)
	}

	if snap.Models[1].Total != 2 {
		t.Fatalf("expected gpt-4o total 2, got %d", snap.Models[1].Total)
	}

	// 逻辑模型统计：1 个逻辑模型聚合了 3 次调用
	if len(snap.LogicalModels) != 1 {
		t.Fatalf("expected 1 logical model, got %d", len(snap.LogicalModels))
	}

	if snap.LogicalModels[0].Model != "smart-coder" {
		t.Fatalf("expected logical model smart-coder, got %s", snap.LogicalModels[0].Model)
	}

	if snap.LogicalModels[0].Total != 3 {
		t.Fatalf("expected logical model total 3, got %d", snap.LogicalModels[0].Total)
	}

	if snap.LogicalModels[0].Success != 2 {
		t.Fatalf("expected logical model success 2, got %d", snap.LogicalModels[0].Success)
	}

	if snap.LogicalModels[0].Failure != 1 {
		t.Fatalf("expected logical model failure 1, got %d", snap.LogicalModels[0].Failure)
	}
}
