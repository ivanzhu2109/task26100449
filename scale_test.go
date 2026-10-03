package devlink

import (
	"fmt"
	"runtime"
	"testing"
	"time"
)

// TestScaleRepeatedSplitMerge 规模测试：
//   - 10 万个账户、约 10 万个设备指纹、30 万条记录、100 万个查询项；
//   - 两个各 4 万账户的大组之间仅有一座桥，桥被反复搭建与撤销，
//     使大组反复合并与拆分；
//   - 校验每个查询的人数与最小编号，并输出耗时与内存。
func TestScaleRepeatedSplitMerge(t *testing.T) {
	const (
		groupSize   = 40000 // 每个大组 4 万账户
		fillerAccs  = 20000 // 零散账户，补足 10 万账户
		fillerDevs  = 5     // 每个零散账户的设备数，补足约 10 万设备
		bridgeRound = 29000 // 桥反复搭建/撤销的轮数
		numQueries  = 2000  // 每条 500 账户，共 100 万查询项
		querySpan   = 500
	)

	var records []Record
	var expectMerged []bool // 每条查询生成时桥是否连通
	nextID := uint64(1)
	add := func(acc int64, dev string) {
		records = append(records, Record{Kind: AddEvidence, EvidenceID: nextID, Account: acc, Device: dev})
		nextID++
	}
	revoke := func(id uint64) {
		records = append(records, Record{Kind: RevokeEvidence, EvidenceID: id})
	}
	queryAcc := int64(1)
	merged := false
	maybeQuery := func(step, every int) {
		if step%every != 0 || len(expectMerged) >= numQueries {
			return
		}
		accs := make([]int64, querySpan)
		for i := range accs {
			accs[i] = queryAcc
			queryAcc++
			if queryAcc > 100000 {
				queryAcc = 1
			}
		}
		records = append(records, Record{Kind: QueryGroup, Accounts: accs})
		expectMerged = append(expectMerged, merged)
	}

	// 两个大组：各自通过共享一个设备指纹形成 4 万账户的星型组。
	for a := int64(1); a <= groupSize; a++ {
		add(a, "groupA")
	}
	for a := int64(groupSize + 1); a <= 2*groupSize; a++ {
		add(a, "groupB")
	}
	// 零散账户与设备，补足规模。
	for a := int64(2*groupSize + 1); a <= 2*groupSize+fillerAccs; a++ {
		for d := 0; d < fillerDevs; d++ {
			add(a, fmt.Sprintf("filler_%d_%d", a, d))
		}
	}
	// 桥反复搭建与撤销，期间穿插查询。
	const every = 2 // 每 2 条桥记录插一条查询，凑满 2000 条查询
	for r := 0; r < bridgeRound; r++ {
		b1, b2 := nextID, nextID+1
		add(1, "bridge")
		maybeQuery(len(records), every)
		add(groupSize+1, "bridge")
		merged = true
		maybeQuery(len(records), every)
		revoke(b1)
		merged = false
		maybeQuery(len(records), every)
		revoke(b2)
		maybeQuery(len(records), every)
	}
	t.Logf("记录总数 %d，查询条数 %d，查询项总数 %d",
		len(records), len(expectMerged), len(expectMerged)*querySpan)

	// 后台采样峰值内存。
	var peak uint64
	var ms runtime.MemStats
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				runtime.ReadMemStats(&ms)
				if ms.Alloc > peak {
					peak = ms.Alloc
				}
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()
	runtime.GC()
	runtime.ReadMemStats(&ms)
	before := ms.Alloc

	start := time.Now()
	results := Replay(records)
	elapsed := time.Since(start)

	close(stop)
	runtime.ReadMemStats(&ms)
	t.Logf("Replay 耗时 %v", elapsed)
	t.Logf("内存: 运行前 %d MiB, 结束后存活 %d MiB, 运行期峰值约 %d MiB",
		before>>20, ms.Alloc>>20, peak>>20)

	if len(results) != len(expectMerged) {
		t.Fatalf("查询结果条数不符: got %d, want %d", len(results), len(expectMerged))
	}
	// 重新扫描出查询记录，与结果一一对照。
	qi := 0
	for _, rec := range records {
		if rec.Kind != QueryGroup {
			continue
		}
		m := expectMerged[qi]
		res := results[qi]
		qi++
		for j, a := range rec.Accounts {
			var want GroupInfo
			switch {
			case a <= groupSize:
				if m {
					want = GroupInfo{2 * groupSize, 1}
				} else {
					want = GroupInfo{groupSize, 1}
				}
			case a <= 2*groupSize:
				if m {
					want = GroupInfo{2 * groupSize, 1}
				} else {
					want = GroupInfo{groupSize, groupSize + 1}
				}
			default:
				want = GroupInfo{1, a}
			}
			if res.Groups[j] != want {
				t.Fatalf("第 %d 条查询账户 %d: got %+v, want %+v", qi-1, a, res.Groups[j], want)
			}
		}
	}
	if elapsed > 15*time.Second {
		t.Errorf("耗时 %v 超过 15 秒预算", elapsed)
	}
	if peak > 512<<20 {
		t.Errorf("峰值内存 %d MiB 超过 512 MiB 预算", peak>>20)
	}
}
