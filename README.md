# IC-9700 Remote IO

用于通过 CI-V 控制 Icom IC-9700 的紧凑型 Windows 工作台。

## 当前功能

- 顶部功能栏：退出、连接、SAT
- 连接配置独立弹窗：串口、波特率、刷新串口
- 保存最近一次串口配置；主界面连接按钮可一键连接
- 未配置或连接失败时自动打开连接配置并提示修改
- 主界面使用连接按钮 + 状态指示灯显示连接状态
- DATA OFF / DATA 输入控制
- 一键切换 LAN / USB 输入
- USB AF / IF 输出切换
- 保留底部 `COMMUNICATION`，显示最后一次 TX/RX CI-V 帧
- SAT 非模态浮动子窗口：主界面与 SAT 窗口可同时操作
- SAT 模式开关
- RX VFO（D0 / 主 / 下行）频率与模式控制
- TX VFO（D1 / 副 / 上行）频率与模式控制
- 白天 / 黑夜主题切换，默认白天

## SAT CI-V 控制

依据 IC-9700 CI-V Reference Guide：

- Satellite mode：`1A 05 5A`，`00=OFF`、`01=ON`
- VFO/侧选择：`07 D0` 选择 MAIN，`07 D1` 选择 SUB
- Operating frequency：读取 `03`，设置 `05`
- Operating mode：读取 `04`，设置 `06`
- 频率按 CI-V 文档定义的 5 字节 BCD 顺序编码

在 IC-9700 卫星模式下，应用层将 D0 作为 RX/下行侧、D1 作为 TX/上行侧，并在每次 SAT 操作结束后恢复到 D0。

## 软件图标

Windows 正式软件图标预留路径为 `build/windows/icon.ico`。后续替换正式 ICO 文件时保持该路径和文件名即可；前端窗口图标同步预留在 `frontend/src/app-icon.ico`。主界面不再在“遥控工作台”标题左侧单独显示 Logo，应用图标由窗口/标题栏图标提供。

## 构建

Windows 环境：

```powershell
go mod tidy
wails build
```

前端资源位于 `frontend/src`；Wails 生成的绑定会由构建流程复制到 `frontend/dist/wailsjs`。


### SAT 独立窗口

顶部 **SAT** 会启动一个真正独立的顶层窗口，可自由拖出主窗口范围，并与主窗口同时操作。由于当前工程采用 Wails v2，SAT 窗口由同一 EXE 启动第二个窗口实例；串口/CI-V 始终只由主实例持有，SAT 实例通过带随机令牌的 `127.0.0.1` 本机 RPC 请求主实例执行卫星控制，从而避免两个进程同时打开同一个串口。关闭主窗口时 SAT 窗口及本机 RPC 会一并结束。