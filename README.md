# 巡护排班与装备协同平台

面向祁连山自然保护区华隆保护站一线巡护场景的后端服务，统一管理巡护班次、巡护装备（无人机、红外相机、卫星电话）与告警工单三类实体，覆盖排班审批、装备申领借还、路线打卡告警、告警派单闭环四条核心工作流。

## 用途

- **站长**：按周生成并审批巡护班次，管理红外相机数据归档。
- **巡护员**：凭班次申领装备，沿预设路线打卡，上报火情/盗猎告警，支持离线暂存与补传。
- **装备保管员**：核验装备借还与损耗记录。
- **上级管理局调度员**：签收告警、派单跟踪直至闭环，审批跨站借调。

## 端口

服务默认监听 `48235`，可通过环境变量 `PATROL_ADDR` 覆盖。

## 主要接口

### 班次管理
| 方法 | 路径 | 角色 | 说明 |
|------|------|------|------|
| POST | `/api/shifts/generate` | chief | 按周生成草稿班次 |
| POST | `/api/shifts/{id}/submit` | chief | 提交班次待审批 |
| POST | `/api/shifts/{id}/approve` | chief | 审批班次 |
| POST | `/api/shifts/{id}/complete` | chief | 完成班次 |
| POST | `/api/shifts/{id}/cancel` | chief | 取消班次 |
| GET  | `/api/shifts` | any | 列出全部班次 |
| GET  | `/api/shifts/{id}` | any | 查看班次详情 |

### 装备管理
| 方法 | 路径 | 角色 | 说明 |
|------|------|------|------|
| POST | `/api/equipment` | keeper | 登记装备 |
| GET  | `/api/equipment` | any | 列出装备 |
| POST | `/api/equipment/{id}/claims` | officer | 申领装备（自动锁定或候补） |
| POST | `/api/claims/{id}/borrow` | keeper | 核验借出 |
| POST | `/api/claims/{id}/return` | keeper | 核验归还（可报损耗） |
| POST | `/api/claims/{id}/cancel` | any | 取消申领 |
| POST | `/api/claims/{id}/approve-cross-station` | dispatcher | 跨站借调二次审批 |
| GET  | `/api/claims/{id}` | any | 查看申领详情 |

### 巡护打卡
| 方法 | 路径 | 角色 | 说明 |
|------|------|------|------|
| POST | `/api/routes` | chief | 创建预设路线 |
| GET  | `/api/routes` | any | 列出路线 |
| POST | `/api/checkins` | officer | 单次打卡 |
| POST | `/api/checkins/batch` | officer | 离线批量补传 |

### 告警工单
| 方法 | 路径 | 角色 | 说明 |
|------|------|------|------|
| POST | `/api/alerts` | officer | 上报告警 |
| POST | `/api/alerts/batch` | officer | 离线批量补传 |
| POST | `/api/alerts/{id}/sign` | dispatcher | 签收告警 |
| POST | `/api/alerts/{id}/dispatch` | dispatcher | 派单 |
| POST | `/api/alerts/{id}/progress` | dispatcher | 标记处理中 |
| POST | `/api/alerts/{id}/resolve` | dispatcher | 标记已解决 |
| POST | `/api/alerts/{id}/close` | dispatcher | 关闭工单 |
| POST | `/api/alerts/{id}/escalate` | dispatcher | 手动升级 |
| GET  | `/api/alerts` | any | 列出告警 |
| GET  | `/api/alerts/{id}` | any | 查看告警详情 |

### 红外相机归档
| 方法 | 路径 | 角色 | 说明 |
|------|------|------|------|
| POST | `/api/shifts/{id}/archive` | keeper/officer | 当班归档相机数据 |
| GET  | `/api/shifts/{id}/archive` | chief/dispatcher | 调阅归档（仅站长与调度员） |

### 健康检查
| 方法 | 路径 | 说明 |
|------|------|------|
| GET  | `/api/health` | 服务健康检查 |

## 认证方式

通过请求头传递身份信息（生产环境可替换为 JWT/Session）：

```
X-User-ID: <用户ID>
X-User-Role: chief|officer|keeper|dispatcher
X-Station-ID: <站点ID>
```

## 业务规则

1. **出发前两小时锁定**：装备申领时距出发不足 2 小时将被拒绝。
2. **未归还禁领**：存在未归还同类装备的巡护员不得再次申领。
3. **告警五分钟自动升级**：告警 5 分钟内未签收自动升级至站长。
4. **无人机天气限制**：雨雪或六级以上大风禁止无人机出库。
5. **红外数据归档**：当班归档，仅站长与调度员可调阅。
6. **跨站借调二次审批**：跨站申领须经调度员审批后方可锁定。

## 并发冲突处理

两个班次同时申领同一台无人机时，按班次优先级（高>中>低）与提交时间（先到先得）判定唯一归属。落选方进入候补队列，当装备释放时自动顺延晋升，而非直接失败。

## 失败恢复

巡护员在无网络区域可离线暂存打卡与告警，回到有网区域通过 `/api/checkins/batch` 和 `/api/alerts/batch` 批量补传。每条记录携带幂等键（idempotency_key），服务端按原始时间戳重放，确保不重复计次、不丢失记录。

## 启动方式

```bash
go run ./cmd/server
```

服务启动后监听 `:48235`。

## 测试方法

```bash
go test -timeout=120s -count=1 ./...
```

## Docker 构建与运行

```bash
docker build -t patrol-platform .
docker run -p 48235:48235 patrol-platform
```

支持多架构构建：

```bash
docker buildx build --platform linux/amd64,linux/arm64 -t patrol-platform .
```

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `PATROL_ADDR` | `:48235` | 监听地址 |
| `PATROL_STATION_ID` | `hualong` | 默认站点 ID |
| `PATROL_CHIEF_ID` | `chief-001` | 默认站长 ID（告警升级目标） |

## 快速验证示例

```bash
# 健康检查
curl http://localhost:48235/api/health

# 站长生成班次
curl -X POST http://localhost:48235/api/shifts/generate \
  -H "Content-Type: application/json" \
  -H "X-User-ID: chief-001" \
  -H "X-User-Role: chief" \
  -d '{"station_id":"hualong","chief_id":"chief-001","week_start":"2026-03-02T00:00:00Z","assignments":[{"officer_id":"officer-1","route_id":"route-1","departure_at":"2026-03-02T10:00:00Z","priority":2}]}'

# 装备保管员登记无人机
curl -X POST http://localhost:48235/api/equipment \
  -H "Content-Type: application/json" \
  -H "X-User-ID: keeper-1" \
  -H "X-User-Role: keeper" \
  -d '{"name":"DJI-Mavic-3","type":"drone","home_station_id":"hualong"}'
```
