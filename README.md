# LAN Drop

一个使用 Go 编写的局域网文件快传工具。主机启动服务后，手机或其他电脑直接打开浏览器即可上传和下载文件。

![LAN Drop 桌面界面](docs/ui-desktop.png)

## 功能

- 浏览器拖拽上传和多文件队列
- 二维码连接新设备，无需手输局域网地址
- 实时上传进度、速度、预计剩余时间与取消操作
- 流式写入磁盘，上传时同步计算 SHA-256
- 文件列表、下载、删除和同名文件自动编号
- HTTP Range 下载，支持浏览器续传
- 启动连接码与 HttpOnly、SameSite 会话 Cookie
- 响应式界面、暗色模式与减少动态效果适配
- 单个 Go 二进制，网页资源已嵌入程序

## 快速开始

需要 Go 1.26 或更新版本。

```bash
cd /Users/x/Go/lan-drop
go run ./cmd/lan-drop
```

启动后会自动打开浏览器，终端同时显示连接码和可访问地址：

```text
LAN Drop 已启动
连接码：482901
共享目录：/Users/you/lan-drop/shared
打开：http://192.168.1.20:8080/?token=482901
```

在同一局域网设备中打开终端显示的地址即可。首次打开带连接码的地址会自动建立会话，也可以在页面中手动输入连接码。

已经连接的设备可以点击页面右上角的“连接设备”，让手机或另一台电脑扫描二维码加入。二维码包含当前连接码，只应分享给可信设备。

## 构建

```bash
go build -o lan-drop ./cmd/lan-drop
./lan-drop
```

常用参数：

```text
-addr    监听地址，默认 :8080
-dir     共享目录，默认 ./shared
-max-mb  单文件大小上限，默认 4096 MiB
-token   自定义 4–64 位字母、数字、连字符或下划线连接码；留空时每次启动随机生成
-open    是否自动打开浏览器，默认 true；服务器无桌面环境时使用 -open=false
```

例如：

```bash
./lan-drop -addr :9000 -dir /Users/you/Downloads/LAN-Drop -max-mb 8192
```

## 安全说明

LAN Drop 面向可信局域网。连接码可以阻止同网段中的随意访问，但默认使用 HTTP，传输内容不会加密。不要把监听端口直接暴露到公网；在公共或不可信网络中使用时，应放在带 TLS 的反向代理或 VPN 后面。

服务只会读写指定共享目录中的普通文件。文件名会被规范化，下载和删除接口会拒绝目录穿越路径。

## 验证

```bash
go test -race ./...
go vet ./...
```

测试覆盖连接码会话、上传、SHA-256、同名处理、列表、Range 下载、删除、大小限制和目录穿越防护。项目还使用真实 Chromium 验证了桌面及移动端完整流程。

## 结构

```text
cmd/lan-drop/       命令行入口与局域网地址发现
internal/server/    HTTP API、访问控制和嵌入式网页
internal/storage/   文件存储、校验与元数据索引
docs/               架构图和界面截图
```

[交互式架构图](docs/architecture.html)可以切换明暗主题，并导出 PNG、JPEG、WebP 或 SVG。
