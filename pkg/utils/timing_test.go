package utils

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestTimingCumulativeSnapshotAndContext(t *testing.T) {
	type valueKey struct{}
	parent, cancel := context.WithTimeout(context.WithValue(context.Background(), valueKey{}, "value"), time.Second)
	defer cancel()
	ctx := StartTiming(parent)
	if StartTiming(ctx) != ctx {
		t.Fatal("重复启动重置了计时器")
	}
	if ctx.Value(valueKey{}) != "value" {
		t.Fatal("丢失Context值")
	}
	gotDeadline, _ := ctx.Deadline()
	wantDeadline, _ := parent.Deadline()
	if !gotDeadline.Equal(wantDeadline) {
		t.Fatal("改变截止时间")
	}
	timer := ctx.Value(timingKey{}).(*timing)
	now := timer.start
	timer.now = func() time.Time { return now }
	now = now.Add(3 * time.Millisecond)
	MarkTiming(ctx, "auth")
	now = now.Add(9 * time.Millisecond)
	MarkTiming(ctx, "query")
	MarkTiming(ctx, "auth")
	MarkTiming(ctx, "entry")
	MarkTiming(ctx, "")
	want := map[string]int64{"entry": 0, "auth": 3, "query": 12}
	snapshot := TimingSnapshot(ctx)
	if !reflect.DeepEqual(snapshot, want) {
		t.Fatalf("时间点=%v", snapshot)
	}
	snapshot["auth"] = 999
	if !reflect.DeepEqual(TimingSnapshot(ctx), want) {
		t.Fatal("快照污染计时器")
	}
	MarkTiming(ctx, "finish")
	if _, exists := snapshot["finish"]; exists {
		t.Fatal("后续标记改变了已有快照")
	}
	other := StartTiming(context.Background())
	if !reflect.DeepEqual(TimingSnapshot(other), map[string]int64{"entry": 0}) {
		t.Fatal("请求串数据")
	}
	MarkTiming(context.Background(), "ignored")
	if TimingSnapshot(context.Background()) != nil {
		t.Fatal("无计时器应为空")
	}
	cancel()
	if ctx.Err() != context.Canceled {
		t.Fatal("未保留取消")
	}
}

func TestTimingConcurrentMarksAndSnapshots(t *testing.T) {
	ctx := StartTiming(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			MarkTiming(ctx, "shared")
			snapshot := TimingSnapshot(ctx)
			snapshot["local"] = 1
		}()
	}
	wg.Wait()
	snapshot := TimingSnapshot(ctx)
	if len(snapshot) != 2 || snapshot["shared"] < 0 {
		t.Fatalf("并发结果=%v", snapshot)
	}
}
