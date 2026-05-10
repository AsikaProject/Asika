# Asika

Asika（[/əˈsiːkə/](https://ipa-reader.com/?text=əˈsiːkə)，读作 *uh-SEE-kuh*）这个名字来自：

- **Akira（明）**：代表“聪明、清晰”
- **seeker**：代表“搜索、观察、发现”

就像一个智能的观察者，Asika 会扫描你的仓库、跟踪 Pull Request、检测垃圾 PR、自动打标签，并帮助你安全地管理整个 PR 工作流。

---

# 为什么选择 Asika？

同时管理多个平台上的 Pull Request 是一件很痛苦的事情。

你需要在 GitHub、GitLab、Gitea、Forgejo、Codeberg 或 Bitbucket 之间不断切换，打开一堆标签页，还要担心：

- PR 是否过早合并
- CI 是否真的通过
- 有没有遗漏重要 Review
- 有没有垃圾 PR 混进来

**Asika 通过统一的控制面板和自动化工作流，让 PR 管理变得简单、安全且高效。**

## 🔹 一个地方管理所有 PR

无需频繁切换标签页。  
在同一个 Dashboard、CLI 或聊天机器人中统一查看和管理多个平台的 PR。

## 🔹 不会“抢跑”的 Merge Queue

内置 Merge Queue。  
只有当 Review 数量、CI 状态等条件全部满足后，PR 才会真正合并。

## 🔹 自动处理重复工作

自动 Label、Stale PR 管理、垃圾 PR 检测等重复工作全部自动完成。

## 🔹 在你原本的工作流里工作

直接通过 Telegram、Slack、Discord 或飞书操作 PR。

## 🔹 简单部署

单文件 Go 二进制。  
不依赖 Node.js，不依赖外部 Git，不需要复杂环境。

---

# 快速开始

## 1. 获取二进制文件

从 Releases 下载：

https://github.com/minibp/asika/releases

或者自行编译：

```bash
git clone https://github.com/minibp/asika.git
cd asika

# 默认构建（strip + 自动版本号）
bash build.sh

# 其它命令
bash build.sh build      # 构建二进制
bash build.sh dep        # 下载依赖
bash build.sh clean      # 清理构建文件
bash build.sh distclean  # 深度清理（包含 Go cache）
```

生成的文件：

- `asika`：CLI
- `asikad`：Daemon

版本号会根据日期自动生成。

---

## 2. 配置

首次使用推荐运行向导：

```bash
./asika wizard
```

或者直接启动 Daemon 并使用 Web 配置界面：

```bash
sudo ./asikad
```

默认 Web UI：

```text
http://localhost:8080
```

最小配置示例（`/etc/asika_config.toml`）：

```toml
[server]
listen = ":8080"

[tokens]
github = "ghp_xxx"

[[repo_groups]]
name   = "my-project"
github = "org/repo"
```

### GitHub Enterprise Server

如果你使用 GitHub Enterprise：

```toml
[server]
github_base_url = "https://github.example.com/api/v3"

[tokens]
github = "ghev_xxx"
```

完整配置请查看：

- `asika.toml.example`

其中包含：

- 通知系统
- Spam 检测
- Label Rules
- Merge Queue
- Chat Bot
- CPU 线程控制（`min_procs` / `max_procs`）
- 热重载等配置

---

## 3. 开始管理 PR

```bash
# CLI
./asika pr list my-project
```

或者打开 Dashboard：

```text
http://localhost:8080
```

---

# Chat Bot

## Slack

在机器人加入的频道中直接发送：

```text
prs my-project
pr my-project 42
approve my-project 42
close my-project 42
queue my-project
```

---

## Telegram

```text
/prs my-project
/pr my-project 42
/approve my-project 42
/close my-project 42
/queue my-project
```

---

## 飞书（Feishu / Lark）

```text
prs my-project
pr my-project 42
approve my-project 42
close my-project 42
queue my-project
```

---

## Discord

支持 Prefix Command：

```text
!prs my-project
!pr my-project 42
!approve my-project 42
!close my-project 42
!queue my-project
```

同时支持 Slash Command：

```text
/prs
/pr
/approve
/close
/queue
```

---

# CLI 速查表

所有命令都需要 Token：

```bash
asika --token <token>
```

或者：

```bash
export ASIKA_TOKEN=xxx
```

## PR 操作

```bash
asika pr list [group]
asika pr show [group] [id]
asika pr approve [group] [id]
asika pr close [group] [id]
asika pr reopen [group] [id]
asika pr spam [group] [id]
asika pr comment [group] [id] [body]
```

---

## 批量操作

```bash
asika pr batch-approve [group] [id1,id2,...]
asika pr batch-close [group] [id1,id2,...]
asika pr batch-label [group] [ids] --label <name>
```

---

## Merge Queue

```bash
asika queue list [group]
asika queue recheck [group]
```

---

## Sync

```bash
asika sync history
asika sync retry [sync_id]
```

---

## 自更新

```bash
asika self-update
asika self-update --check
asika self-update --rollback
asika self-update --dry-run
```

---

## Stale PR 管理

```bash
asika stale check [group]
asika stale unmark [group] [id]
```

---

## 配置

```bash
asika config show
asika config reload
```

---

# 配置亮点

## Repo Groups

### Multi Mode（默认）

同步多个平台：

```toml
mode = "multi"

[[repo_groups]]
name           = "my-project"
github         = "org/repo"
gitlab         = "org/repo"
gitea          = "org/repo"
default_branch = "main"
```

---

### Single Mode

只管理单个平台：

```toml
mode = "single"

[single_repo]
platform       = "github"
repo           = "org/repo"
default_branch = "main"
```

---

# Label Rules

根据文件路径自动打标签：

```toml
[[label_rules]]
pattern = "**/*.go"
label   = "go"

[[label_rules]]
pattern = "docs/**"
label   = "documentation"

[[label_rules]]
pattern = "regex:^.*test.*$"
label   = "has-tests"
```

支持：

- Glob
- Regex
- 热重载

---

# Spam Detection

自动检测垃圾 PR：

```toml
[spam]
enabled  = true
threshold = 3
time_window = "10m"

trigger_on_author = true
trigger_on_similar_title = true
title_similarity_threshold = 0.6

trigger_on_title_kw = [
  "spam",
  "fix typo",
  "readme update"
]
```

---

# Notifications

支持多通知渠道同时工作：

- Email
- Slack
- Discord
- Telegram
- 飞书
- 企业微信
- Microsoft Teams
- DingTalk
- GitHub @mention

示例：

```toml
[[notify]]
type   = "telegram"

config = {
  token = "bot-token",
  to = ["@channel"]
}
```

---

# 贡献

欢迎贡献代码。

首次贡献请先阅读：

- `CONTRIBUTING.md`

---

# License

BSD 3-Clause

详见：

- `LICENSE.md`

---

# Issues

发现 Bug？  
有功能建议？

欢迎提交 Issue：

https://github.com/AsikaProject/asika/issues

---

# 更多文档

- `PROJECT.md`：开发者文档
- `asika.toml.example`：完整配置示例
- `ChangeLog.md`：更新日志
