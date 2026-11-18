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

首次生成 `config.yml` 时会保存环境变量或 `.env` 中指定的登录账号；后续启动以 `config.yml` 中已保存的账号为准。

- `/settings`：登录、外观、搜索和其他设置。
- `/settings/seo`：站点地址、描述、关键词、分享图与搜索引擎索引开关。
- `/settings/scripts`：添加 Google Analytics、百度统计等自定义脚本。
- `/editor`：在线编辑应用、书签与配置。
- `/settings/ports`：查看端口发现结果。

修改用户名或密码会使当前会话失效，需要使用新凭据重新登录。禁用登录模式被视为受信任访问，私有项目仍会显示；请只在可信网络中关闭登录。

编辑器下载的 ZIP 备份包含四份配置以及当前使用的上传背景原图。恢复时会先校验配置和图片，再一并替换；旧版 ZIP 或单独的配置文件若引用了本机不存在的上传背景，会拒绝恢复并保留现有配置。备份上传上限为 64 MiB，单份配置上限为 8 MiB，背景原图上限为 32 MiB。

单独上传的 YAML 配置同样受 8 MiB 限制。恢复请求和主题保存请求在解析 multipart 前分别限制为 65 MiB、33 MiB（含表单字段和所有附件），超限返回 HTTP 413；文件本身仍遵守上述大小限制。

编辑器和设置保存也会在写入前校验每份 YAML 的实际大小，超过 8 MiB 时拒绝整次保存并保留原文件。恢复旧版配置缺少登录账号或 `LocalNetworkID` 时，会保留当前账号和实例标识；仅提供用户名或密码之一的备份会被拒绝。

编辑器的“保存全部更改”会一并保存分类、应用和书签，包含当前仍在输入的单元格。端口页切换隐藏项显示不会丢弃备注草稿；离开页面、刷新列表或恢复备份前，会提示尚未保存的修改。

编辑器和端口备注保存会核对加载时的配置版本。其他标签页或本地文件已更新时，旧页面会保留草稿并提示冲突，请先复制要保留的修改，再刷新并合并最新内容。调用 `POST /editor` 或 `POST /settings/ports` 的脚本同样需要携带 `revision`；分别从编辑器页面的同名隐藏字段、`GET /settings/ports/data?includeHidden=1` 的 JSON 获取。请求时使用 `Accept: application/json`，成功保存后使用 JSON 响应中的新 `revision`。缺少版本或版本过期返回 HTTP 409；端口备注接口仅接受列表，传入 `ports=[]` 和 `includeHidden=1` 可显式清空全部已保存项。

恢复和主题上传请求有 5 分钟的传输时间，响应期限比传输期限多 30 秒；其他接口保留原有短超时。反向代理部署时也需为上传设置相应的请求体大小和超时。保存已有上传背景地址时会检查本地图片是否存在；已保存的背景文件若丢失，页面会使用主题底色并提示重新上传。

会话会校验当前账号凭据，修改用户名或密码、恢复包含不同凭据的配置后，所有旧登录会话均需重新登录。升级至带有凭据校验的版本时，旧格式 Cookie 也需要重新登录一次；仅恢复应用、书签或端口配置不会改变登录凭据。

登录接口只接受 `application/x-www-form-urlencoded` 表单，请求体上限为 16 KiB，拒绝 multipart 文件上传。登录、退出和管理写请求会校验浏览器的 `Sec-Fetch-Site` / `Origin`，同站点其他子域的请求也会被拒绝；关闭登录时管理写请求仍受此保护。反向代理应保留浏览器访问的 `Host` 以及这两个来源头。没有来源头的非浏览器客户端仍可使用原有接口。

动态书签和 SEO 的协议推断只接受可信代理传来的 `X-Forwarded-Proto` 或 `Forwarded: proto=`；SEO 的 `X-Forwarded-Host` 使用同一信任规则。默认信任回环地址；其他代理通过环境变量或 `.env` 中的 `FLARE_TRUSTED_PROXIES`，或命令行 `--trusted_proxies` 指定逗号分隔的 IP/CIDR，例如 `127.0.0.1,::1,172.18.0.2/32`。显式设置会替换默认列表，`none` 表示全部禁用。请填写与 SuperFlare 直接建立连接的代理地址，并让代理覆盖客户端传入的转发头、保留外部 `Host`（含非默认端口）。

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

在支持 POSIX 权限的系统上，创建、保存和恢复 `config.yml`、`.env` 时最多授予 `0600` 权限；已有更严格权限会保留，普通配置文件的保存也不会放宽原有权限。Windows 的文件访问权限由 ACL 管理。

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

### 端口采集

端口采集默认开启，仅在打开或刷新端口设置页时执行，没有后台定时采集。可在「设置 → 端口」取消勾选「启用端口采集」并保存，立即对后续请求生效，无需重启。

也可在 `config.yml` 中关闭：

```yaml
DisablePortCollection: true
```

省略此项或设为 `false` 时开启采集。关闭后不会读取系统端口、进程信息或调用相关系统命令，端口页仅展示已保存的备注和隐藏设置，运行状态显示「未采集」；这些数据仍可编辑且不会被清空。

### 内网书签访问

在「设置 → 界面设置」可填写 **SuperFlare 内网访问地址**，也可在 `config.yml` 设置 `LocalNetworkURL`。填写的是部署本项目的内网入口，支持 HTTP 和 HTTPS，例如 `http://192.168.0.10:3636`，不包含路径。这个入口应只在你的内网或 VPN 中可达，不要填写同样能从公网访问的入口。

SuperFlare 首次启动会生成并保存 `LocalNetworkID`。浏览器通过 `/api/local-network/identity` 获取该实例的公开随机标识，匹配后才将带 `local_link` 的应用、收藏和书签切到内网地址。不按固定 IP 网段排除 VPN 或跨网段访问，也不把任意服务的 HTTP 响应当作身份确认。识别端点不返回配置、密码或书签；它支持不带凭据的跨域读取和本地网络预检。

「设置 → 界面」提供「自动 / 内网 / 外网」访问模式，选择后立即保存在当前浏览器的当前站点；内网识别状态和重新检测按钮也放在这里。首页在后台完成识别，不显示访问模式切换或书签地址菜单。自动模式只探测上述固定入口，成功结果缓存 30 秒，未确认结果缓存 5 秒，页面重新激活、恢复联网或浏览器报告网络变化时重新检测。

「设置 → 界面设置」中的「内网未确认时直接打开外网链接」默认开启。自动模式下，点击时若还没确认内网，会直接打开外网链接，跳过等待页；确认后仍优先打开内网链接。关闭此选项（`LocalNetworkDirectFallback: false`）后，首次点击尚未完成检测的链接会进入等待页，最多等待约 1.2 秒再回退到外网。此选项不影响手动选择的「内网」或「外网」模式。

留空表示不固定内网地址。使用私有 IP、localhost、`.local`、`.lan` 或 `.home.arpa` 等内网地址打开 SuperFlare 时，自动使用当前入口，设置页也会显示该地址，不会写入配置。通过公网入口打开时，SuperFlare 不会猜测内网地址；需要从公网入口检测内网时，可手动填写实际地址。HTTPS 页面也会尝试检测 HTTP 内网入口，是否能检测取决于浏览器的混合内容规则和本地网络访问权限。检测被拦截时仍可手动选择「内网」模式。确认入口可达不等于各项服务都在线；服务停止时可在设置内切换到外网模式。

实例标识用于避免误连其他同网段设备，不是身份认证凭据。克隆整套配置到另一套独立部署时应删除副本的 `LocalNetworkID`，让它重新生成。统一域名与内外网分流 DNS 仍可正常使用。

## 图标与网络访问

可为项目配置图标 URL 或内置 MDI 图标名；未配置时，SuperFlare 会尝试使用站点 favicon。图标请求、公开链接检查和页面访问都依赖当前网络条件及目标站点响应，无法保证外部资源始终可用。favicon 获取失败、缓存无效或浏览器加载错误时，界面会使用内置图标回退，避免导航项失去可识别的图标。

站点图标下载优先使用 `HTTP_PROXY` / `HTTPS_PROXY` 环境变量，并遵守 `NO_PROXY`。Windows 下未配置对应环境变量时，会使用当前用户启用的系统手动代理及其绕过规则；内网 IP 保持直连。Linux、Docker 和 fnOS 可通过环境变量配置代理。

图标代理仅处理当前访问者可见书签的站点 favicon 或显式配置的图标 URL；配置中的内网站点仍可取图标，但重定向和图标发现不能跳到另一个未配置的内网服务。代理不会展开来自请求 Host 的动态 URL 占位符，这类站点可填写固定的图标 URL。背景图代理仅处理当前配置的背景地址，图片响应隔离脚本执行；位图解码限制为 16,777,216 像素，最多同时处理两张图片。

图标和背景图片下载会将连接固定到本地校验后的 IP。使用 HTTP(S) 代理时，代理须支持向目标端口建立 CONNECT 隧道（HTTP 图片地址也需要）；SOCKS 代理同样使用已校验的 IP，不再由代理重新解析目标域名。

背景图下载及其重定向还会拒绝共享地址段 `100.64.0.0/10`、保留地址和 IPv4 转换地址段；图标跳转到非原始配置服务时使用相同的公网地址规则。

一次图标下载中的同站点重定向、HTML 图标发现和回退请求会复用该站点首次解析的 IP 集合，避免 DNS 变化把后续请求转向其他内网服务。

公开链接检查只访问经过 DNS 校验的公网 HTTP(S) 地址，并对每次重定向重新校验。连接会固定到已验证的 IP，内网、回环、链路本地和保留地址不会被探测。链接检查遵守 `HTTP_PROXY` / `HTTPS_PROXY` 和 `NO_PROXY`；使用 HTTP(S) 代理时需要代理支持向目标端口建立 CONNECT 隧道（HTTP 链接也需要），以避免代理重新解析目标域名。

## 开发与校验

回归测试和性能基准保留在源码中，供后续修改时使用，不会编入发布程序。部署说明见上方“部署方式”，本地工具说明见 [`.tools/README.md`](.tools/README.md)。

修改嵌入的静态资源或模板后，先生成资源，再运行测试和构建：

```bash
go run ./build/build.go
go test ./... -count=1
go build ./...
go vet ./...
```

Windows 未将 Go 加入 PATH 时，将 `go` 替换为 `& .\.tools\go\bin\go.exe`。

Windows 可额外执行仓库脚本校验，需要 Git Bash 和 Node.js，包含 fnOS 回归、脚本安全及站点图标 JavaScript 测试：

```powershell
powershell -ExecutionPolicy Bypass -File .\tools\test-all-scripts.ps1
```

清理时可删除可重新生成的二进制、FPK、覆盖率报告、性能采样和测试临时文件。`build/` 是资源生成器源码，`internal/` 中的嵌入资源用于编译和运行；这些文件、本地工具链及上方列出的运行数据应保留。`.gitignore` 也忽略了运行配置和工具链，不要按忽略规则批量删除文件。

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
