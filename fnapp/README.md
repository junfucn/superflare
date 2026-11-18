# SuperFlare fnOS 原生应用打包说明

这个目录存放 SuperFlare 的 fnOS 原生应用打包文件。

## 打包模型

- 包根目录：`fnapp/superflare`
- 包类型：fnOS 原生应用
- 运行模型：原生 Go 二进制，不依赖 Docker
- 不可变程序：`app/server/superflare`
- 打包默认配置：`app/server/defaults`
- 应用文件目录：fnOS `data-share` 注册的 `superflare` 共享目录
- 当前配置存储：共享目录中的 `data/etc/`
- 可变运行数据：共享目录中的 `data/var/`
- 二进制工作目录：共享目录中的 `data/etc/`

安装后在「文件管理 → 应用文件 → superflare」中查看和备份全部应用数据。实际路径读取 fnOS 提供的 `TRIM_DATA_SHARE_PATHS`，不固定存储空间；共享目录不可用时会报错，不会回退到隐藏目录并新建空配置。程序文件和打包默认模板仍由 fnOS 在 `TRIM_APPDEST` 中管理。

| 应用文件中的路径 | 内容 |
| --- | --- |
| `data/etc/` | `.env`、`config.yml`、`apps.yml`、`bookmarks.yml`、`ports.yaml` 及配置备份、恢复文件 |
| `data/var/uploads/` | 上传的背景等资源 |
| `data/var/cache/` | 背景、网站图标等缓存 |
| `data/var/run/` | PID、应用日志和生命周期日志 |
| `data/var/home/`、`data/var/tmp/` | 进程 HOME 和临时文件 |
| `data/.shared-storage` | 共享存储初始化、迁移完成标记 |
| `.superflare-storage.lock` | 共享存储迁移锁 |

SuperFlare 从工作目录读取配置及相对路径 `var/`。生命周期脚本维护 `data/etc/var -> data/var/` 软链接，使所有业务文件均写入应用文件。`.env` 用于端口、登录和 Cookie 等启动项，`.env` 和 `config.yml` 继续限制为仅所有者可读写。fnOS 传入的临时诊断日志仍由系统管理。

## 旧版本升级迁移

升级初始化先停止旧服务，再将原 `TRIM_PKGETC` 和 `TRIM_PKGVAR` 的完整内容复制到共享目录下的临时目录，包括隐藏文件、上传、缓存、日志和备份。复制完成后才原子重命名为 `data/` 并切换读写位置；复制失败不会启用半份数据，也不会删除旧文件。原目录保留为迁移前副本，迁移完成后的配置修改仅写入应用文件，后续升级或重复回调不会重新用旧副本覆盖。

共享目录已有未标记的 `data/` 时会拒绝覆盖；已标记但 `data/etc/` 或 `data/var/` 缺失时会拒绝启动，需先恢复数据。共享目录与旧数据目录必须彼此独立。旧版 `var/runtime/` 中真实目录里的配置在复制后继续按下述规则迁移，原始副本保持不变。

## 配置保留保证

安装、恢复时重放的安装回调以及升级回调都把现有配置视为用户数据。共享目录 `data/etc/` 中的以下五个文件会被保留：

- `.env`
- `config.yml`
- `apps.yml`
- `bookmarks.yml`
- `ports.yaml`

生命周期初始化只补齐缺失的文件和必需键，不会用包内默认值覆盖已经存在的文件或值。安装向导的用户名和密码只会应用于真正的首次安装：共享目录及旧版 `etc/`、真实的 `var/runtime/` 中都没有上述任一配置文件。只要任一位置已有配置，即使只是部分配置，安装回调也会忽略向导中的替换凭据，并只修复缺失项。

共享目录内，旧版真实目录中的配置仅在当前目标文件缺失时迁移。如果 `data/etc/` 与 `data/var/runtime/` 中存在同名冲突文件，两份内容都会保留并记录冲突；软链接形式的旧版目录不会被当作配置目录遍历。若 `data/etc/var` 是真实文件或目录，生命周期脚本会先将它移动到首个可用的 `var.pre-superflare-link`、`var.pre-superflare-link.1` 等保留路径，再创建受管的运行数据链接，已有的同名保留路径也不会被覆盖。

卸载回调只会移除目标精确指向共享目录 `data/var/` 的受管 `data/etc/var` 软链接。配置、上传和其他应用文件、原目录副本以及操作员创建的其他软链接都不会被递归删除；是否保留共享目录由 fnOS 卸载选项管理。必需的配置读取、写入、迁移或链接操作一旦失败，相关生命周期或配置回调会返回失败状态，不会在配置更新未完成时报告成功。

## 构建

Windows 下双击 `fnapp/build-superflare-fpk.cmd` 即可一键打包。脚本会询问是否自增补丁版本号：

- 输入 `Y`：先将 `fnapp/superflare/manifest` 中的版本号末位加一，例如 `0.1.3` → `0.1.4`，再构建。
- 输入 `N`：保留当前版本号，直接构建。

构建完成后会显示结果并等待按键，便于查看日志。自增版本号会直接保存到 `manifest`；构建失败后重试时，可以选择 `N` 避免再次递增。

也可以在仓库根目录执行 PowerShell 脚本，默认保留当前版本号：

```powershell
powershell -ExecutionPolicy Bypass -File .\fnapp\build-superflare-fpk.ps1
```

需要自增补丁版本号时，加上 `-BumpPatch`：

```powershell
powershell -ExecutionPolicy Bypass -File .\fnapp\build-superflare-fpk.ps1 -BumpPatch
```

构建脚本会依次执行：

1. 运行 `go run build/build.go`
2. 运行 `go test ./... -count=1`
3. 运行 `fnapp/test-lifecycle-config-preservation.ps1`，验证安装、恢复、升级和卸载不会破坏持久化配置
4. 将 `config/defaults/` 下的四个 YAML 模板同步到 `app/server/defaults/`，并重建打包源目录中的空 `etc/`、`var/`
5. 交叉编译 Linux `amd64` 版本的 `superflare`
6. 执行 `fnpack build`，成功后将 FPK 文件重命名为带版本号的文件名

配置保留测试在同步默认文件和重置打包源运行目录之前执行；测试失败会立即中止构建，不会继续生成 FPK。仓库级脚本检查 `tools/test-all-scripts.ps1` 也运行同一测试，因此破坏性更新回归会同时阻断常规脚本检查和打包流程。

构建结果：

- `fnapp/superflare/superflare-<版本号>.fpk`，例如 `superflare-0.1.4.fpk`

文件名使用本次构建的 manifest 版本号；选择自增时会使用递增后的版本号。同一版本重新打包会覆盖对应文件，其他版本的包会保留。

## 运行默认值

- 端口：`3636`
- 登录：默认启用
- 首次安装向导的用户名和密码初始值为 `admin` / `admin`，且只会写入真正的首次安装；安装后建议尽快修改
- 编辑页：默认启用
- fnOS 权限：`run-as: root`，用于让端口页在 NAS 上读取完整的系统端口归属名称
- fnOS 应用设置：`系统设置 -> 应用设置` 现已支持修改 `FLARE_USER` / `FLARE_PASS`，会持久化到应用文件的 `data/etc/.env` 并通过自动重启生效

## 说明

- 这个包明确以 fnOS 原生运行方式为目标，不依赖 Docker。
- 打包源目录的 `etc/`、`var/` 保持为空；用户配置只在安装后的 fnOS 持久化目录中初始化，不会嵌入 FPK。
