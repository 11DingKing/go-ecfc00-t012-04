# Bug Reproduction

## 包的性质

当前 test_model_fix 保存的是被测模型修复后的结果源码，不是初始含 Bug 源码。要复现原始缺陷，必须检出下面固定的 parent SHA；不要在当前修复结果源码上期待重新出现修复前失败。生成系统使用的可信验证补丁和完整验证日志仅在本地留存，不提交到结果分支。

## 问题现象

跨站借调的二次审批被绕过了，麻烦先帮我们定位原因，暂时不要改代码。这块是管理局明确要求的管控点。

规程：跨站申领（装备归属站和班次所在站不是同一个站）必须经上级管理局调度员审批后才能锁定装备。
另外候补队列的规则是，装备释放时自动顺延晋升下一个候补。

现象：
1. 一台无人机归属 other-station
2. other-station 自己的班次申领它 → 正常锁定（status locked，Won:true）
3. 我们站（hualong）的班次也申领同一台 → 返回 202，"status":"queued"、"cross_station":true、"dispatcher_approved":false，符合预期，等调度员审批
4. 这时 other-station 那个申领被取消了（装备释放）
5. 再查我们站那条跨站申领："status" 已经变成 "locked"，但 "dispatcher_approved" 还是 false —— 没有任何调度员审批过
6. 查装备：无人机 "status":"locked"，locked_by_shift_id 指向我们站的班次
7. 直接 POST /api/claims/{id}/borrow 用保管员身份借出 → 返回 200，无人机就这么被借走了

也就是说，只要等本站的人一释放装备，跨站借调就能自动拿到装备并借出，完全跳过了调度员审批。
如果没有别人先占用这台装备（跨站申领一直挂在队列里），倒是不会出现这个情况，所以一直没被发现。

请先不要修改任何代码，只做定位。我们需要：
- 出问题的具体 Go 文件和具体符号
- 该符号的什么错误行为造成的
- 它为什么会让未经审批的跨站申领在装备释放时拿到装备并可被借出（完整因果机制）
- 你自己实际跑出来的证据（执行了什么命令、看到什么输出）
临时复现程序请放在仓库之外的临时目录，不要改动仓库里的文件。

## 含 Bug 版本

- 仓库：11DingKing/go-ecfc00-t012-04
- 仓库地址：https://github.com/11DingKing/go-ecfc00-t012-04.git
- parent SHA：3d4d50c4d6109b120e720e609143bb032a219f0b

## 复现步骤

```bash
git clone -- https://github.com/11DingKing/go-ecfc00-t012-04.git bug-repro
cd bug-repro
git checkout --detach 3d4d50c4d6109b120e720e609143bb032a219f0b
go test ./internal/app/ -run "^TestCrossStationClaimWaitsForApprovalWhenEquipmentIsFreed$|^TestCrossStationRequiresApproval$" -count=1 -v
```

## 双架构完整错误信息

### linux/amd64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test ./internal/app/ -run "^TestCrossStationClaimWaitsForApprovalWhenEquipmentIsFreed$|^TestCrossStationRequiresApproval$" -count=1 -v
=== RUN   TestCrossStationClaimWaitsForApprovalWhenEquipmentIsFreed
    cross_station_queue_test.go:88: cross-station claim status = "locked", want "queued"
    cross_station_queue_test.go:95: drone status = "locked", want "available"
    cross_station_queue_test.go:98: drone locked by shift "shift-6d10886d5e9e759f", want it not locked
    cross_station_queue_test.go:101: the drone must not be handed over on a cross-station claim that no dispatcher approved
    cross_station_queue_test.go:110: an approved cross-station claim should lock the free drone
--- FAIL: TestCrossStationClaimWaitsForApprovalWhenEquipmentIsFreed (0.01s)
=== RUN   TestCrossStationRequiresApproval
--- PASS: TestCrossStationRequiresApproval (0.00s)
FAIL
FAIL	patrol-platform/internal/app	0.047s
FAIL

```

stderr：

```text
(empty)
```

### linux/arm64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test ./internal/app/ -run "^TestCrossStationClaimWaitsForApprovalWhenEquipmentIsFreed$|^TestCrossStationRequiresApproval$" -count=1 -v
=== RUN   TestCrossStationClaimWaitsForApprovalWhenEquipmentIsFreed
    cross_station_queue_test.go:88: cross-station claim status = "locked", want "queued"
    cross_station_queue_test.go:95: drone status = "locked", want "available"
    cross_station_queue_test.go:98: drone locked by shift "shift-b3df4dacd05005d8", want it not locked
    cross_station_queue_test.go:101: the drone must not be handed over on a cross-station claim that no dispatcher approved
    cross_station_queue_test.go:110: an approved cross-station claim should lock the free drone
--- FAIL: TestCrossStationClaimWaitsForApprovalWhenEquipmentIsFreed (0.00s)
=== RUN   TestCrossStationRequiresApproval
--- PASS: TestCrossStationRequiresApproval (0.00s)
FAIL
FAIL	patrol-platform/internal/app	0.001s
FAIL

```

stderr：

```text
(empty)
```

## 通过条件

目标仓库工作区零改动：git status --porcelain 为空，执行前后 tree hash 一致，生产代码、测试与配置均未被修改。
指出具体 Go 文件与具体符号，并说明该符号的错误行为如何导致题面症状，因果机制完整（含为什么必须有别人先占用装备才会触发）。
给出自己实际运行得到的证据（命令与输出），不能只做静态推断。
结论需与 gold_root_cause 的文件、符号和失效机制一致。
允许在仓库之外的临时目录写一次性复现程序；不产生代码修复提交。
