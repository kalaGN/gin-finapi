#!/bin/bash

# Gin FinAPI 系统压力测试脚本
# 测试目标: loan 业务接口
# 工具: Apache Bench (ab)

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 配置
BASE_URL="http://localhost:8082"
LOAN_ENDPOINT="/loan"
TEST_DATA='{"phone":"r707qyr0k2xmucjp7lz0"}'
RESULTS_DIR="./results"
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")

echo -e "${BLUE}======================================${NC}"
echo -e "${BLUE}   Gin FinAPI 系统压力测试${NC}"
echo -e "${BLUE}======================================${NC}"
echo ""

# 检查 ab 工具
if ! command -v ab &> /dev/null; then
    echo -e "${RED}错误: 未找到 Apache Bench (ab) 工具${NC}"
    echo -e "${YELLOW}请先安装: brew install httpd (macOS)${NC}"
    exit 1
fi

# 检查服务是否运行
echo -e "${YELLOW}[1/8] 检查服务状态...${NC}"
if ! curl -s ${BASE_URL}/health > /dev/null 2>&1; then
    echo -e "${RED}错误: 服务未运行，请先启动服务${NC}"
    echo -e "${YELLOW}启动命令: go run main.go${NC}"
    exit 1
fi
echo -e "${GREEN}✓ 服务运行正常${NC}"
echo ""

# 创建结果目录
mkdir -p ${RESULTS_DIR}
echo -e "${YELLOW}[2/8] 创建结果目录: ${RESULTS_DIR}${NC}"
echo ""

# 创建请求数据文件
echo ${TEST_DATA} > /tmp/loan_bench_request.json
echo -e "${YELLOW}[3/8] 准备测试数据${NC}"
echo -e "  请求数据: ${TEST_DATA}"
echo ""

# 测试函数
run_test() {
    local test_name=$1
    local requests=$2
    local concurrency=$3
    local output_file="${RESULTS_DIR}/${test_name}_${TIMESTAMP}.txt"

    echo -e "${BLUE}================================${NC}"
    echo -e "${BLUE}测试: ${test_name}${NC}"
    echo -e "${BLUE}请求数: ${requests} | 并发数: ${concurrency}${NC}"
    echo -e "${BLUE}================================${NC}"

    ab -n ${requests} -c ${concurrency} \
       -p /tmp/loan_bench_request.json \
       -T application/json \
       ${BASE_URL}${LOAN_ENDPOINT} 2>&1 | tee ${output_file}

    echo ""
    echo -e "${GREEN}✓ 测试完成，结果已保存: ${output_file}${NC}"
    echo ""
    sleep 2
}

# 执行测试套件
echo -e "${YELLOW}[4/8] 开始压力测试...${NC}"
echo ""

# 测试 1: 低并发预热
run_test "test1_warmup" 100 10

# 测试 2: 中等并发
run_test "test2_medium" 500 50

# 测试 3: 高并发
run_test "test3_high" 1000 100

# 测试 4: 超高并发
echo -e "${YELLOW}[5/8] 执行超高并发测试...${NC}"
run_test "test4_ultra_high" 2000 150

# 测试 5: 持续压力测试
echo -e "${YELLOW}[6/8] 执行持续压力测试...${NC}"
run_test "test5_sustained" 5000 100

echo -e "${YELLOW}[7/8] 生成测试摘要...${NC}"
echo ""

# 生成汇总报告
SUMMARY_FILE="${RESULTS_DIR}/summary_${TIMESTAMP}.txt"
cat > ${SUMMARY_FILE} << EOF
========================================
Gin FinAPI 压力测试汇总报告
========================================
测试时间: $(date)
目标接口: ${BASE_URL}${LOAN_ENDPOINT}
测试数据: ${TEST_DATA}

测试场景:
1. 低并发预热    - 100 请求 / 10 并发
2. 中等并发      - 500 请求 / 50 并发
3. 高并发        - 1000 请求 / 100 并发
4. 超高并发      - 2000 请求 / 150 并发
5. 持续压力      - 5000 请求 / 100 并发

详细结果文件保存在: ${RESULTS_DIR}/

========================================
EOF

cat ${SUMMARY_FILE}

echo ""
echo -e "${YELLOW}[8/8] 清理临时文件...${NC}"
rm -f /tmp/loan_bench_request.json

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}   ✓ 所有测试完成！${NC}"
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}结果目录: ${RESULTS_DIR}${NC}"
echo -e "${GREEN}汇总报告: ${SUMMARY_FILE}${NC}"
echo ""
