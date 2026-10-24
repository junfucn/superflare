# SuperFlare

SuperFlare 是一个轻量级、自托管的导航首页，基于 [soulteary/docker-flare](https://github.com/soulteary/docker-flare) 演进而来。它使用本地 YAML 保存导航数据，适合个人主页、家庭实验室和内网服务入口。

## 功能亮点

- 管理应用和普通书签；应用、书签都可标记为私有，普通书签可单独设为收藏。
- 书签描述会以提示信息展示；带 `subdir` 的应用可在子目录弹窗中按分类访问。
- 提供搜索、主题与背景等外观设置、多语言界面、在线编辑，以及配置备份和恢复。
- 可检查公开链接、发现本机端口并在端口页查看结果，便于整理服务入口。
- 站点图标支持显式图标、站点 favicon 和内置图标回退；外部图标请求可能因网络或目标站点限制失败，失败时会回退到可用的内置图标。
- 内置面向搜索引擎与 AI 的发现能力：可配置站点地址、描述、关键词与分享图，自动输出 canonical、Open Graph、Twitter Card、JSON-LD 结构化数据，并提供 `/robots.txt`、`/sitemap.xml`、`/llms.txt`。

## 部署方式

| 方式 | 适用场景 | 入口 |
| --- | --- | --- |
| 源码运行 | 本地体验与开发 | 本 README 的“快速开始” |
| Docker | 容器化部署 | [`tools/docker/README.md`](tools/docker/README.md) |
| Linux 原生 | 二进制与 systemd | [`tools/linux/README.md`](tools/linux/README.md) |
| Windows | PowerShell 构建与后台运行 | `tools/windows/restart-superflare.ps1` |
| fnOS | NAS 原生 FPK | [`fnapp/README.md`](fnapp/README.md) |

## 快速开始

需要 Go `1.26.x`。在仓库根目录依次执行：

```bash
go run ./build/build.go
go run .
```

默认访问地址：

```text
http://127.0.0.1:3636/
```

首次启动会补齐缺失的运行配置；随后可登录管理或直接编辑本地 YAML 文件。

## 登录与管理入口

默认用户名和密码均为 `admin` / `admin`，首次登录后应立即修改。生产环境请配置稳定且足够随机的 `FLARE_COOKIE_SECRET`，避免重启、容器重建或密钥变更导致会话失效。

- `/settings`：登录、外观、搜索和其他设置。
- `/settings/seo`：站点地址、描述、关键词、分享图与搜索引擎索引开关。
- `/settings/scripts`：添加 Google Analytics、百度统计等自定义脚本。
- `/editor`：在线编辑应用、书签与配置。
- `/settings/ports`：查看端口发现结果。

修改用户名或密码会使当前会话失效，需要使用新凭据重新登录。禁用登录模式被视为受信任访问，私有项目仍会显示；请只在可信网络中关闭登录。

## 自定义脚本

在“设置 → 脚本”中粘贴统计平台提供的完整代码（包含 `<script>` 标签），可同时添加多段脚本。保存后立即生效，代码保存在 `config.yml` 的 `CustomScripts` 字段中；清空后保存即可停用。

脚本会插入首页、应用、书签、帮助及站内搜索结果页面的 `<head>`，设置和编辑页面不会加载。系统自动为脚本添加当前页面的 CSP nonce，并允许受信任脚本加载其依赖，无需关闭 CSP。自定义脚本可以访问访客页面数据，请仅添加可信来源的代码。

## SEO 与 AI 可发现性

`/settings/seo` 中的设置会同时作用于页面头和公开发现文件：

- 站点地址（`SiteURL`）：用于生成 canonical、Open Graph `og:url` 与 sitemap 的绝对地址。反向代理部署时建议显式填写，否则按请求的 `X-Forwarded-Proto` / `Host` 推断。
- 描述、关键词、作者与分享图：留空时使用按页面类型区分的内置中英文文案。
- 索引开关（`SEONoIndex`）：开启后所有页面输出 `noindex, nofollow`，`robots.txt` 直接 `Disallow: /`。

默认输出的发现文件：

| 路径 | 内容 |
| --- | --- |
| `/robots.txt` | 放行公开页面、屏蔽 `/settings`、`/editor` 等管理路径，并让训练型爬虫（GPTBot、ClaudeBot、CCBot 等）退出抓取 |
| `/sitemap.xml` | 首页、应用、书签、帮助四个公开页面 |
| `/llms.txt` | 面向大模型的站点摘要与页面索引（不会列出私有书签） |

页面还会输出 `WebSite`、`Organization`、`SoftwareApplication`、`WebPage`、`BreadcrumbList`、`ItemList` 组成的 JSON-LD，使不执行 JavaScript 的抓取端也能理解页面内容。当 `FLARE_VISIBILITY=PRIVATE` 或登录被禁用时，应用与书签页不进入 sitemap，且整站按 `noindex` 处理。

## 运行数据与升级

以下文件和目录是持久化运行数据：

- `config.yml`
- `apps.yml`
- `bookmarks.yml`
- `ports.yaml`
- `.env`（可选）
- `var/`

原生部署会从进程工作目录解析这些路径，因此二进制、配置和 `var/` 应放在同一运行目录。Docker 必须持久化 `/data`，其中包含上述所有数据；复用同一个 `/data` 后，镜像升级或容器重建不会覆盖已有配置。

fnOS 将配置保存在可写的 `etc/`，运行数据保存在 `var/`。升级时会保留已有配置，只补齐缺失文件或必要键，不会用包内默认值覆盖现有值。

## 配置示例

应用写入 `apps.yml`：

```yaml
# apps.yml
links:
  - name: NAS
    link: https://nas.example.com
    local_link: http://192.168.1.10:5000
    subdir: 存储服务
    private: true
```

普通书签和分类写入 `bookmarks.yml`：

```yaml
# bookmarks.yml
categories:
  - id: "1"
    title: 开发
links:
  - name: SuperFlare
    link: https://github.com/junfuchang/superflare
    category: "1"
    desc: 项目仓库
    favorite: true
    private: false
```

- `local_link`：优先用于内网或本地网络访问的地址。
- `subdir`：应用会按子目录显示在文件夹模态框中；普通书签会在所属分类内按子目录折叠分组。
- `desc`：应用描述显示在应用卡片中；普通书签描述在鼠标悬停或键盘聚焦时显示为 tooltip。
- `favorite`：将普通书签加入收藏区；该字段不用于应用。
- `private`：仅在已登录时显示的应用或书签；禁用登录时视为受信任访问，因此仍可见。

## 图标与网络访问

可为项目配置图标 URL 或内置 MDI 图标名；未配置时，SuperFlare 会尝试使用站点 favicon。图标请求、公开链接检查和页面访问都依赖当前网络条件及目标站点响应，无法保证外部资源始终可用。favicon 获取失败、缓存无效或浏览器加载错误时，界面会使用内置图标回退，避免导航项失去可识别的图标。

站点图标下载优先使用 `HTTP_PROXY` / `HTTPS_PROXY` 环境变量，并遵守 `NO_PROXY`。Windows 下未配置对应环境变量时，会使用当前用户启用的系统手动代理及其绕过规则；内网 IP 保持直连。Linux、Docker 和 fnOS 可通过环境变量配置代理。

## 开发与校验

修改嵌入的静态资源或模板后，先生成资源，再运行测试和构建：

```bash
go run ./build/build.go
go test ./... -count=1
go build ./...
```

Windows 可额外执行仓库脚本校验，需要 Git Bash 和 Node.js，包含 fnOS 回归、脚本安全及站点图标 JavaScript 测试：

```powershell
powershell -ExecutionPolicy Bypass -File .\tools\test-all-scripts.ps1
```

## 仓库结构

```text
build/      资源生成工具
config/     配置模型与默认配置
embed/      模板和静态资源
internal/   应用主体代码
fnapp/      fnOS 原生应用打包文件
tools/      Docker、Linux、Windows 等部署与校验脚本
var/        运行时可变数据
```

## 致谢

感谢 [soulteary/docker-flare](https://github.com/soulteary/docker-flare) 提供的开源基础，以及所有参与测试、反馈和维护的贡献者。
