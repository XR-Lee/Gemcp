# 测试说明（Jiyao）

[English](tester-brief.md) · [中文](tester-brief.zh.md)

Linear [XIN-28](https://linear.app/xinrunli/issue/XIN-28/gemcp-development) 请 Jiyao Pu 测试 Gemcp。这是检查清单，不是产品规格。想看产品概览的人从 [README.md](../README.md) / [README.zh.md](../README.zh.md) 开始。

快速开始：测 `main`（当前 `VERSION` 为 `0.20.0`），**不要**用名为 `Jiyao` 的分支。在仓库根目录运行 `./scripts/bootstrap-local.sh`，再 `./scripts/dev-serve.sh`。第二个终端跑 `./scripts/local-http-smoke.sh` 和 `./scripts/local-cpu-loop.sh`（CPU 五条流，不需要 NVIDIA）。不要手抄 `.env.example`（那是生产 Compose 模板），也不要用 Debian/Ubuntu apt 里的 Go 当编译器版本要求。下面是完整步骤。

## 用哪棵树

| 用 | 不要用 |
| --- | --- |
| `main`（当前 `VERSION` 为 `0.20.0`） | 分支 `Jiyao`（停在 v0.15.1；没有 Research / Graph / Lab 分层） |
| 本仓库：`git@github.com:XR-Lee/Gemcp.git`（私有，需要 GitHub 权限） | 公开 Ruby gem `baweaver/gemcp` |
| `./scripts/bootstrap-local.sh` 然后 `./scripts/dev-serve.sh` | 复制 `.env.example`，并只在命令行带 `GEMCP_DATABASE_URL` 去跑 `./bin/gemcp serve` |

检出后确认：

```bash
git fetch --tags origin
git checkout main
git pull origin main
git rev-parse HEAD
cat VERSION
```

`VERSION` 应与当前发布文件一致（这棵树是 `0.20.0`）。运行中的二进制会报该版本加上你构建时的 commit。`alpha-0.19` 是更早的已发布标签，不是本地 HTTP 路径。

## 本地启动（HTTP）

bootstrap 脚本会检查或安装的前置条件：

- 有一条 Go 1.21+ 命令，这样 `go.mod` 钉死的 `1.26.6` 才能下载官方工具链。Debian 13 / Ubuntu apt 里的 Go 只够当引导编译器，不是要求的编译版本。
- Node.js 22+
- `127.0.0.1:5432` 上的 PostgreSQL 16+（Debian 13 apt 17 和 Ubuntu 24.04 apt 16 都可以）。生产 Compose 仍用 PostgreSQL 18。不需要 GPU，也不需要 NVIDIA Container Toolkit。

```bash
./scripts/bootstrap-local.sh
./scripts/dev-serve.sh
```

`bootstrap-local.sh` 用 `deploy/env.local.example` 在仓库根写 `.env`，生成 `GEMCP_MASTER_KEY` 和 `GEMCP_BOOTSTRAP_TOKEN`，创建 `gemcp` 角色和数据库，并构建 `./bin/gemcp`。`gemcp serve` 读取这份 `.env`（进程环境变量仍然优先）。开发时 `GEMCP_AUTO_MIGRATE` 为 true，因此 `/api/v1/setup/status` 不会 500，而 `/healthz` 和 `/readyz` 返回 200。本地模板还会打开 `GEMCP_SCHEDULER_ENABLED`、`GEMCP_SSH_CLOUD_ENABLED` 和 `GEMCP_LOCAL_PROCESS_ENABLED`，这样 CPU 循环可以在 `127.0.0.1` 上调度主机进程，不需要 NVIDIA。开发 serve 在环境变量省略这三项时默认打开它们。如果你已有一份从生产示例抄来的 `.env`（这三项为 `=false`），重新跑 bootstrap——它会把三个开关改回 true——然后重启 serve。

这条路径**不要**复制 `.env.example`。那个文件是生产 Compose 模板（`GEMCP_ENV=production`、`GEMCP_SECURE_COOKIES=true`、只给 Compose 用的 `GEMCP_DATABASE_URL`）。旧文档里只导出 `GEMCP_DATABASE_URL` 的命令不会加载 `.env`，于是 serve 会死在 master-key 占位符上，或者起来但没有迁移。

第二个终端：

```bash
./scripts/local-http-smoke.sh
./scripts/local-cpu-loop.sh
```

`local-http-smoke.sh` 检查 `/healthz`、`/readyz`、`/api/v1/version`、`/api/v1/setup/status`，需要时用 `skip_provider` 做首次设置，并 `POST /mcp` initialize。`local-cpu-loop.sh` 接着跑五条不需要 NVIDIA 的流：登记 compute / environment / dataset（Cloud SSH 回环 + `modelnet40-mini`），经 Gemcp 调度，记下 assumption 与 sub-assumption 的 Graph，等到 `get_experiment` 出现抓取到的 heartbeat，再用 `CLI_DECISION` 把结果拉回 Graph。FLOW5 的 `close_run` 必须成功，且不能编造 Git SHA。打开 `http://127.0.0.1:8080`，使用 `.env` 里的 Owner 邮箱和密码（`GEMCP_DEV_OWNER_*`）。

Compose（`deploy/`）用于带隧道的 HTTPS 主机。不要把 8080 暴露到公网。完成这次 HTTP + MCP 检查不需要 GPU。

## 首次运行与控制台

第一次打开是设置（Owner → Compute → Project），不是登录。如果还没开浏览器，`./scripts/local-http-smoke.sh` 可以做同样的 `skip_provider` POST。

- 粘贴 `.env` 里的 bootstrap token（`GEMCP_BOOTSTRAP_TOKEN`）。
- 在 Compute 里明确跳过 AutoDL。这条路径不需要 GPU 或 Provider token。以后需要时再到 Lab → Provider 添加 AutoDL。
- 复制结尾显示的一次性 Agent Token。Gemcp 不会再显示它。smoke 脚本把它存在 `.gemcp-local-agent-token`（已 gitignore）。

设置之后，主页是 **Research**（Study、计划、Graph）。**Evidence** 列出 Experiment。**Lab** 是 Diagnostics、Finance、Project、Agents、Provider、Alerts。Self-hosted 或 Cloud SSH 打开时才出现 **Nodes**。

顶栏有语言切换（中文 / English）。

## 先试什么（不需要 GPU）

全新本地控制面应能完成这些：

1. 在 Research 创建 Study。在 Graph 上记一个节点**不得**启动作业。
2. 打开 Evidence。从未进入 Graph 的 Experiment 会标成 orphaned。这个标记是故意的。
3. 打开 Lab → Project、Agents、Finance、Alerts、Provider、Images。如果跳过了 AutoDL，Provider 应显示尚未配置；以后保存真实 token 才会打开实时视图。Images 是 Lab 的 bake 工作区：有 `configure` 的 Agent 可以请求 bake，但只有 Owner 确认 digest 才会启动 AutoDL Pro。这棵树的控制面对实时 Pro create 是 fail-closed，因此 Confirm 会在 Lab 记 `failed`，并且不会编造 `image_uuid`。这不是 Graph 或 Experiment 回归。
4. 在 Agents 下载不含密钥的 Owner / Agent 指南。运行中的副本也在 `/docs/owner-mcp.md` 和 `/docs/agent-mcp.md`。
5. 把 MCP 客户端接到 `http://127.0.0.1:8080/mcp`（支持回环 HTTP；生产仍是 HTTPS）。服务器当前注册 **35** 个工具。调用 `get_usage_guide`、`get_research_workspace`、`get_experiment_catalog`、`get_next_actions` 和 `export_research_plan_sync`（dry-run）。本地 CPU 路径上，只有 `read,submit,cancel` 的 token 可以登记回环 compute stub、主机 Environment，以及目录 `modelnet40-mini`。`./scripts/local-cpu-loop.sh` 会做这些，针对 `examples/local-cpu/train.py` 准备 argv，并由 Owner 确认 digest。除非你手里有同一份 Owner 已确认的 digest，否则**不要**自己调用 `submit_prepared_experiment`。`configure` 可以 `request_image_bake`；这次写入零成本，也不会启动 AutoDL Pro。

Grok：`grok mcp list` 和 `grok mcp doctor gemcp-project`。如果 doctor 说目录不受信任，在本目录用顶层 `grok --trust` 授予信任，再跑一次 doctor。没有 `grok mcp doctor --trust` 这个 flag。

不启动服务器的单元 / UI 检查：

```bash
make test
make frontend-test
```

## 打开调度和节点的开关

升级之后这三项故意保持关闭。

| 开关 | 作用 | 额外要求 |
| --- | --- | --- |
| `GEMCP_SCHEDULER_ENABLED=true` | 在已启用的后端上 FIFO 调度新 Experiment | `GEMCP_PUBLIC_URL` 可以是不含凭据的 HTTPS 或回环 HTTP；AutoDL Runner 回调仍要 HTTPS，付费 AutoDL 还要健康的 Watchdog |
| `GEMCP_SELF_HOSTED_ENABLED=true` | Node enrollment、Nodes 控制台、`gemcp-node` | 远程节点需要可达的 HTTPS `GEMCP_PUBLIC_URL`；Linux x86_64 NVIDIA 主机；Node Setup 克隆**精确 commit**，不是 `v0.19.0` |
| `GEMCP_SSH_CLOUD_ENABLED=true` | 实验性的出站 SSH 主机进程后端 | 回环 HTTP 就够，因为控制面自己开 SSH 并轮询进程；没有入站 Runner 回调 |
| `GEMCP_LOCAL_PROCESS_ENABLED=true` | 同一套 Cloud SSH 观察者，但 `127.0.0.1` / `localhost` 以本地 `sh -c` 进程运行 | 需要 Cloud SSH + scheduler；占位密码 `local-process`；不需要 NVIDIA |

本地回环 HTTP 进程可以打开 scheduler 和 Cloud SSH。`config.Load` 在打开 Self-hosted 时也接受回环 HTTP，但远程 `gemcp-node` 无法通过那个回环地址连上控制面。在 `GEMCP_PUBLIC_URL` 成为 HTTPS 之前，AutoDL create 仍然被挡住。

Self-hosted Node Setup：托管页面 `/node/setup`。在 GPU 主机上按页面给出的 commit 执行 `make build-node`。领取之后，Enrollment 行是 `claimed`，同时 Node 的 desired state 是 `pending_verification`；Owner 批准这条已领取的 Enrollment。Trusted workspace 只属于 Owner（一个宿主机目录）。付费 AutoDL 诊断需要明确的 confirmation digest。

## 不要把这些当成回归

这次测试已知未完成或不在范围内：

- **没有 tracing / OpenTelemetry。** 尚未实现。
- **Node 协议仍是 `v1`。** 本仓库没有协议 v2。
- **公开仓库 URL 接入已在这棵树上。** 粘贴 `https://github.com/owner/repository` 或 SSH 表单。公开仓库不需要 Deploy Key 就会激活。私有仓库用只读 Deploy Key + verify；Deploy Key 被禁用时，也可以用只写的 GitHub HTTPS token（`access=https_token`）。之后，Research 空态和仓库对话框会给出就绪报告（访问、默认分支、`gemcp.yaml` 里的名字或没有、缺少的 Environment / Resource Profile / Dataset Binding）。GitHub Deploy Key 主链接是 `/{owner}/{repo}/settings/keys`。token 字段只写。
- **Graph 节点详情只在有请求的 ref 时显示 Branch。** 双击一个不是 Evidence 链接的节点：记录有 `requested_ref` 时侧栏先列 Branch，再列 Commit。只有 commit 的记录仍然显示 Commit。一次活实验如果从未记录 requested ref，**不得**显示仓库默认的 `main`。不要把空的 Branch 再报成缺分支回归。
- **Study 路由 + 计划同步。** 导入或更新 Study 时，协议分支用 `research-plan`，代码 ref 族用 `autoresearch/*`。在 `autoresearch/…` ref 上 prepare；省略 `ref` 或传入 `main` 必须失败。决策或结果之后，Research → Export plan sync（或 MCP `export_research_plan_sync`）返回一份 dry-run markdown 修订。它不是训练，也不会 push git。
- **pending_key 要说真话。** 如果因为 Deploy Key 被禁用而 verify 失败，对话框应显示那条 GitHub 错误，并仍然说明 Graph / catalog 的 observation 写入仍然可以。不要把 `pending_key` 上的 catalog 或 Graph observation 写入再报成回归。
- **这棵树已有 `gemcp.yaml` 命名 workload。** `prepare_experiment` 接受 `workload` 加上类型化 `parameters`，来自已验证 commit 根上的 version-1 manifest。如果那个文件缺失或没有匹配的名字，就用从一次成功的 one-shot 存下来的 Project workload。Owner 可以不拿 Agent Token，从 Evidence 准备，也可以从一次成功的 argv experiment 做 `Save as workload`，而不写 git。Dataset snapshot、build session 和跨节点资产放置仍在后面。
- **Scheduler 关闭时：** 已接受的 prepared 和 Advanced 提交保持 `queued`，不创建计算资源。当前本地 `.env` 模板会打开 scheduler 和 Cloud SSH，这样 `local-cpu-loop.sh` 才能跑完。**Nodes 隐藏：** 只在 Self-hosted 和 Cloud SSH 两个开关都为 false 时才是预期。
- **Provider / Diagnostics / 付费 AutoDL / Images 的实时 Pro create / 两张物理 GPU / Watchdog 挂掉时 / SMTP / `/root/autodl-fs` 持久化** 仍需要已授权的真实资源。没有这些资源时的失败不是产品回归。这棵树上实时 Pro create 保持 fail-closed：Confirm 不得编造 image UUID。

## 怎么报告

有用的报告包括：树（`git rev-parse HEAD` + `./bin/gemcp version`）、怎么启动的（本地 HTTP 还是 Compose）、哪些开关开着、点了什么或调用了哪个 MCP 工具、预期与实际，以及截图或响应体。意外请对着这棵树提，不要对着分支 `Jiyao`。[issue #6](https://github.com/XR-Lee/Gemcp/issues/6) 里的本地 HTTP 坑（没加载 `.env`、apt 的 Go/Postgres 与文档版本不符、MCP HTTPS/26 tools/`--trust`）按本说明做应已消失。[issue #13](https://github.com/XR-Lee/Gemcp/issues/13) 和 [issue #14](https://github.com/XR-Lee/Gemcp/issues/14) 的公开 URL 接入和 Graph 节点详情 Branch 已在这棵树上。
