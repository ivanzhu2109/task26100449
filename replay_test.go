package devlink

import (
	"fmt"
	"math/rand"
	"testing"
)

// reference 是暴力参考实现：对每条查询，基于其之前仍有效的证据
// 重新建图并 BFS 求连通块，用于对照验证。
func reference(records []Record) []QueryResult {
	type ev struct {
		acc int64
		dev string
	}
	active := make(map[uint64]ev)
	var results []QueryResult
	for _, rec := range records {
		switch rec.Kind {
		case AddEvidence:
			active[rec.EvidenceID] = ev{rec.Account, rec.Device}
		case RevokeEvidence:
			delete(active, rec.EvidenceID)
		case QueryGroup:
			byDev := make(map[string][]int64)
			for _, e := range active {
				byDev[e.dev] = append(byDev[e.dev], e.acc)
			}
			adj := make(map[int64][]int64)
			for _, accs := range byDev {
				for i := 0; i < len(accs); i++ {
					for j := i + 1; j < len(accs); j++ {
						adj[accs[i]] = append(adj[accs[i]], accs[j])
						adj[accs[j]] = append(adj[accs[j]], accs[i])
					}
				}
			}
			res := QueryResult{Groups: make([]GroupInfo, len(rec.Accounts))}
			for k, a := range rec.Accounts {
				seen := map[int64]bool{a: true}
				queue := []int64{a}
				minAcc := a
				for len(queue) > 0 {
					cur := queue[len(queue)-1]
					queue = queue[:len(queue)-1]
					if cur < minAcc {
						minAcc = cur
					}
					for _, nb := range adj[cur] {
						if !seen[nb] {
							seen[nb] = true
							queue = append(queue, nb)
						}
					}
				}
				res.Groups[k] = GroupInfo{Count: len(seen), MinAccount: minAcc}
			}
			results = append(results, res)
		}
	}
	return results
}

func assertResults(t *testing.T, got, want []QueryResult) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("结果条数不符: got %d, want %d", len(got), len(want))
	}
	for i := range got {
		if len(got[i].Groups) != len(want[i].Groups) {
			t.Fatalf("第 %d 条查询结果长度不符: got %d, want %d",
				i, len(got[i].Groups), len(want[i].Groups))
		}
		for j := range got[i].Groups {
			if got[i].Groups[j] != want[i].Groups[j] {
				t.Fatalf("第 %d 条查询第 %d 项不符: got %+v, want %+v",
					i, j, got[i].Groups[j], want[i].Groups[j])
			}
		}
	}
}

// TestMultiEvidenceRevoke 同一账户与同一设备存在多条证据时，
// 逐条撤销，只有最后一条被撤销后关系才断开。
func TestMultiEvidenceRevoke(t *testing.T) {
	records := []Record{
		{Kind: AddEvidence, EvidenceID: 1, Account: 10, Device: "dev1"},
		{Kind: AddEvidence, EvidenceID: 2, Account: 10, Device: "dev1"},
		{Kind: AddEvidence, EvidenceID: 3, Account: 10, Device: "dev1"},
		{Kind: AddEvidence, EvidenceID: 4, Account: 20, Device: "dev1"},
		{Kind: QueryGroup, Accounts: []int64{10, 20}},
		{Kind: RevokeEvidence, EvidenceID: 1},
		{Kind: QueryGroup, Accounts: []int64{10, 20}},
		{Kind: RevokeEvidence, EvidenceID: 2},
		{Kind: QueryGroup, Accounts: []int64{10, 20}},
		{Kind: RevokeEvidence, EvidenceID: 3},
		{Kind: QueryGroup, Accounts: []int64{10, 20}},
	}
	got := Replay(records)
	want := []QueryResult{
		{Groups: []GroupInfo{{2, 10}, {2, 10}}},
		{Groups: []GroupInfo{{2, 10}, {2, 10}}},
		{Groups: []GroupInfo{{2, 10}, {2, 10}}},
		{Groups: []GroupInfo{{1, 10}, {1, 20}}},
	}
	assertResults(t, got, want)
	assertResults(t, reference(records), want)
}

// TestRingAndBridge 带环关联撤销一条证据仍连通；
// 撤销桥接证据后真正拆组。两种情况下人数与最小编号都要正确。
func TestRingAndBridge(t *testing.T) {
	records := []Record{
		// 环: 1-d1-2-d2-3-d3-1
		{Kind: AddEvidence, EvidenceID: 1, Account: 1, Device: "d1"},
		{Kind: AddEvidence, EvidenceID: 2, Account: 2, Device: "d1"},
		{Kind: AddEvidence, EvidenceID: 3, Account: 2, Device: "d2"},
		{Kind: AddEvidence, EvidenceID: 4, Account: 3, Device: "d2"},
		{Kind: AddEvidence, EvidenceID: 5, Account: 3, Device: "d3"},
		{Kind: AddEvidence, EvidenceID: 6, Account: 1, Device: "d3"},
		// 另一组: 10-d9-11
		{Kind: AddEvidence, EvidenceID: 7, Account: 10, Device: "d9"},
		{Kind: AddEvidence, EvidenceID: 8, Account: 11, Device: "d9"},
		// 桥: 3 与 10 共享 dbridge，两组连通
		{Kind: AddEvidence, EvidenceID: 9, Account: 3, Device: "dbridge"},
		{Kind: AddEvidence, EvidenceID: 10, Account: 10, Device: "dbridge"},
		{Kind: QueryGroup, Accounts: []int64{1, 2, 3, 10, 11}},
		// 撤销环上一条证据，环变链，仍连通
		{Kind: RevokeEvidence, EvidenceID: 6},
		{Kind: QueryGroup, Accounts: []int64{1, 2, 3, 10, 11}},
		// 撤销桥的一侧，拆组
		{Kind: RevokeEvidence, EvidenceID: 9},
		{Kind: QueryGroup, Accounts: []int64{1, 2, 3, 10, 11}},
		// 撤销桥的另一侧，结果不变
		{Kind: RevokeEvidence, EvidenceID: 10},
		{Kind: QueryGroup, Accounts: []int64{1, 2, 3, 10, 11}},
	}
	got := Replay(records)
	want := []QueryResult{
		{Groups: []GroupInfo{{5, 1}, {5, 1}, {5, 1}, {5, 1}, {5, 1}}},
		{Groups: []GroupInfo{{5, 1}, {5, 1}, {5, 1}, {5, 1}, {5, 1}}},
		{Groups: []GroupInfo{{3, 1}, {3, 1}, {3, 1}, {2, 10}, {2, 10}}},
		{Groups: []GroupInfo{{3, 1}, {3, 1}, {3, 1}, {2, 10}, {2, 10}}},
	}
	assertResults(t, got, want)
	assertResults(t, reference(records), want)
}

// TestTransitiveAndIsolated 传递关联与无关联账户自成一组。
func TestTransitiveAndIsolated(t *testing.T) {
	records := []Record{
		{Kind: AddEvidence, EvidenceID: 1, Account: 100, Device: "x"},
		{Kind: AddEvidence, EvidenceID: 2, Account: 200, Device: "x"},
		{Kind: AddEvidence, EvidenceID: 3, Account: 200, Device: "y"},
		{Kind: AddEvidence, EvidenceID: 4, Account: 300, Device: "y"},
		{Kind: QueryGroup, Accounts: []int64{100, 300, 999, 999}},
	}
	got := Replay(records)
	want := []QueryResult{
		{Groups: []GroupInfo{{3, 100}, {3, 100}, {1, 999}, {1, 999}}},
	}
	assertResults(t, got, want)
	assertResults(t, reference(records), want)
}

// TestHistoryImmutable 后续撤销不能改变此前查询的结果：
// 相同前缀下，先查询再撤销 与 不撤销，此前查询结果必须一致。
func TestHistoryImmutable(t *testing.T) {
	prefix := []Record{
		{Kind: AddEvidence, EvidenceID: 1, Account: 1, Device: "d"},
		{Kind: AddEvidence, EvidenceID: 2, Account: 2, Device: "d"},
		{Kind: QueryGroup, Accounts: []int64{1, 2}},
	}
	withRevoke := append(append([]Record{}, prefix...),
		Record{Kind: RevokeEvidence, EvidenceID: 1},
		Record{Kind: RevokeEvidence, EvidenceID: 2},
		Record{Kind: QueryGroup, Accounts: []int64{1, 2}},
	)
	got := Replay(withRevoke)
	want := []QueryResult{
		{Groups: []GroupInfo{{2, 1}, {2, 1}}},
		{Groups: []GroupInfo{{1, 1}, {1, 2}}},
	}
	assertResults(t, got, want)
}

// TestRandomAgainstReference 随机操作序列与暴力参考实现全量对照，
// 验证每个历史查询都等于按该位置之前有效证据独立算出的结果。
func TestRandomAgainstReference(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	var records []Record
	var activeIDs []uint64
	nextID := uint64(1)
	for i := 0; i < 4000; i++ {
		switch {
		case len(activeIDs) == 0 || rng.Intn(100) < 55:
			records = append(records, Record{
				Kind:       AddEvidence,
				EvidenceID: nextID,
				Account:    int64(rng.Intn(60) + 1),
				Device:     fmt.Sprintf("dev%d", rng.Intn(25)),
			})
			activeIDs = append(activeIDs, nextID)
			nextID++
		case rng.Intn(100) < 60:
			k := rng.Intn(len(activeIDs))
			records = append(records, Record{Kind: RevokeEvidence, EvidenceID: activeIDs[k]})
			activeIDs = append(activeIDs[:k], activeIDs[k+1:]...)
		default:
			n := rng.Intn(10) + 1
			accs := make([]int64, n)
			for j := range accs {
				accs[j] = int64(rng.Intn(70) + 1)
			}
			records = append(records, Record{Kind: QueryGroup, Accounts: accs})
		}
	}
	assertResults(t, Replay(records), reference(records))
}
