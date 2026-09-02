# bigbang for Samsung

bigbang 的原生 Android 客户端，面向三星 One UI 设备设计，同时兼容 Android 10 及以上系统。它连接电脑上运行的 Go 服务，可查看实时文件列表、选择文件上传，并通过三星系统相机拍照后预览上传。

## 使用

1. 在电脑上启动 bigbang，记下局域网地址和连接码。
2. 安装 APK，手机与电脑连接同一个局域网。
3. 输入 `http://192.168.x.x:8080` 和连接码；Android 17 首次连接时允许“附近设备”访问。
4. 点击“拍照”，确认预览后再上传；也可以选择已有文件。

应用不会申请相机或媒体库权限。拍照通过系统 `TakePicture` 合约启动三星相机，并用非导出的 `FileProvider` 临时授权一个缓存文件 URI；用户确认后才上传。服务器地址会保留，连接码只存在于当前进程内，不会明文落盘。

> 默认局域网传输使用 HTTP，连接码和文件内容不会被加密。只应在可信网络中使用；公网地址必须使用 HTTPS。

## 构建

需要 JDK 17、Android SDK Platform 37，以及可执行权限完整的 Gradle Wrapper：

```bash
cd android
./gradlew testDebugUnitTest lintDebug assembleDebug assembleDebugAndroidTest
```

调试 APK 位于 `app/build/outputs/apk/debug/app-debug.apk`。

## 技术设计

- Kotlin、Jetpack Compose、Material 3，状态由 `ViewModel + StateFlow` 单向驱动。
- `ActivityResultContracts.TakePicture` 复用系统相机，不自行持有相机权限。
- `ContentResolver` + 64 KiB 缓冲流式上传，不把整张照片或大文件读入内存。
- SSE 只作为状态失效通知，断线后退避重连，权威状态始终来自 `/api/files`。
- 仅允许私网地址使用 HTTP；公网主机在客户端策略层强制 HTTPS。
- API 请求不自动跟随重定向，避免连接码被带到另一个主机。
- Android 17 按需申请 `ACCESS_LOCAL_NETWORK`，旧系统不出现多余权限弹窗。

[移动端交互式架构图](../docs/samsung-app-architecture.html)展示了拍照、权限、上传与实时同步的完整边界。

## 面试时可以展开

- 为什么选择系统相机合约，而不是申请 `CAMERA` 后自建 CameraX 页面。
- FileProvider 如何避免暴露真实文件路径，临时 URI 授权的生命周期是什么。
- 为什么凭据不落盘，以及局域网 HTTP 的威胁模型和 HTTPS 演进方案。
- 协程取消为什么必须重新抛出 `CancellationException`，否则会出现假失败提示。
- SSE 为什么允许丢中间事件，以及如何以一次列表拉取恢复最终一致状态。
- Android 版本演进中如何只在 API 37 触发局域网运行时权限。
