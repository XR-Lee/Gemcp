# Gemcp 节点部署

**中文** | [English]({{GEMCP_PUBLIC_URL}}/node/setup?lang=en)

本页面是供编码 Agent 在可信自托管 GPU PC 上完成 Owner 审批式部署的完整交接说明。

## 发布身份

- 控制面：`{{GEMCP_PUBLIC_URL}}`
- 要求版本：`v{{GEMCP_VERSION}}`
- 要求提交：`{{GEMCP_COMMIT}}`
- 代码仓库：`git@github.com:XR-Lee/Gemcp.git`

节点二进制与控制面必须使用同一版本。代码仓库为私有仓库，因此 GPU 主机或其编码 Agent 必须独立具备 GitHub 只读权限；Node Setup Link 不授予仓库访问权限。

## Setup Link 安全边界

完整的 `{{GEMCP_PUBLIC_URL}}/node/setup?lang=zh#code=...` 是短期、单次使用的 bearer capability。可选的 `lang` query 只选择说明语言。Setup code 仅存放在 URL fragment 中，获取本 Markdown 页面时不会发送给服务器。

- 不得将完整链接发送到 Web search、Web fetch、issue tracker、shell history、日志或命令行参数。
- 编码 Agent 可以在 enrollment 创建前读取这个公开页面。
- 前置条件和二进制准备完成后，启动安装器，由 Owner 在交互提示中粘贴完整链接。
- 如果 Owner 明确将完整链接交给可信本地 Agent，Agent 只能通过安装器标准输入传递，并在使用后立即丢弃。

## Agent 执行约束

修改主机前，先检查并报告当前状态。除非 Owner 单独批准，否则不得安装或升级 NVIDIA Driver。Gemcp 安装器不会安装或修改 GPU Driver、Docker Engine 或 NVIDIA Container Toolkit。

必须验证以下全部条件：

- Linux x86_64，并使用 systemd。
- 首个版本要求节点恰好有一张受支持的 NVIDIA GPU。
- NVIDIA Driver 和 `nvidia-smi` 正常工作。
- Docker Engine 正在运行。
- NVIDIA Container Toolkit 已正确配置，Docker 可以访问 GPU。
- 已安装 Go 1.26.5，可用于从源码构建。
- `/var/lib/gemcp-node/storage` 可写，并有足够空间保存源码树、输出和完整日志。

如果缺少前置条件、存储空间不足，或修复过程需要变更 Driver，必须暂停并向 Owner 报告，不得继续。

## 构建与校验

在 GPU 主机上执行：

```bash
git clone --branch "v{{GEMCP_VERSION}}" --depth 1 \
  git@github.com:XR-Lee/Gemcp.git Gemcp
cd Gemcp
test "$(git rev-parse HEAD)" = "{{GEMCP_COMMIT}}"
make build-node
./bin/gemcp-node version
file ./bin/gemcp-node
```

版本输出必须包含 `{{GEMCP_VERSION}}` 和预期 commit 前缀；`file` 输出必须确认它是静态链接的 Linux x86-64 可执行文件。

## 安装与 Enrollment

安装器会创建专用 system user，安装静态二进制和 systemd unit，运行诊断，并通过标准输入读取完整 Setup Link，避免把它放入进程参数：

```bash
sudo GEMCP_NODE_STORAGE_ROOT=/var/lib/gemcp-node/storage \
  GEMCP_NODE_BINARY=./bin/gemcp-node \
  ./deploy/install-gemcp-node.sh
```

仅在安装器提示时粘贴完整 Setup Link。安装器只输出 Node ID 和短 pairing code，不会输出 Node Token。向 Owner 报告 Node ID、pairing code、hostname、GPU UUID/型号和 `systemctl status gemcp-node`；不得报告 Node Token。

Owner 必须在 **Nodes -> Enrollment activity** 中核对 pairing code 和硬件，选择授权的 Projects 并批准。在批准前，节点必须保持 `pending_verification`。批准后确认 daemon 变为 active，并持续发送 heartbeat。

## Runtime 配置

对于可信开发主机，低操作成本路径是 **节点 -> 运行时配置 -> 启用工作区**。选择已授权的 Node 和 Project，然后只填写一个规范化的宿主机绝对目录。Gemcp 会根据当前 inventory 自动生成 GPU、CPU、内存、Environment 和 Resource Profile。目录必须已经存在，并且不得与 Gemcp 受管存储根目录重叠。容器通过 `GEMCP_TRUSTED_WORKSPACE` 在 `/gemcp/workspace` 访问它。只有此模式允许使用 public image tag；成功运行后会记录实际解析的 digest。

严格的高级运行时仍用于不可变、接近生产的执行。创建时填写：

在 **Nodes -> Runtime configuration** 中选择 Project，并创建包含以下内容的 runtime：

- 使用 `@sha256:<64-hex-digest>` 固定的 public OCI image reference。
- Profile 接受的准确 GPU 型号；页面会预填节点报告的型号。
- 容器 CPU 和宿主机内存限制。
- 可选设置为 Project 默认 Environment 和 Resource Profile。

Agent 可以通过现有 Project options MCP tool 获取 Environment 和 Resource Profile ID。Self-hosted Experiment 的 CNY 预留为零，但仍受 Project 和全局并发限制。

## 升级已注册节点

已有节点不得重新运行 enrollment 安装器。在 Owner 控制台中打开 **节点 -> 主机 -> 升级指引**，为目标节点复制绑定精确 release 的交接指令，并交给该主机上的可信编码 Agent。交接指令会校验准确 commit、构建静态候选二进制、确认清理前没有遗留的受管 workload container，然后调用 `deploy/upgrade-gemcp-node.sh`。

升级脚本保留 `/etc/gemcp-node/config.json`、`/etc/gemcp-node/credential`、`/var/lib/gemcp-node/state.db` 和受管存储根目录。它只原子替换 `/usr/local/bin/gemcp-node` 并重启现有 systemd service；如果新 daemon 无法持续运行，则恢复 `/usr/local/bin/gemcp-node.previous`。脚本不会注册新节点，也不会输出 Node credential。

## 诊断与运行

```bash
sudo -u gemcp-node /usr/local/bin/gemcp-node doctor \
  --storage-root /var/lib/gemcp-node/storage

sudo systemctl status gemcp-node
sudo journalctl -u gemcp-node
```

节点只通过 outbound HTTPS 连接 `{{GEMCP_PUBLIC_URL}}`，不开放 MCP、SSH 或控制端口。

Workload container 只接收已验证源码树、受管输出目录和固定资源约束；不会接收 Node Token、Attempt Token、Docker socket 或由 Agent 指定的宿主机路径。
