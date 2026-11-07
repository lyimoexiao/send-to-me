# 递我 · Send to Me

一个用 **Go + Vue 3** 写的轻量文件分享工具。拖入文件或写一段文本，设置有效期和可取的设备数，上传后得到 **6 位取件码** 和 **取件链接**。取件者打开链接或输入取件码，确认内容后再点击下载；只有点击下载 / 查看文本才消耗一次次数，且**按终端设备计数**——同一设备重复下载不再扣减。

## 功能

- 文件拖拽、多选上传，文本直传
- 按文件格式显示彩色产品图标（内联 Carbon Icons）：PDF / Word / Excel / PPT / 图片 / 视频 / 音频 / 压缩包 / 代码等一眼可辨
- 过期时间与取件设备数限制（1 / 3 / 5 / 10 / 不限，也可输入自定义数量）
- 上传进度、取件码 / 链接一键复制
- 取件页先展示文件类型、文件名、大小和剩余次数，**打开页面不计数**
- 下载与查看按终端设备计次：同一设备重复取件不再扣减，已取件的设备在次数用完后仍可再次下载
- 文件下载与文本查看由服务端原子扣减次数，并同时校验过期时间
- 相同文件按 SHA-256 复用已存储内容；上传仍需传输文件，多个分享共用一份物理文件
- 多文件自动打包为 ZIP，一次点击只计一次
- 上传时记录发送者 IP、User-Agent，仅落库用于安全审计
- 默认本机文件存储；可切换 S3 兼容对象存储，内置多个云厂商预设
- 取件页在过期 / 次数耗尽后不再向新设备暴露文件名等元数据
- 完整的 CSP、`nosniff`、`no-referrer`、下载 `sandbox` 等安全响应头
- 管理后台 `/admin`：配置上传 / 文本 / 有效期 / 取件设备上限，上传背景图或添加外链背景
- 单二进制部署：Vue 构建产物通过 `go:embed` 打进 Go 程序

## 技术栈

| 层 | 技术 |
| --- | --- |
| 后端 | Go 1.26+，`net/http`，SQLite（`modernc.org/sqlite`，纯 Go） |
| 前端 | Vue 3 + TypeScript + Vite + Vue Router + Element Plus（主题定制） |
| 文件存储 | 本地磁盘 / S3 兼容对象存储（AWS SDK for Go v2） |
| 元数据 | SQLite，文件内容不进数据库 |

## 界面

前端沿用 **MuseTransfer** 的排版与动效语言。主题色随背景图片取色，无法取色时使用科技蓝：

- 桌面端左上角是一张浮动、不可拖拽的染色毛玻璃卡片，与页面 24px 边距对齐；右上角为玻璃导航，右下角可切换背景
- 移动端卡片停靠在屏幕底部（12px 内缩、全宽），背景切换按钮移到左上角
- 跟随系统深浅色：暗色模式使用中性深色毛玻璃，主题色只保留在按钮、徽标等强调元素上
- 卡片展开 / 收起使用 anime.js 弹簧高度动画（`spring({ stiffness: 300, damping: 24 })`），只改变高度、不改变宽度，因此动画过程中文字不会重排
- 自定义次数输入框以缩放 + 位移弹出；按钮按压统一 `scale(0.96)`
- 背景默认使用 Bing 每日一图与 Picsum 无版权图，也可在管理后台上传图片或添加外链；多张背景循环切换，图片加载完成后淡入，切换时 1.1s 交叉淡化
- 文件图标使用 [Carbon Design System](https://carbondesignsystem.com/elements/icons/library/) 的格式图标（Apache-2.0），按扩展名映射颜色：PDF 红、Word 蓝、Excel 绿、PPT 橙、图片青、视频紫、音频玫红等，图标底色取同色淡染
- 所有动效都受 `prefers-reduced-motion` 约束；外链图片域名已加入 CSP 白名单

## 快速开始

```bash
cp .env.example .env
make run
```

打开 <http://localhost:8080>。

`make run` 会先构建前端（`web/dist`），再编译并启动 Go 服务。Makefile 会自动加载 `.env`。也可以手动执行：

```bash
cd web
pnpm install --frozen-lockfile
pnpm run build
cd ..
go run .
```

### 开发模式

两个终端：

```bash
# 终端 1：后端，:8080
go run .

# 终端 2：Vite 开发服务器，:5173，/api 代理到 :8080
cd web
pnpm install --frozen-lockfile
pnpm run dev
```

开发时打开 <http://localhost:5173>。修改前端后由 Vite HMR 热更新；修改后端后重启 `go run .`。注意 `go:embed` 需要 `web/dist` 存在，首次执行前先构建一次前端（仓库中已包含当前构建产物）。

### 测试

```bash
make test        # Go 单元 / 集成测试
make test-web    # Vue TypeScript 类型检查
```

测试覆盖上传 IP 记录、取件元数据、HEAD 预检不计数、下载次数并发上限、按设备计次（同设备重复下载不扣减）、文本取件、多文件 ZIP、过期分享、上传大小限制、各 S3 厂商端点预设，以及用内存假 S3 服务验证 Put / Get / Delete。

## 使用方式

### 发送

1. 拖入文件，或切换到「发送文本」。
2. 选择有效期和可取件设备数。
3. 点击「开始发送」，上传完成后页面会显示取件码和取件链接。
4. 把取件码或链接发给对方。

### 取件

取件者无需注册或登录，直接打开 `http://your-host/p/<code>`，或在首页输入 6 位取件码。

- 打开取件页只读取元数据，**不会下载、也不会计数**。
- 点击「下载文件」/「打包下载全部文件」后，服务端原子扣减一次，再开始传输。
- 文本分享点击「查看文本」后扣减一次，随后可以复制或另存为 `.txt`。
- 多文件分享每次点击下载一个 ZIP，一次计 1 次。

## 配置

环境变量提供启动默认值，完整示例见 [`.env.example`](../.env.example)。上传大小、文件数、文本上限、有效期与背景图可以在管理后台 `/admin` 中修改，保存到数据库后立即生效，并覆盖下表中的同名默认值。

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `ADDR` | `:8080` | HTTP 监听地址 |
| `ADMIN_PASSWORD` | 空 | 管理后台密码；留空时 `/admin` 不可用 |
| `DATA_DIR` | `./data` | 数据目录 |
| `DB_PATH` | `$DATA_DIR/sendtome.db` | SQLite 路径 |
| `PUBLIC_BASE_URL` | 空 | 对外访问地址，用于生成取件链接；反向代理后建议显式设置，如 `https://send.example.com` |
| `TRUST_PROXY` | `false` | 是否信任 `X-Forwarded-For` / `X-Real-IP` / `X-Forwarded-Proto`，仅在可信反向代理后启用 |
| `MAX_UPLOAD_MB` | `2048` | 单次分享的文件总大小上限（MB） |
| `MAX_FILES` | `20` | 单次分享的文件数量上限 |
| `MAX_TEXT_KB` | `64` | 文本分享大小上限（KB） |
| `CLEANUP_GRACE_HOURS` | `24` | 过期后保留元数据的宽限时间，之后清理数据库记录与对象 |
| `STORAGE_DRIVER` | `local` | `local` 或 `s3` |
| `LOCAL_STORAGE_DIR` | `$DATA_DIR/files` | 本地文件目录 |
| `S3_PROVIDER` | `aws` | S3 厂商预设，见下表 |
| `S3_REGION` | 依厂商 | 区域；AWS、MinIO 等有默认值，云厂商通常必填 |
| `S3_BUCKET` | 空 | 桶名，S3 模式必填 |
| `S3_ACCESS_KEY_ID` | 空 | 访问密钥；AWS 可留空以使用默认凭证链 |
| `S3_SECRET_ACCESS_KEY` | 空 | 访问密钥 |
| `S3_SESSION_TOKEN` | 空 | 临时凭证 token，可选 |
| `S3_ENDPOINT` | 依厂商 | 自定义端点 / CNAME；MinIO、custom 必填 |
| `S3_ACCOUNT_ID` | 空 | Cloudflare R2 账户 ID，未设置 `S3_ENDPOINT` 时必填 |
| `S3_PREFIX` | `send-to-me` | 对象 key 前缀 |
| `S3_FORCE_PATH_STYLE` | 依厂商 | 显式覆盖 path-style 设置，`true` / `false` |

### 管理后台

访问 `/admin`，输入 `ADMIN_PASSWORD` 登录，会话保持 12 小时。可配置：

- 单次分享的文件总大小上限、文件数量上限、文本大小上限
- 默认有效期、最长有效期、默认取件设备数
- 背景图：上传 JPG / PNG / WebP / GIF（单张 ≤ 8 MB），或填写 http(s) 外链；最多 30 张

配置保存在 SQLite 的 `settings` 表中，保存后立即对新寄件生效；背景图文件存放在 `$DATA_DIR/backgrounds/`，通过同源 `/media/backgrounds/...` 提供并设置长期缓存。未配置背景时回落到内置的 Bing / Picsum 图源。

后台只支持单管理员密码登录：密码用常量时间比较，登录按 IP 限速（10 分钟内最多 8 次失败），会话 Cookie 为 `HttpOnly` + `SameSite=Strict`，修改类请求还会校验 `Origin`。外链背景的域名会自动加入页面 CSP 的 `img-src` 白名单。

### S3 兼容存储

所有对象存储都通过同一套 S3 协议接入。读取始终经过应用代理，因此桶可以保持私有，下载次数也无法通过对象直链绕过。

| `S3_PROVIDER` | 端点生成方式 | 默认 path-style | 备注 |
| --- | --- | --- | --- |
| `aws` | AWS 自动解析 | 否 | 可省略密钥，使用 IAM Role / 环境变量 / 共享配置 |
| `r2` | `https://<account>.r2.cloudflarestorage.com`，或用 `S3_ENDPOINT` | 是 | 区域默认 `auto`，需 `S3_ACCOUNT_ID` |
| `aliyun` | `https://s3.oss-<region>.aliyuncs.com` | 否 | 例：`S3_REGION=cn-hangzhou` |
| `tencent` | `https://cos.<region>.myqcloud.com` | 是 | 例：`ap-guangzhou`；桶名需带 APPID 后缀 |
| `huawei` | `https://obs.<region>.myhuaweicloud.com` | 是 | 例：`cn-north-4` |
| `qiniu` | `https://s3.<region>.qiniucs.com` | 是 | 例：`cn-east-1` |
| `backblaze` | `https://s3.<region>.backblazeb2.com` | 是 | 例：`us-west-004` |
| `minio` | 必须提供 `S3_ENDPOINT` | 是 | 区域默认 `us-east-1` |
| `custom` | 必须提供 `S3_ENDPOINT` | 是 | 其他任意 S3 兼容服务 |

示例（AWS S3）：

```dotenv
STORAGE_DRIVER=s3
S3_PROVIDER=aws
S3_REGION=us-east-1
S3_BUCKET=my-bucket
S3_ACCESS_KEY_ID=...
S3_SECRET_ACCESS_KEY=...
```

示例（Cloudflare R2）：

```dotenv
STORAGE_DRIVER=s3
S3_PROVIDER=r2
S3_ACCOUNT_ID=your_cloudflare_account_id
S3_REGION=auto
S3_BUCKET=send-to-me
S3_ACCESS_KEY_ID=...
S3_SECRET_ACCESS_KEY=...
```

示例（MinIO）：

```dotenv
STORAGE_DRIVER=s3
S3_PROVIDER=minio
S3_ENDPOINT=http://127.0.0.1:9000
S3_REGION=us-east-1
S3_BUCKET=send-to-me
S3_ACCESS_KEY_ID=minioadmin
S3_SECRET_ACCESS_KEY=minioadmin
```

说明：

- 对象存储走 SigV4，服务端单次 `PutObject`。因此单个对象不能超过厂商的 `PutObject` 限制（AWS S3 为 5 GB），默认上传上限 2 GB。
- 阿里云 OSS 的 S3 兼容端点使用 virtual-hosted 风格；如账号受默认公网 Endpoint 限制，可通过 `S3_ENDPOINT` 指向自定义 CNAME 域名。
- 若厂商要求 path-style 或 virtual-hosted 风格与预设不同，设置 `S3_FORCE_PATH_STYLE=true|false`。
- 非 AWS 厂商需要显式提供 `S3_ACCESS_KEY_ID` / `S3_SECRET_ACCESS_KEY`。

## 计数与数据生命周期

- 元数据 `GET /api/shares/{code}` 不计数。
- 下载预检 `HEAD /api/shares/{code}/download` 只校验状态，不计数。
- 真正的 `GET .../download` 或 `GET .../text` 以终端设备（`stm_device` 长期 Cookie）为准：
  - 首次取件的设备在事务中登记到 `share_devices`，再执行一条带条件的 `UPDATE`：分享未过期、类型匹配、`max_downloads = 0` 或不大于剩余次数，成功后 `download_count = download_count + 1`
  - 已登记的设备直接返回内容，不修改计数；即使分享已被其他设备取完，本设备仍可重复下载
- 条件更新保证并发下不会超发次数。计数在流式传输前完成，符合「点击即计数」的语义。
- 打开过期 / 次数耗尽的分享，API 不再返回文件名、文件大小和文本长度；已取件的设备仍能看到自己的内容。
- 设备标识只用于计次；清除 Cookie 会被视为新设备。
- 文件内容按 SHA-256 去重：`objects` 表记录每份物理对象的引用数，下载与清理都基于引用；同一内容即使文件名、分享不同也只占用一份存储。并发上传相同内容时，后到的重复副本会被自动丢弃。
- 过期清理先删除分享元数据，再释放没有任何分享引用的对象；仍被其他分享引用的文件继续保留。
- 过期分享在 `CLEANUP_GRACE_HOURS` 宽限期后被定时任务删除；本地文件或 S3 对象一并清理。
- 本地 SQLite 文件权限设为 `0600`，数据目录为 `0750`，上传暂存文件为 `0600`。

## API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/api/shares` | `multipart/form-data` 创建分享 |
| `GET` | `/api/shares/{code}` | 取件元数据，不计数 |
| `HEAD` | `/api/shares/{code}/download` | 下载预检，不计数 |
| `GET` | `/api/shares/{code}/download` | 下载文件或 ZIP，计数 1 次 |
| `GET` | `/api/shares/{code}/text` | 查看文本，计数 1 次 |
| `GET` | `/api/config` | 前端可用的限制与背景图列表 |
| `GET` | `/api/health` | 健康检查（含数据库连通性） |
| `GET` | `/media/backgrounds/{name}` | 后台上传的背景图文件 |
| `POST` | `/api/admin/login` | 管理员登录，成功后写入 12 小时会话 Cookie |
| `POST` | `/api/admin/logout` | 退出并吊销当前会话 |
| `GET` | `/api/admin/session` | 查询后台是否开启及当前登录状态 |
| `GET` / `PUT` | `/api/admin/settings` | 读取 / 保存运行时配置 |
| `POST` | `/api/admin/backgrounds` | 按外链（或 Bing / Picsum 图源）添加背景 |
| `POST` | `/api/admin/backgrounds/upload` | `multipart/form-data` 上传背景图 |
| `DELETE` | `/api/admin/backgrounds/{id}` | 删除背景及其上传文件 |

`POST /api/shares` 表单字段：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `kind` | 否 | `files` 或 `text`；缺省时按是否有文件推断 |
| `files` | 文件分享必填 | 可重复，多个文件 |
| `text` | 文本分享必填 | 文本内容 |
| `expires_in` | 否 | 有效秒数；`0` 表示不过期；缺省 7 天 |
| `max_downloads` | 否 | 可取的设备数（每台设备首次下载 / 查看计 1 次，重复取件不计数）；`0` 表示不限；缺省 5 |

示例：

```bash
curl -X POST http://localhost:8080/api/shares \
  -F kind=files \
  -F expires_in=86400 \
  -F max_downloads=3 \
  -F files=@report.pdf
```

错误响应：

```json
{
  "error": {
    "code": "exhausted",
    "message": "这份分享的下载次数已用完"
  }
}
```

## 目录结构

```text
.
├── main.go                 # 服务启动、embed 前端、过期清理
├── internal/
│   ├── config/             # 环境变量
│   ├── model/              # Share / File 模型
│   ├── server/             # HTTP 路由、上传、取件、安全头
│   ├── storage/            # Storage 接口、本地与 S3 实现
│   └── store/              # SQLite 元数据与原子计数
├── web/                    # Vue 3 + Vite 前端
│   ├── src/
│   └── dist/               # 构建产物，被 go:embed 使用
├── .env.example
└── Makefile
```

## 安全说明

- 取件码由 32 字符表随机生成 6 位（约 30 bit），使用 crypto/rand；链接即凭证。
- 生产部署建议在反向代理 / WAF 层增加按 IP 的限流，尤其是上传接口。
- 分享内容以原始字节存储，没有业务层加密；敏感文件请在上传前自行加密。
- 不提供账号体系；任何拿到取件码或链接的人都可以取件。
- 没有病毒扫描能力，请只接收可信来源的文件。
- 反向代理场景请设置 `TRUST_PROXY=true` 并确保代理会覆盖客户端传入的转发头，否则发送 IP 可能被伪造。
- 本机部署建议把 `DATA_DIR` 放在仅服务账号可访问的磁盘上。

## 参考

- [AWS S3 API](https://docs.aws.amazon.com/AmazonS3/latest/userguide/Welcome.html)
- [Cloudflare R2 S3 API](https://developers.cloudflare.com/r2/api/s3/api/)
- [Alibaba Cloud OSS：使用 AWS SDK 访问 OSS](https://help.aliyun.com/en/oss/developer-reference/use-aws-sdks-to-access-oss)
- [Tencent Cloud COS：Accessing COS with AWS S3 SDK](https://www.tencentcloud.com/document/product/436/32537)
- [Qiniu Kodo：S3 兼容 SDK 示例](https://developer.qiniu.com/kodo/12553/S3%20SDK%20sample)
- [Backblaze B2：Call the S3-Compatible API](https://www.backblaze.com/docs/cloud-storage-call-the-s3-compatible-api)
