package replay

import (
	"fmt"
	"runtime"
	"testing"
	"time"
)

// 规模测试：10 万账户、约 10 万设备、约 29 万条记录、100 万个查询项。
// 两个各 5 万账户的大组之间反复用唯一桥接关系合并与拆分，
// 验证在最坏结构下仍满足耗时与内存要求。
func TestScaleRepeatedSplitMerge(t *testing.T) {
	const (
		groupSize   = 50000 // 每组账户数，共 10 万账户
		queryCycles = 2000  // 带查询的合并/拆分循环数
		extraCycles = 20000 // 无查询的空转循环数
		perQuery    = 250   // 每条查询的账户数
	)
	totalAcc := int64(2 * groupSize)

	p := NewProcessor()
	var evid int64

	// 两条链：账户 i -- 设备 i -- 账户 i+1，构成两个大组。
	// 组 A：账户 1..50000，设备 d1..d49999
	// 组 B：账户 50001..100000，设备 d50001..d99999
	chain := func(base int64) {
		for i := int64(0); i < groupSize-1; i++ {
			dev := fmt.Sprintf("d%d", base+i)
			evid++
			p.Add(Record{Type: RecordAddEvidence, EvidenceID: evid, Account: base + i, Device: dev})
			evid++
			p.Add(Record{Type: RecordAddEvidence, EvidenceID: evid, Account: base + i + 1, Device: dev})
		}
	}
	chain(1)
	chain(int64(groupSize) + 1)

	// 查询账户：一半来自组 A，一半来自组 B。
	queryAccs := make([]int64, 0, perQuery)
	for i := int64(0); i < perQuery/2; i++ {
		queryAccs = append(queryAccs, 1+i*2)
		queryAccs = append(queryAccs, int64(groupSize)+1+i*2)
	}

	// 反复合并/拆分：桥接设备 brg 上的两条证据是唯一连接。
	mergedQueryIx := make([]int, 0, queryCycles)
	splitQueryIx := make([]int, 0, queryCycles)
	numQueries := 0
	for c := 0; c < queryCycles+extraCycles; c++ {
		evid++
		b1 := evid
		p.Add(Record{Type: RecordAddEvidence, EvidenceID: b1, Account: 1, Device: "brg"})
		evid++
		b2 := evid
		p.Add(Record{Type: RecordAddEvidence, EvidenceID: b2, Account: totalAcc, Device: "brg"})
		if c < queryCycles {
			p.Add(query(queryAccs...))
			mergedQueryIx = append(mergedQueryIx, numQueries)
			numQueries++
		}
		p.Add(revoke(b1))
		p.Add(revoke(b2))
		if c < queryCycles {
			p.Add(query(queryAccs...))
			splitQueryIx = append(splitQueryIx, numQueries)
			numQueries++
		}
	}

	numRecords := len(p.records)
	t.Logf("记录总数 %d，查询 %d 条，查询项 %d", numRecords, numQueries, numQueries*perQuery)

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()

	results := p.Run()

	elapsed := time.Since(start)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	if len(results) != numQueries {
		t.Fatalf("结果数不符: got %d want %d", len(results), numQueries)
	}
	// 合并时：10 万人一组，最小编号 1。
	for _, qi := range mergedQueryIx {
		for k := range results[qi].Size {
			if results[qi].Size[k] != int(totalAcc) || results[qi].MinAccount[k] != 1 {
				t.Fatalf("合并查询 %d 第 %d 项: got (%d,%d)",
					qi, k, results[qi].Size[k], results[qi].MinAccount[k])
			}
		}
	}
	// 拆分后：各 5 万人，组标识分别为 1 与 50001。
	for _, qi := range splitQueryIx {
		for k, acc := range queryAccs {
			wantMin := int64(1)
			if acc > int64(groupSize) {
				wantMin = int64(groupSize) + 1
			}
			if results[qi].Size[k] != groupSize || results[qi].MinAccount[k] != wantMin {
				t.Fatalf("拆分查询 %d 第 %d 项: got (%d,%d) want (%d,%d)",
					qi, k, results[qi].Size[k], results[qi].MinAccount[k],
					groupSize, wantMin)
			}
		}
	}

	t.Logf("Run 耗时 %v", elapsed)
	t.Logf("堆内存增量 Alloc: %.1f MiB, TotalAlloc: %.1f MiB",
		float64(after.Alloc-before.Alloc)/1048576,
		float64(after.TotalAlloc-before.TotalAlloc)/1048576)
	t.Logf("进程峰值内存 RSS: %.1f MiB", float64(peakRSSBytes())/1048576)
	if elapsed > 15*time.Second {
		t.Fatalf("耗时超限: %v", elapsed)
	}
}
