# WOL 网络唤醒插件

小米路由器 PRO (R3P) Wake-on-LAN 插件，通过发送 WOL 魔术包唤醒局域网内的设备。

## 构建

```bash
make
```

交叉编译工具链: mipsel-xiaomi-linux-uclibc-g++

## 文件说明

- `TaskWatcher.cpp/h` - WOL 魔术包发送器，解析 MAC:IP 格式并发送 UDP 广播
- `index.html` - Web UI 界面，提供唤醒按钮
- `wol.go` - Go HTTP 服务器，在 8091 端口提供 index.html 和 mpk 下载
- `wol_daemon.sh` - 守护进程脚本，监控 wol 进程并自动重启
- `sleep.cpp` - 睡眠工具，用于守护进程循环
- `start_script` - 插件启动脚本
- `xiaomi.project` - 插件配置文件

## 部署

1. GitHub Actions 交叉编译 MIPS 二进制
2. 下载 artifacts 并配置 xiaomi.project
3. 通过 d.miwifi.com/plugin/mipk 打包为 mpk
4. 将 mpk 托管到公开 URL
5. 在小米路由器 APP 4.3.2 中扫描二维码安装

## 注意事项

- 仅在局域网内使用，不暴露到公网
- 目标设备需支持 Wake-on-LAN 并在 BIOS 中启用
