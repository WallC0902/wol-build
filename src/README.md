# WOL 网络唤醒服务

小米路由器 Wake-on-LAN 服务，基于 Go HTTP Server，提供 REST API 和 Web UI 管理局域网设备并发送唤醒包。

## 架构

单一 Go 二进制，替代原 C++ 插件 + Go helper + Shell 守护进程架构：

- `server.go` — Go HTTP Server，监听 8091 端口
  - REST API：设备 CRUD（增删改查）+ WOL 唤醒
  - 静态文件服务：`/wol` 路径提供 Web UI
  - JSON 文件持久化：`devices.json` 存储设备列表
- `index.html` — Web UI，设备管理界面（添加/编辑/删除/唤醒）
- `start_script` — 插件启动脚本
- `xiaomi.project` — 小米路由器插件配置

## API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/devices` | 获取所有设备 |
| POST | `/api/devices` | 添加设备 `{name, mac, ip}` |
| GET | `/api/devices/:id` | 获取单个设备 |
| PUT | `/api/devices/:id` | 更新设备 |
| DELETE | `/api/devices/:id` | 删除设备 |
| POST | `/api/wake` | 发送 WOL 唤醒包 `{mac}` |

## 构建

GitHub Actions 自动交叉编译 MIPS 二进制：

```bash
GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build -o server ./src/server.go
```

## 部署

1. GitHub Actions 构建并上传 artifacts
2. 下载 `server` 二进制和 `plugin.zip`
3. 通过 d.miwifi.com/plugin/mipk 打包上传
4. 路由器安装插件后访问 `http://192.168.31.1:8091/wol`

## 注意事项

- 路由器需与目标设备在同一局域网
- WOL 魔术包通过 UDP 端口 9 广播发送
- 目标设备 BIOS/网卡需支持并启用 WOL
- 设备数据持久化在 `/data/userdata/wol/devices.json`
