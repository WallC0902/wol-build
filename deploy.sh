#!/bin/bash
# 部署脚本 - 小米路由器Pro (R3P)
# 通过 SSH 将 WOL Server 部署到路由器

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 默认配置
DEFAULT_ROUTER_IP="192.168.31.1"
DEFAULT_SSH_USER="root"
REMOTE_DIR="/data/wol-server"
SERVICE_NAME="wol-server"

# 获取脚本所在目录
SCRIPT_DIR="$(pwd)"
DIST_DIR="${SCRIPT_DIR}/dist"

# 帮助信息
show_help() {
    echo -e "${GREEN}WOL Server 部署脚本 - 小米路由器Pro R3P${NC}"
    echo ""
    echo "用法: $0 [选项] [路由器IP]"
    echo ""
    echo "选项:"
    echo "  -h, --help     显示帮助信息"
    echo "  -u, --user     SSH 用户名 (默认: root)"
    echo "  -p, --port     SSH 端口 (默认: 22)"
    echo "  --uninstall    卸载服务"
    echo "  --status       查看服务状态"
    echo "  --logs         查看服务日志"
    echo ""
    echo "示例:"
    echo "  $0                    # 部署到默认 192.168.31.1"
    echo "  $0 192.168.31.100     # 部署到指定 IP"
    echo "  $0 --status           # 查看服务状态"
    echo "  $0 --uninstall        # 卸载服务"
    echo ""
}

# 检查依赖
check_dependencies() {
    local missing=()
    
    if ! command -v ssh &> /dev/null; then
        missing+=("ssh")
    fi
    
    if ! command -v scp &> /dev/null; then
        missing+=("scp")
    fi
    
    if [ ${#missing[@]} -gt 0 ]; then
        echo -e "${RED}错误: 缺少必要的命令: ${missing[*]}${NC}"
        exit 1
    fi
}

# 检查编译产物
check_build() {
    if [ ! -f "${DIST_DIR}/wol-server-mipsel" ]; then
        echo -e "${RED}错误: 未找到编译产物${NC}"
        echo -e "${YELLOW}请先运行 ./build.sh 进行编译${NC}"
        exit 1
    fi
    
    if [ ! -f "${DIST_DIR}/index.html" ]; then
        echo -e "${RED}错误: 未找到 index.html${NC}"
        exit 1
    fi
}

# 测试 SSH 连接
test_ssh() {
    local ip=$1
    local user=$2
    local port=$3
    
    echo -e "${YELLOW}测试 SSH 连接到 ${user}@${ip}:${port}...${NC}"
    
    # 尝试连接，超时5秒
    if ssh -o ConnectTimeout=5 -o StrictHostKeyChecking=no -p "${port}" "${user}@${ip}" "echo 'SSH连接成功'" 2>/dev/null; then
        echo -e "${GREEN}✓ SSH 连接正常${NC}"
        return 0
    else
        echo -e "${RED}✗ SSH 连接失败${NC}"
        echo ""
        echo "可能的原因:"
        echo "  1. 路由器未开启 SSH (需要刷开发者固件)"
        echo "  2. IP 地址不正确"
        echo "  3. 网络不可达"
        echo ""
        echo -e "${YELLOW}开启 SSH 方法:${NC}"
        echo "  1. 下载小米开发者固件"
        echo "  2. 拷贝到 FAT32 U盘"
        echo "  3. 插入路由器，按住 Reset 通电"
        echo "  4. 等待黄灯闪烁后释放"
        echo "  5. SSH 到 ${ip}:22，用户名 ${user}"
        echo ""
        return 1
    fi
}

# 上传文件
upload_files() {
    local ip=$1
    local user=$2
    local port=$3
    
    echo ""
    echo -e "${YELLOW}上传文件到路由器...${NC}"
    
    # 创建远程目录
    ssh -p "${port}" "${user}@${ip}" "mkdir -p ${REMOTE_DIR}"
    
    # 上传二进制
    echo -e "${BLUE}  上传 wol-server-mipsel...${NC}"
    scp -P "${port}" "${DIST_DIR}/wol-server-mipsel" "${user}@${ip}:${REMOTE_DIR}/wol-server"
    
    # 上传前端
    echo -e "${BLUE}  上传 index.html...${NC}"
    scp -P "${port}" "${DIST_DIR}/index.html" "${user}@${ip}:${REMOTE_DIR}/"
    
    # 设置权限
    ssh -p "${port}" "${user}@${ip}" "chmod +x ${REMOTE_DIR}/wol-server"
    
    echo -e "${GREEN}✓ 文件上传完成${NC}"
}

# 安装服务
install_service() {
    local ip=$1
    local user=$2
    local port=$3
    
    echo ""
    echo -e "${YELLOW}安装系统服务...${NC}"
    
    # 创建 init 脚本
    ssh -p "${port}" "${user}@${ip}" "cat > /etc/init.d/${SERVICE_NAME} << 'INITSCRIPT'
#!/bin/sh /etc/rc.common
# WOL Server 启动脚本

START=99
STOP=01

SERVICE=${SERVICE_NAME}
USE_PROCD=1

start_service() {
    procd_open_instance
    procd_set_param command ${REMOTE_DIR}/wol-server
    procd_set_param stdout 1
    procd_set_param stderr 1
    procd_set_param respawn 3600 5 0
    procd_close_instance
}

stop_service() {
    killall wol-server 2>/dev/null || true
}

restart_service() {
    stop_service
    sleep 1
    start_service
}
INITSCRIPT"
    
    # 设置权限并启用服务
    ssh -p "${port}" "${user}@${ip}" "chmod +x /etc/init.d/${SERVICE_NAME}"
    ssh -p "${port}" "${user}@${ip}" "/etc/init.d/${SERVICE_NAME} enable"
    
    echo -e "${GREEN}✓ 服务已安装并设为开机自启${NC}"
}

# 启动服务
start_service() {
    local ip=$1
    local user=$2
    local port=$3
    
    echo ""
    echo -e "${YELLOW}启动服务...${NC}"
    
    ssh -p "${port}" "${user}@${ip}" "/etc/init.d/${SERVICE_NAME} start"
    sleep 2
    
    # 检查状态
    if ssh -p "${port}" "${user}@${ip}" "ps | grep wol-server | grep -v grep" > /dev/null 2>&1; then
        echo -e "${GREEN}✓ 服务已启动${NC}"
        echo ""
        echo -e "${GREEN}========================================${NC}"
        echo -e "${GREEN}部署完成!${NC}"
        echo -e "${GREEN}========================================${NC}"
        echo ""
        echo -e "${YELLOW}访问地址:${NC}"
        echo "  本机: http://localhost:8091/wol"
        echo "  局域网: http://${ip}:8091/wol"
        echo ""
        echo -e "${YELLOW}管理命令:${NC}"
        echo "  查看状态: $0 --status"
        echo "  查看日志: $0 --logs"
        echo "  重启服务: ssh ${user}@${ip} '/etc/init.d/${SERVICE_NAME} restart'"
        echo "  停止服务: ssh ${user}@${ip} '/etc/init.d/${SERVICE_NAME} stop'"
        echo "  卸载服务: $0 --uninstall"
        echo ""
    else
        echo -e "${RED}✗ 服务启动失败${NC}"
        echo -e "${YELLOW}查看日志: $0 --logs${NC}"
        exit 1
    fi
}

# 查看状态
show_status() {
    local ip=$1
    local user=$2
    local port=$3
    
    echo -e "${YELLOW}服务状态:${NC}"
    ssh -p "${port}" "${user}@${ip}" "
        echo '--- 进程状态 ---'
        ps | grep wol-server | grep -v grep || echo '未运行'
        echo ''
        echo '--- 端口监听 ---'
        netstat -tlnp 2>/dev/null | grep 8091 || echo '未监听 8091 端口'
        echo ''
        echo '--- 服务启用状态 ---'
        ls /etc/rc.d/ 2>/dev/null | grep ${SERVICE_NAME} || echo '未启用'
    "
}

# 查看日志
show_logs() {
    local ip=$1
    local user=$2
    local port=$3
    
    echo -e "${YELLOW}服务日志 (Ctrl+C 退出):${NC}"
    ssh -p "${port}" "${user}@${ip}" "
        if [ -f /var/log/messages ]; then
            grep wol-server /var/log/messages | tail -50
        fi
        if [ -f /tmp/wol-server.log ]; then
            tail -50 /tmp/wol-server.log
        fi
        logread | grep wol-server | tail -20
    "
}

# 卸载服务
uninstall() {
    local ip=$1
    local user=$2
    local port=$3
    
    echo -e "${YELLOW}卸载服务...${NC}"
    
    # 停止服务
    ssh -p "${port}" "${user}@${ip}" "/etc/init.d/${SERVICE_NAME} stop 2>/dev/null || true"
    ssh -p "${port}" "${user}@${ip}" "/etc/init.d/${SERVICE_NAME} disable 2>/dev/null || true"
    
    # 删除文件
    ssh -p "${port}" "${user}@${ip}" "rm -rf ${REMOTE_DIR}"
    ssh -p "${port}" "${user}@${ip}" "rm -f /etc/init.d/${SERVICE_NAME}"
    
    echo -e "${GREEN}✓ 服务已卸载${NC}"
}

# 主流程
main() {
    local router_ip="${DEFAULT_ROUTER_IP}"
    local ssh_user="${DEFAULT_SSH_USER}"
    local ssh_port="22"
    local action="deploy"
    
    # 解析参数
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                show_help
                exit 0
                ;;
            -u|--user)
                ssh_user="$2"
                shift 2
                ;;
            -p|--port)
                ssh_port="$2"
                shift 2
                ;;
            --status)
                action="status"
                shift
                ;;
            --logs)
                action="logs"
                shift
                ;;
            --uninstall)
                action="uninstall"
                shift
                ;;
            -*)
                echo -e "${RED}未知选项: $1${NC}"
                show_help
                exit 1
                ;;
            *)
                router_ip="$1"
                shift
                ;;
        esac
    done
    
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}WOL Server 部署 - 小米路由器Pro R3P${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""
    echo -e "${YELLOW}目标路由器:${NC} ${router_ip}"
    echo -e "${YELLOW}SSH 用户:${NC}   ${ssh_user}"
    echo -e "${YELLOW}SSH 端口:${NC}   ${ssh_port}"
    echo ""
    
    check_dependencies
    
    case ${action} in
        deploy)
            check_build
            test_ssh "${router_ip}" "${ssh_user}" "${ssh_port}" || exit 1
            upload_files "${router_ip}" "${ssh_user}" "${ssh_port}"
            install_service "${router_ip}" "${ssh_user}" "${ssh_port}"
            start_service "${router_ip}" "${ssh_user}" "${ssh_port}"
            ;;
        status)
            show_status "${router_ip}" "${ssh_user}" "${ssh_port}"
            ;;
        logs)
            show_logs "${router_ip}" "${ssh_user}" "${ssh_port}"
            ;;
        uninstall)
            uninstall "${router_ip}" "${ssh_user}" "${ssh_port}"
            ;;
    esac
}

main "$@"
