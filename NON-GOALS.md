# Non-goals

[English](#english) · [中文](#中文)

## English

Gemcp is an **experiment authorization gate** for AI coding agents: an agent can only *prepare* a run; a human confirms one exact digest before any budget is reserved; the result is written back onto a scientific Graph. Provider tokens and compute stay in your own Lab.

That sentence sits next to five better-known categories. Gemcp is none of them, and the differences are enforced in the product, not just stated here.

| Gemcp is not… | Because | In this repository |
| --- | --- | --- |
| **MLflow / Weights & Biases** — an experiment tracker | A tracker records runs you already started; Gemcp decides whether a run may start at all, and a result exists only because `close_run` wrote one. | [graph-contract.md#spend-rules](docs/graph-contract.md#spend-rules) · [graph-contract.md#close_run](docs/graph-contract.md#close_run) |
| **LangGraph** — an in-process agent framework | LangGraph orchestrates steps inside your process; Gemcp's Graph is a cross-process contract checked on the server. Harnesses and coding agents stay outside; MCP is the only door. | [transparency-authorization.md#what-this-is-not](docs/transparency-authorization.md#what-this-is-not) · [roadmap.md](docs/roadmap.md) |
| **ClearML / Slurm / Ray** — a job queue or cluster scheduler | A queue abstracts compute into interchangeable capacity; Gemcp's backends deliberately never fall back to one another, because a silent backend swap would make a confirmed digest lie. | [self-hosted-nodes.md#scheduling-and-execution](docs/self-hosted-nodes.md#scheduling-and-execution) · [AGENTS.md](AGENTS.md) |
| **A research harness** — an "agent does science" loop | Those systems *are* the research loop; Gemcp is the boundary a loop runs against — it may prepare, observe, and close, but it cannot authorize its own spending. | [node-experiment-harness.md#decision](docs/node-experiment-harness.md#decision) · [node-experiment-harness.md#autonomy-boundary](docs/node-experiment-harness.md#autonomy-boundary) |
| **A public science network or marketplace** | There is no shared feed, no social graph, and no capacity market. A Gemcp instance is private to one lab, and agents never receive Provider, Node, or Git credentials. | [transparency-authorization.md#what-this-is-not](docs/transparency-authorization.md#what-this-is-not) · [node-experiment-harness.md#why-it-stays-separate](docs/node-experiment-harness.md#why-it-stays-separate) |

Three narrower non-goals that follow from the same boundary:

- **Not a general artifact or model store.** `list_artifacts` and `read_artifact` expose registered names and bounded control-plane reads, not a download channel — see [node-experiment-harness.md#result-manifest](docs/node-experiment-harness.md#result-manifest).
- **Not a metric inference engine.** Result scalars are projected from `${GEMCP_OUTPUT_DIR}/metrics.json`; `close_run` rejects a scalar that is absent from or differs from the terminal Experiment metrics. Nothing is parsed out of logs — see [node-experiment-harness.md#observation](docs/node-experiment-harness.md#observation).
- **Not a git write path.** Plan sync exports a patch; a human or agent applies it on the docs-only protocol branch — see [graph-contract.md#what-this-is-not](docs/graph-contract.md#what-this-is-not).

A `submit` scope is technical capability, not financial approval. Standing approval and unattended spending are explicitly out of scope until an Owner-approved automation lease exists ([node-experiment-harness.md#autonomy-boundary](docs/node-experiment-harness.md#autonomy-boundary)).

Shared vocabulary lives in [docs/graph-contract.md](docs/graph-contract.md). A one-page picture of who does what: [docs/framework-map.html](docs/framework-map.html).

---

## 中文

Gemcp 是给 AI coding agent 用的**实验授权闸机**：agent 只能*准备*一次运行；人确认一条精确 digest 之后才会预留预算；结果按科学 Graph 写回。Provider Token 和算力留在你自己的 Lab 里。

这句话旁边站着五个更有名的品类。Gemcp 都不是，而且这些区别是产品强制的，不只是这里写一句。

| Gemcp 不是… | 差在哪 | 仓内依据 |
| --- | --- | --- |
| **MLflow / Weights & Biases** —— 实验追踪 | 追踪工具记录你**已经跑过**的；Gemcp 决定一次运行**能不能开始**，而 result 只因为 `close_run` 写了才存在。 | [graph-contract.md#spend-rules](docs/graph-contract.md#spend-rules) · [graph-contract.md#close_run](docs/graph-contract.md#close_run) |
| **LangGraph** —— 进程内 agent 框架 | LangGraph 在你的进程里编排步骤；Gemcp 的 Graph 是跨进程、服务端校验的合约。Harness 和 coding agent 留在外面，MCP 是唯一的门。 | [transparency-authorization.md#what-this-is-not](docs/transparency-authorization.md#what-this-is-not) · [roadmap.md](docs/roadmap.md) |
| **ClearML / Slurm / Ray** —— 作业队列或集群调度 | 队列把算力抽象成可互换的容量；Gemcp 的后端**刻意不互相 fallback**，因为悄悄换后端会让一条已确认的 digest 撒谎。 | [self-hosted-nodes.md#scheduling-and-execution](docs/self-hosted-nodes.md#scheduling-and-execution) · [AGENTS.md](AGENTS.md) |
| **科研 harness** —— 「让 agent 做科研」的循环 | 那些系统**就是**研究循环；Gemcp 是循环所面对的边界——可以准备、可以观察、可以收尾，但不能自己批自己的钱。 | [node-experiment-harness.md#decision](docs/node-experiment-harness.md#decision) · [node-experiment-harness.md#autonomy-boundary](docs/node-experiment-harness.md#autonomy-boundary) |
| **公开科学网络或算力市场** | 没有公共信息流、没有社交图、没有算力市场。一个 Gemcp 实例属于一个实验室，agent 拿不到 Provider、节点或 Git 凭据。 | [transparency-authorization.md#what-this-is-not](docs/transparency-authorization.md#what-this-is-not) · [node-experiment-harness.md#why-it-stays-separate](docs/node-experiment-harness.md#why-it-stays-separate) |

同一条边界还推出三个更窄的「不做」：

- **不是通用 artifact / 模型仓库。** `list_artifacts` 和 `read_artifact` 只暴露已注册的文件名和受限的控制面读取，不是下载通道——见 [node-experiment-harness.md#result-manifest](docs/node-experiment-harness.md#result-manifest)。
- **不从日志里猜指标。** 结果标量从 `${GEMCP_OUTPUT_DIR}/metrics.json` 投影上来；`close_run` 会拒绝一个不在终态 Experiment metrics 里、或与之不符的标量——见 [node-experiment-harness.md#observation](docs/node-experiment-harness.md#observation)。
- **不是 git 写入路径。** Plan sync 导出一个 patch，由人或 agent 在 docs-only 的协议分支上应用——见 [graph-contract.md#what-this-is-not](docs/graph-contract.md#what-this-is-not)。

`submit` 权限只是技术能力，不是财务批准。在 Owner 批准的自动化租约存在之前，长期授权和无人值守花钱明确不在范围内（[node-experiment-harness.md#autonomy-boundary](docs/node-experiment-harness.md#autonomy-boundary)）。

统一用词见 [docs/graph-contract.md](docs/graph-contract.md)。一页看懂谁对谁做什么：[docs/framework-map.html](docs/framework-map.html)。
