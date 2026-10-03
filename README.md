# devlink

本地设备关联回放库（Go）。由上层身份筛查流程直接以库形式调用，无接口层；
账户清单、行为记录与查询结果全部保存在内存中。

## 功能

按业务顺序处理一整批有序记录，记录类型三种：

- `AddEvidence`：新增一条设备绑定证据（独立编号，关联一个账户与一个设备指纹）。
- `RevokeEvidence`：撤销指定编号的证据。同一账户与同一设备之间可存在多条
  独立证据，只要还有有效证据，该账户与设备的关系就继续存在。
- `QueryGroup`：查询至多 500 个账户，分别返回其关联组的不同账户数量，
  以及组内最小账户编号（组标识）。

共享至少一个有效设备指纹的账户属于同一关联组，关联具有传递性；设备指纹
只按完全相等判断。每条查询的结果完全由该查询记录之前仍有效的证据决定：
后续撤销不会改变此前查询的结果，也不会提前影响此前查询。

## 使用

```go
import "devlink"

records := []devlink.Record{
    {Kind: devlink.AddEvidence, EvidenceID: 1, Account: 100, Device: "fp-a"},
    {Kind: devlink.AddEvidence, EvidenceID: 2, Account: 200, Device: "fp-a"},
    {Kind: devlink.QueryGroup, Accounts: []int64{100, 200, 300}},
    {Kind: devlink.RevokeEvidence, EvidenceID: 1},
    {Kind: devlink.QueryGroup, Accounts: []int64{100, 200}},
}

results := devlink.Replay(records)
// results[0].Groups == [{2, 100}, {2, 100}, {1, 300}]
// results[1].Groups == [{1, 100}, {1, 200}]
```

`Replay` 返回的切片与批次中 `QueryGroup` 记录按出现顺序一一对应；
每个 `QueryResult.Groups` 与该查询的 `Accounts` 一一对齐。

## 实现说明

离线动态连通性算法：

1. 第一遍扫描把每条证据的生命周期换算成记录下标区间（新增于第 a 条、
   撤销于第 r 条，则对位于 (a, r) 内的查询有效）。
2. 把每条（账户, 设备）边按有效区间挂到时间轴线段树的 O(log T) 个节点上。
3. 深度优先遍历线段树，进入节点时合并边、离开时回滚（可回滚并查集，
   按大小合并、无路径压缩）；到达叶子即该时刻的有效图，直接回答查询。

复杂度 O((E·log T + Q)·log N)，E 为证据数、T 为记录数、Q 为查询项总数。
反复增删连接两个大组的唯一桥接边时，每次变动只影响 O(log T) 个线段树
节点，天然满足大组反复拆分合并的规模要求。

## 测试

```sh
go test ./...
```

- `replay_test.go`：多条证据逐条撤销、带环关联撤销仍连通、撤销桥接证据
  拆组、历史查询不受后续撤销影响，以及与暴力参考实现全量对照的随机测试。
- `scale_test.go`：规模测试——10 万账户、约 10 万设备指纹、29.8 万条记录、
  100 万查询项，两个 4 万账户的大组经唯一桥反复合并/拆分 2.9 万次。
  本机（Apple Silicon）实测：Replay 耗时约 0.14 秒，运行期峰值内存
  约 148 MiB（含输入与输出），远低于 15 秒 / 512 MiB 的预算。
