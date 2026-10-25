本地 main 开发测试环境

入口：http://127.0.0.1:18090/
当前本机账号：Admin（已按用户要求修改）。
当前密码保存在 .cache/local-dev/credentials.json（600 权限，Git 忽略）。
可运行 python3 tools/local-dev/dev.py credentials 查看。不要把该文件提交 Git。

在 /Users/yangguang/Documents/3x-ui 目录运行：
  python3 tools/local-dev/dev.py build       # 构建当前 main 前端、后台和定制 Xray；完成后停止旧本地实例
  python3 tools/local-dev/dev.py start       # 启动后台进程，关闭终端也可继续使用
  python3 tools/local-dev/dev.py stop        # 仅停止本地实例，保留测试数据
  python3 tools/local-dev/dev.py restart     # 重启已有本地构建
  python3 tools/local-dev/dev.py status
  python3 tools/local-dev/verify.py          # 登录、客户端、限速保存及真实 VLESS 转发验收

修改源码后的日常步骤：build → start → 浏览器刷新 → verify。
这是完整构建后的预览，不是源码热更新；restart 本身不会重新编译。
全新环境首次运行自动创建独立 SQLite 和随机 LocalAdmin 密码，并建立“本地测试 · main”入站、local-test 客户端。
重建/重启不会重置账号、数据和限速。验收脚本暂时设为双向 2 Mbps，结束时恢复原值。

隔离范围：
- 数据库、二进制、生成配置、日志、密码都在 .cache/local-dev。
- 18090 管理后台、18190 VLESS 测试节点、18191 核心 API、18192 核心 metrics 均为 127.0.0.1。
- 订阅服务和 Telegram 通知默认关闭。不复制线上数据库、节点凭据、证书、远程节点。
- 测试节点出口是这台 Mac；脚本不改系统代理，也不操作 v2rayN。
- 全新本地模板不依赖线上 geoip/geosite，允许 loopback 以测试本机 HTTP 服务；此模板不可用于生产。
- 不在本地导入生产数据库或添加生产远程节点。新增测试入站也应仅监听 127.0.0.1。
- 脚本没有 SSH、推送、合并或部署功能；build/start 只允许 main 分支。
- 没有安装开机自启，重启电脑后执行 start。

工具链：Python 3、仓库 go.mod 对应的 Go、Node >=26/npm >=11、frontend/node_modules。
脚本优先使用当前已有 /private/tmp/xui-toolchain 的 Go/Node 和缓存，其次使用系统 PATH。
临时目录被清理后需重新安装工具链（或在 PATH 提供对应版本），前端依赖可用 npm ci 重装。

发布流程：
main 本地验收 → GitHub Linux 构建与集成测试通过 → 确认候选版本 → 合入并 Push Release。
Release Push 自动部署，服务器继续执行生产配置预检、真实转发健康检查和失败回滚验证。
本地通过不能替代 Linux/TLS/geodata/生产部署验证；单实例正式切换仍可能短暂重连。
