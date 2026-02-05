# 开发人员文档

本文档面向开发人员，详细介绍 Simple KCP File Manager 的架构设计、技术实现和开发指南。

## 目录

- [架构总览](#架构总览)
- [技术栈](#技术栈)
- [项目结构](#项目结构)
- [核心模块详解](#核心模块详解)
  - [网络传输层](#网络传输层)
  - [服务端架构](#服务端架构)
  - [客户端架构](#客户端架构)
  - [GUI 组件系统](#gui-组件系统)
- [API 参考](#api-参考)
- [关键技术点](#关键技术点)
- [开发指南](#开发指南)
- [性能调优](#性能调优)
- [故障排查](#故障排查)

---

## 架构总览

### 系统架构图

```
┌──────────────────────────────────────────────────────────────────────┐
│                        客户端 (Fyne GUI)                              │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌────────────────┐  │
│  │ MainWindow │  │ DirTree    │  │ TaskQueue  │  │ ContextMenu    │  │
│  └─────┬──────┘  └─────┬──────┘  └─────┬──────┘  └────────────────┘  │
│        │               │               │                             │
│        └───────────────┼───────────────┘                             │
│                        ▼                                             │
│              ┌─────────────────────┐                                 │
│              │   KCP Client        │                                 │
│              │   (kcpclient/)      │                                 │
│              └──────────┬──────────┘                                 │
└─────────────────────────┼────────────────────────────────────────────┘
                          │ HTTP over KCP/smux
                          ▼
┌─────────────────────────────────────────────────────────────────────┐
│                        服务端 (HTTP Server)                          │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌──────────────┐  │
│  │FileHandler │  │UploadHndlr│  │CompressHndl│  │ EditHandler  │  │
│  └─────┬──────┘  └─────┬──────┘  └─────┬──────┘  └──────┬───────┘  │
│        │               │               │                │          │
│        └───────────────┼───────────────┼────────────────┘          │
│                        ▼                                            │
│              ┌─────────────────────┐                                │
│              │   smux Session      │                                │
│              └──────────┬──────────┘                                │
│                        ▼                                            │
│              ┌─────────────────────┐                                │
│              │   KCP Listener      │                                │
│              │   (AES-256 加密)     │                                │
│              └──────────┬──────────┘                                │
└─────────────────────────┼───────────────────────────────────────────┘
                          │ UDP
                          ▼
                    ┌──────────┐
                    │  网络层   │
                    └──────────┘
```

### 协议栈

```
┌─────────────────────────────────────────────────────────┐
│                    Application Layer                    │
│                  (File Manager / HTTP)                  │
├─────────────────────────────────────────────────────────┤
│                   smux (Multiplexing)                   │
│              (Stream Multiplexing Protocol)             │
├─────────────────────────────────────────────────────────┤
│              KCP (Reliable UDP Transport)               │
│              (Fast ARQ Protocol)                        │
├─────────────────────────────────────────────────────────┤
│                        UDP                              │
└─────────────────────────────────────────────────────────┘
```

---

## 技术栈

| 组件 | 技术 | 版本 | 用途 |
|------|------|------|------|
| 传输协议 | [KCP](https://github.com/xtaci/kcp-go) | v5.6.1+ | 基于 UDP 的快速可靠传输 |
| 多路复用 | [smux](https://github.com/xtaci/smux) | v1.0.0-rc | 单连接多流复用 |
| 应用协议 | HTTP/1.1 | - | RESTful API |
| GUI 框架 | [Fyne](https://fyne.io/) | v2.7.0+ | 跨平台桌面 GUI |
| 加密 | AES-256-CBC | - | 数据传输加密 |
| 密钥派生 | PBKDF2-SHA256 | 4096 次迭代 | 密钥安全派生 |
| 压缩 | tar.gz | - | 打包传输压缩 |

---

## 项目结构

```
simpleKcpFileTransfer/
├── client/                        # 客户端入口
│   ├── main.go                    # 主程序入口，解析命令行参数
│   └── gui/                       # GUI 组件目录
│       ├── main_window.go         # 主窗口（三栏布局）
│       ├── connection_dialog.go   # 连接对话框
│       ├── directory_tree.go      # 目录树组件（懒加载）
│       ├── task_queue.go          # 任务队列面板
│       ├── context_menu.go        # 右键菜单系统
│       ├── text_editor.go         # 内置文本编辑器
│       ├── file_list_item.go      # 文件列表项组件
│       └── drag_drop.go           # 拖拽上传支持
│
├── kcpclient/                     # KCP 客户端核心库
│   ├── client.go                  # KCP 连接管理、HTTP 客户端、打包传输
│   └── tasks/                     # 任务管理系统
│       └── manager.go             # 并发任务调度器
│
├── server/                        # 服务端
│   ├── main.go                    # 服务端入口，HTTP 路由
│   ├── handlers/                  # HTTP 处理器
│   │   ├── file_handler.go        # 文件列表、删除、重命名、权限
│   │   ├── upload_handler.go      # 上传处理（支持分块、自动解压）
│   │   ├── compress_handler.go    # 压缩/解压操作
│   │   └── edit_handler.go        # 文件编辑（读取/保存）
│   └── compress/                  # 压缩工具
│       ├── tar.go                 # tar 打包/解包
│       └── zip.go                 # zip 处理
│
├── common/                        # 共享模块
│   ├── kcp.go                     # KCP/smux 配置、加密初始化
│   └── protocol.go                # 协议常量定义
│
├── scripts/                       # 构建脚本
│   ├── build.sh                   # Linux/macOS 交叉编译
│   └── build.ps1                  # Windows PowerShell 编译
│
├── docs/                          # 文档目录
│   └── DEVELOPMENT.md             # 本文档
│
├── go.mod                         # Go 模块定义
├── go.sum                         # 依赖校验和
├── README.md                      # 项目说明
├── CLAUDE.md                      # AI 开发指南
└── LICENSE                        # MIT 许可证
```

---

## 核心模块详解

### 网络传输层

#### KCP 配置 (`common/kcp.go`)

采用类似 kcptun `fast3` 模式的激进配置：

```go
func ConfigKCP(conn *kcp.UDPSession) {
    conn.SetNoDelay(1, 10, 2, 1)    // nodelay, interval, resend, nc
    conn.SetWindowSize(1024, 1024)  // sndwnd, rcvwnd
    conn.SetMtu(1350)               // 避免 IP 分片
    conn.SetACKNoDelay(true)        // 立即发送 ACK
}
```

**参数说明：**
- `NoDelay(1, 10, 2, 1)`:
  - `nodelay=1`: 启用立即 ACK（低延迟）
  - `interval=10`: 10ms 发包间隔（激进）
  - `resend=2`: 快速重传触发次数
  - `nc=1`: 无拥塞控制
- `WindowSize(1024, 1024)`: 大窗口支持高吞吐
- `MTU=1350`: 安全 MTU，避免分片

#### 加密配置

```go
func GetBlockCrypt(key string) (kcp.BlockCrypt, error) {
    // PBKDF2 密钥派生
    derivedKey := pbkdf2.Key(
        []byte(key),
        []byte("kcp-file-transfer"),  // 固定 salt
        4096,                          // 迭代次数
        32,                            // AES-256 密钥长度
        sha256.New,
    )
    return kcp.NewAESBlockCrypt(derivedKey)
}
```

#### smux 配置

```go
func SmuxConfig() *smux.Config {
    config := smux.DefaultConfig()
    config.MaxReceiveBuffer = 4194304  // 4MB 接收缓冲
    config.MaxStreamBuffer = 2097152   // 2MB 流缓冲
    config.KeepAliveInterval = 10 * time.Second
    return config
}
```

#### SmuxListener 适配器

为了让 HTTP 服务器与 smux 兼容，实现了 `net.Listener` 接口：

```go
type SmuxListener struct {
    Session *smux.Session
}

func (l *SmuxListener) Accept() (net.Conn, error) {
    return l.Session.AcceptStream()
}
```

### 服务端架构

#### 请求处理流程

```
UDP 数据包 → KCP 解密 → smux 解复用 → HTTP 请求 → Handler 处理 → HTTP 响应
```

#### Handler 职责

| Handler | 文件 | 职责 |
|---------|------|------|
| FileHandler | `file_handler.go` | 列表、删除、重命名、权限、统计 |
| UploadHandler | `upload_handler.go` | 上传、分块上传、自动解压 |
| CompressHandler | `compress_handler.go` | 压缩、解压 |
| EditHandler | `edit_handler.go` | 文件读取、保存（编辑器） |

#### 路径安全检查

所有文件操作都经过 `isPathSafe()` 验证，防止目录遍历攻击：

```go
func (h *FileHandler) isPathSafe(requestPath string) (string, bool) {
    cleanPath := path.Clean("/" + requestPath)
    fullPath := filepath.Join(h.rootDir, filepath.FromSlash(cleanPath))
    
    absRoot, _ := filepath.Abs(h.rootDir)
    absPath, _ := filepath.Abs(fullPath)
    
    // 确保路径在根目录内
    if !strings.HasPrefix(absPath, absRoot+string(filepath.Separator)) && absPath != absRoot {
        return "", false
    }
    return fullPath, true
}
```

### 客户端架构

#### KCP Client (`kcpclient/client.go`)

**核心功能：**
- 连接管理（带 3 秒超时验证密钥）
- HTTP 客户端（通过 smux 流）
- 文件上传/下载（支持多线程、断点续传）
- 打包传输（tar.gz 压缩/解压）

**连接流程：**
```
1. 创建 KCP 连接（带加密）
2. 建立 smux 会话
3. 发送测试请求验证密钥
4. 配置 HTTP 客户端使用 smux 流
```

**多线程下载：**
```go
// 大文件（>4MB）自动启用多线程
const (
    defaultChunkSize = 4 * 1024 * 1024  // 4MB 分块
    numWorkers = 8                       // 8 线程并行
)
```

#### 任务管理器 (`kcpclient/tasks/manager.go`)

**任务类型：**
- `TaskTypeDownload`: 下载任务
- `TaskTypeUpload`: 上传任务
- `TaskTypeCompress`: 压缩任务
- `TaskTypeExtract`: 解压任务

**任务状态：**
- `StatusPending`: 等待执行
- `StatusRunning`: 执行中
- `StatusPaused`: 已暂停
- `StatusCompleted`: 已完成
- `StatusFailed`: 失败
- `StatusCanceled`: 已取消

**并发控制：**
```go
type Manager struct {
    semaphore chan struct{}  // 信号量控制并发
    maxParallel int          // 最大并行任务数（默认 3）
}
```

### GUI 组件系统

#### 主窗口布局 (`main_window.go`)

采用三栏布局：

```
┌─────────────┬─────────────────────┬──────────────┐
│ 目录树       │     文件列表         │   任务队列    │
│ (25%)       │     (50%)           │   (25%)      │
│             │                     │              │
│ HSplit      │                     │   HSplit     │
│ offset=0.25 │                     │   offset=0.75│
└─────────────┴─────────────────────┴──────────────┘
```

#### 事件拦截层 (`EventInterceptLayer`)

为了在 `widget.List` 上实现右键菜单和双击进入文件夹，使用自定义透明覆盖层：

```go
type EventInterceptLayer struct {
    widget.BaseWidget
    mainWindow   *MainWindow
    onRightClick func(fyne.Position)
}

// 实现三个接口方法：
func (l *EventInterceptLayer) Tapped(e *fyne.PointEvent)          // 单击选择
func (l *EventInterceptLayer) TappedSecondary(e *fyne.PointEvent) // 右键菜单
func (l *EventInterceptLayer) DoubleTapped(e *fyne.PointEvent)    // 双击进入
```

**技术要点：**
- 使用 `container.Stack` 将透明层放在 `fileListScroll` 上方
- 透明层使用 `canvas.Rectangle(color.Transparent)` 渲染
- `MinSize()` 返回 `(1, 1)` 确保事件可被接收
- 通过滚动偏移计算点击的列表项索引

#### 目录树 (`directory_tree.go`)

**懒加载机制：**
- 节点展开时才加载子目录
- 使用 `treeMutex` 保护并发访问
- 缓存已加载的目录结构

#### 上下文菜单 (`context_menu.go`)

**菜单类型：**
- 文件菜单：下载、编辑、删除、重命名、压缩
- 文件夹菜单：进入、下载、删除、压缩
- 空白区域菜单：上传、新建文件夹、刷新

---

## API 参考

### HTTP 端点

| 方法 | 端点 | 参数 | 说明 |
|------|------|------|------|
| GET | `/?action=list` | `path`, `recursive` | 获取文件列表 |
| GET | `/?action=checksum` | `path` | 获取 SHA256 校验和 |
| GET | `/?action=stat` | `path` | 获取文件/目录信息 |
| GET | `/?action=edit` | `path` | 获取文件内容（编辑） |
| PUT | `/?action=edit` | `path` | 保存文件内容 |
| PUT | `/?action=upload` | `path` | 上传文件 |
| DELETE | `/?action=delete` | `path` | 删除文件/目录 |
| POST | `/?action=mkdir` | `path` | 创建目录 |
| POST | `/?action=rename` | `old`, `new` | 重命名 |
| POST | `/?action=chmod` | `path`, `mode` | 修改权限 |
| POST | `/?action=compress` | `paths`, `output`, `format` | 压缩文件 |
| POST | `/?action=extract` | `path` | 解压文件 |
| GET | `/path/to/file` | - | 下载文件（支持 Range） |

### 特殊 HTTP Headers

| Header | 值 | 说明 |
|--------|-----|------|
| `X-Auto-Extract` | `1` | 上传后自动解压 tar.gz |
| `Content-Range` | `bytes start-end/total` | 分块上传 |
| `Range` | `bytes=start-end` | 断点续传下载 |

### 响应格式

**文件列表 (`action=list`):**
```json
[
  {
    "name": "file.txt",
    "path": "/docs/file.txt",
    "size": 1024,
    "modTime": 1699123456,
    "isDir": false,
    "mode": "-rw-r--r--"
  }
]
```

---

## 关键技术点

### 1. HTTP-over-KCP 实现

通过 smux 在单个 KCP 连接上复用多个 HTTP 流：

```go
// 服务端
smuxLis := &common.SmuxListener{Session: mux}
http.Serve(smuxLis, mainHandler)

// 客户端
c.httpClient = &http.Client{
    Transport: &http.Transport{
        DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
            return session.OpenStream()
        },
    },
}
```

### 2. 打包传输机制

**上传流程：**
```
本地文件夹 → tar.gz 压缩 → 上传 → 服务端自动解压
```

**下载流程：**
```
服务端压缩 → tar.gz 下载 → 本地解压
```

**临时文件清理：**
- 客户端：`uploadAndCleanup()` 确保正确关闭后删除
- 服务端：异步重试删除（处理 Windows 文件锁定问题）

### 3. 多线程文件传输

**分块策略：**
- 文件 < 4MB：单线程传输
- 文件 ≥ 4MB：8 线程并行分块

**下载实现：**
```go
for i := 0; i < numWorkers; i++ {
    go func(workerID int) {
        // 每个 worker 负责一个分块
        // 使用 Range header 请求分块
    }()
}
```

### 4. Fyne GUI 事件处理

**事件优先级问题：**
- `widget.List` 不支持 `TappedSecondary`/`DoubleTapped`
- 使用透明 `EventInterceptLayer` 覆盖层拦截事件

**位置计算：**
```go
func (l *EventInterceptLayer) getClickedIndex(localPos fyne.Position) int {
    scrollOffsetY := l.mainWindow.fileListScroll.Offset.Y
    adjustedY := localPos.Y + scrollOffsetY
    const estimatedRowHeight = float32(37)
    return int(adjustedY / estimatedRowHeight)
}
```

### 5. 线程安全

**UI 更新原则：**
- 直接更新 widget 属性是安全的（Fyne 内部处理）
- 避免使用 `fyne.Do()`（可能导致死锁）
- 使用 `sync.Mutex` 保护共享状态

---

## 开发指南

### 添加新功能

1. **确定影响范围**：先阅读相关代码
2. **遵循模式**：参考现有代码结构
3. **错误处理**：使用 `log.Printf("[DEBUG] ...")`
4. **编译测试**：`go build ./...`

### 添加新的 HTTP 端点

1. 在 `server/main.go` 的 `createMainHandler()` 中添加 case
2. 创建对应的 Handler 方法
3. 在客户端 `kcpclient/client.go` 添加对应方法

### 添加新的 GUI 组件

1. 在 `client/gui/` 下创建新文件
2. 实现 `fyne.CanvasObject` 或继承 `widget.BaseWidget`
3. 在 `main_window.go` 中集成

### 调试技巧

**添加日志：**
```go
log.Printf("[DEBUG] FunctionName: variable=%v", variable)
```

**检查 goroutine 泄漏：**
```go
log.Printf("[DEBUG] Goroutine count: %d", runtime.NumGoroutine())
```

---

## 性能调优

### KCP 参数调整

```go
func ConfigKCP(conn *kcp.UDPSession) {
    conn.SetNoDelay(1, 10, 2, 1)    // 更激进：(1, 5, 1, 1)
    conn.SetWindowSize(1024, 1024)  // 更大窗口：(2048, 2048)
    conn.SetMtu(1350)               // 保持不变
}
```

### 客户端参数

| 参数 | 文件 | 默认值 | 说明 |
|------|------|--------|------|
| `connectionTimeout` | `client.go` | 3s | 连接超时 |
| `defaultChunkSize` | `client.go` | 4MB | 分块大小 |
| `maxParallelTasks` | `manager.go` | 3 | 最大并行任务 |
| `defaultWorkers` | `manager.go` | 8 | 单文件并行线程 |

### 提升传输速度

1. 增加 `maxParallelTasks`（更多并发下载）
2. 增加 `defaultWorkers`（单文件更多线程）
3. 减小 KCP `interval`（更激进发包）
4. 启用打包传输（减少小文件开销）

### 降低 CPU 使用

1. 减少 `maxParallelTasks` 和 `defaultWorkers`
2. 增大 KCP `interval`（如 20ms）

---

## 故障排查

### 常见问题

| 问题 | 可能原因 | 解决方案 |
|------|---------|---------|
| 连接超时 | 密钥错误/服务器不可达 | 检查密钥一致性，确认服务器运行 |
| UI 冻结 | 主线程阻塞 | 使用 goroutine 执行耗时操作 |
| 文件无法删除 | 文件句柄未关闭 | 显式 Close() 后再删除 |
| 目录树不加载 | 死锁/无限循环 | 检查 mutex 使用，添加日志 |

### 调试日志位置

- 客户端：标准输出
- 服务端：标准输出（`log.Printf`）

### 启用详细日志

在关键函数入口添加：
```go
log.Printf("[DEBUG] FunctionName: START params=%v", params)
defer log.Printf("[DEBUG] FunctionName: END")
```

---

## 相关文档

- [README.md](../README.md) - 项目说明和使用指南
- [CLAUDE.md](../CLAUDE.md) - AI 开发辅助指南
- [Fyne 文档](https://developer.fyne.io/) - GUI 框架文档
- [KCP 协议](https://github.com/skywind3000/kcp) - KCP 原理说明


