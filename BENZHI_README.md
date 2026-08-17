# BENZHI_README

## 项目说明

- 项目：11DingKing/go-ecfc00-t012-04
- 项目用途：面向祁连山自然保护区华隆保护站一线巡护场景的后端服务，统一管理巡护班次、巡护装备（无人机、红外相机、卫星电话）与告警工单三类实体，覆盖排班审批、装备申领借还、路线打卡告警、告警派单闭环四条核心工作流。
- Go 工具链：`golang:1.26`
- 前端工具链：无

## 标准构建、运行和测试命令

进入容器后执行：

```bash
# 编译
cd '/app' && GOTOOLCHAIN=local go build ./...

# 启动
cd '/app' && GOTOOLCHAIN=local go run ./cmd/server

# 测试
cd '/app' && GOTOOLCHAIN=local go test ./...
```

## Docker 构建和进入容器

```bash
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh benzhi-task-39-amd64 linux/amd64
./build_benzhi_docker.sh benzhi-task-39-arm64 linux/arm64
docker run -it benzhi-task-39-amd64:latest
docker run -it --platform linux/arm64 benzhi-task-39-arm64:latest
```

## 题目验证命令

1. 预期退出码 1：`go test ./internal/app/ -run "^TestCrossStationClaimWaitsForApprovalWhenEquipmentIsFreed$|^TestCrossStationRequiresApproval$" -count=1 -v`

## Bug 复现

Bug 现象、触发步骤和完整错误信息见 `BUG_REPRO.md`。
