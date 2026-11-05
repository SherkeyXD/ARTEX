# Harness 框架替换评估方案：从 Norma 到 Pi-go / 轻量级 Agent 架构

## 1. 背景与现状

ARTEX 目前的底层 Agent 驱动依赖于 `github.com/autumn-27/norma` 框架（基于 `agentcore`）：
- **存在的问题**：
  - **框架层抽象过重**：Norma 封装了较多状态与内部结构，外部难以对每轮对话的 Prompt、上下文修剪（Distillation）、缓存友好度进行精细控制。
  - **输出与 Token 控制受限**：工具输出的截断与处理粒度较粗（默认达 30,000 字符），易造成单次调用 Token 膨胀及缓存失效。
  - **架构耦合**：虽然业务侧定义了工具和系统提示词，但运行循环、历史记录截断与事件流深度绑定了 Norma 的特定生命周期。

为了提升系统的掌控力、降低 Token 消耗并消除重型外部依赖，评估迁移至更轻量、可控的 Agent 执行引擎（如 `dimetron/pi-go` 或自研/精简版 ADK Agent 循环）。

---

## 2. `dimetron/pi-go` 架构与可行性分析

### 2.1 项目概述
`dimetron/pi-go` 是一个基于 Go 语言构建的 Agent 运行时与 CLI，核心借鉴了轻量级 Terminal Agent 理念，具有以下特征：
- 结构清晰，核心依托标准的 LLM 交互与 Tool Calling 机制。
- 支持单二进制运行、工具沙箱隔离、多模型后端与 RPC 运行模式。

### 2.2 关键约束：Go `internal` 包限制
在评估将其作为 Go Module 直接 `import` 引用时，存在关键的技术阻碍：
- **Go 编译器内部包限制**：`pi-go` 的核心 Agent 逻辑、会话状态与工具引擎均组织在其仓库的 `internal/` 目录结构下（如 `internal/agent`, `internal/tools` 等）。
- 根据 Go 语言规范，任何外部 Module 尝试 `import ".../internal/..."` 均会被 Go 编译器强行拦截并报错（`use of internal package not allowed`）。

---

## 3. 候选集成方案对比

| 评估维度 | 方案 A：子进程 / RPC Wrapper 模式 | 方案 B：Fork 并暴露 `pkg/` 导出库 | 方案 C（推荐）：提取核心 ADK/Loop 内置化 |
| :--- | :--- | :--- | :--- |
| **实现机制** | ARTEX 编译/打包 `pi-go` 二进制，通过 Stdio / JSON-RPC 子进程通信调用 | Fork `dimetron/pi-go` 仓库，将 `internal/` 改造为 `pkg/` 作为外部依赖引入 | 提炼 `pi-go` 的核心 Agent Loop 理念，在 ARTEX 内部实现极简的 原生 Goroutine 引擎 |
| **进程与性能** | 进程间通信（IPC），频繁序列化与反序列化，存在额外进程与管道开销 | 同进程内运行，Goroutine 级轻量并发 | 同进程内运行，零 IPC 开销，极致性能 |
| **状态共享** | 困难。难以共享 ARTEX 内部的内存缓存、SQLite DB 句柄、任务取消信号（Context） | 较容易。支持内存传参，但需维护跨仓库 Fork 的版本同步成本 | 最佳。无缝接入 ARTEX 的 `db.Database`、`StatusManager`、`EventBus` 等组件 |
| **可定制性** | 受限于 RPC 暴露的接口与消息结构，二次定制需反复修改子进程协议 | 良好，但受限于外部模块发版与依赖管理 | 最高。可针对 ARTEX 特有的增量态势感知、缓存静态化、输出清洗进行全生命周期拦截 |
| **维护成本** | 需管理外部二进制路径、子进程崩溃恢复、跨平台兼容性 | 需长期维护 Fork 分支并同步上游变更 | 一次性提炼极简内核（约几百行核心代码），无外部包锁死风险 |

---

## 4. 推荐落地路径与分步规划

建议采取 **“先解耦抽象，后平滑切换”** 的演进策略，避免破坏现有业务稳定性：

### 第一阶段：工具与状态管理解耦（当前正在进行）
1. **统一工具接口**：保持 ARTEX 自定义的 `AgentTool` / `guardedTool` 规范，不让工具实现直接感知底层 Harness。
2. **状态与提示词解耦**：将态势数据提炼（`DistillOverview`）、系统提示词静态化（Static Prompt）、工具输出清洗（Sanitization）收敛在 ARTEX 自身模块内，而非依赖 Harness 钩子。

### 第二阶段：构建轻量级原生 Agent Runner
1. 参考 `pi-go` 与 Google ADK 的轻量设计模式，在 `agent/` 下设计一套仅包含 `Run(ctx, messages, tools)` 的极简 Agent Loop：
   - 消息循环（User Prompt -> LLM -> Tool Call -> Tool Result -> LLM Loop）。
   - 上下文滑动窗口与 Token 预算感知。
   - 原生支持 ARTEX 的增量态势注入与终止条件判断。
2. 保持与现有 Worker / Planner 的输入输出签名一致，便于无缝替代。

### 第三阶段：双轨并行与验证
1. 编写集成测试，对比新 Agent Runner 与 Norma 在相同任务输入下的 Token 消耗、工具调用准确率与稳定性。
2. 在单元测试和模拟靶场环境下完成回归。

### 第四阶段：下线 Norma 并清理依赖
1. 移除 `agent/worker.go`、`agent/planner.go`、`agent/mainagent.go` 中对 `norma` / `agentcore` 的导入。
2. 从 `go.mod` 中移除 `github.com/autumn-27/norma`，消除重型外部依赖。

---

## 5. 结论

- 不建议使用外部子进程 Wrapper 的方式接入 `dimetron/pi-go`，避免引入不必要的 IPC 复杂度和状态割裂。
- 最优解为：**吸收 `pi-go` 简单直观的 Agent 交互设计思想，基于 Go 原生库与官方 SDK 构建 ARTEX 专用的轻量级 Agent 引擎**，彻底摆脱外部框架束缚，实现对 Token、缓存命中率和上下文生命周期的完全掌控。
