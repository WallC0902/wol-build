#!/bin/bash
# 交叉编译脚本 - 小米路由器Pro (R3P)
# CPU: MT7621A MIPS mipsel小端序, 无硬件FPU

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 项目路径
SCRIPT_DIR="$(pwd)"
SRC_DIR="${SCRIPT_DIR}/src"
DIST_DIR="${SCRIPT_DIR}/dist"

# 目标平台配置
export GOOS=linux
export GOARCH=mipsle
export GOMIPS=softfloat  # MT7621A 无硬件FPU，必须使用软浮点

# 输出文件名
OUTPUT_NAME="wol-server-mipsel"

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}WOL Server 交叉编译 - 小米路由器Pro R3P${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo -e "${YELLOW}目标平台:${NC}"
echo "  GOOS:     ${GOOS}"
echo "  GOARCH:   ${GOARCH}"
echo "  GOMIPS:   ${GOMIPS}"
echo ""

# 创建输出目录
mkdir -p "${DIST_DIR}"

# 进入源码目录
cd "${SRC_DIR}"

echo -e "${YELLOW}开始编译...${NC}"

# 编译
CGO_ENABLED=0 go build -o "${DIST_DIR}/${OUTPUT_NAME}" -ldflags="-s -w" .

if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ 编译成功!${NC}"
    echo ""
    
    # 显示文件信息
    OUTPUT_FILE="${DIST_DIR}/${OUTPUT_NAME}"
    FILE_SIZE=$(ls -lh "${OUTPUT_FILE}" | awk '{print $5}')
    FILE_TYPE=$(file "${OUTPUT_FILE}" 2>/dev/null || echo "file command not available")
    
    echo -e "${YELLOW}输出文件:${NC}"
    echo "  路径: ${OUTPUT_FILE}"
    echo "  大小: ${FILE_SIZE}"
    echo "  类型: ${FILE_TYPE}"
    echo ""
    
    # 复制前端文件到 dist
    cp "${SRC_DIR}/index.html" "${DIST_DIR}/"
    echo -e "${GREEN}✓ 已复制 index.html 到 dist/${NC}"
    
    echo ""
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}下一步: 运行部署脚本${NC}"
    echo -e "${GREEN}./deploy.sh <路由器IP>${NC}"
    echo -e "${GREEN}========================================${NC}"
else
    echo -e "${RED}✗ 编译失败!${NC}"
    exit 1
fi
