# Gin FinAPI 压力测试脚本

本目录包含 Gin FinAPI 系统的压力测试脚本。

## 📁 文件说明

- `bench.sh` - Linux/macOS 压测脚本
- `bench.bat` - Windows 压测脚本
- `results/` - 测试结果输出目录（自动创建）

## 🎯 测试场景

脚本包含 5 个测试场景，针对 loan 业务接口：

| 场景 | 请求数 | 并发数 | 说明 |
|------|--------|--------|------|
| 1. 预热测试 | 100 | 10 | 预热系统，建立连接池 |
| 2. 中等并发 | 500 | 50 | 模拟正常业务负载 |
| 3. 高并发 | 1000 | 100 | 测试高负载场景 |
| 4. 超高并发 | 2000 | 150 | 寻找系统瓶颈 |
| 5. 持续压力 | 5000 | 100 | 测试系统稳定性 |

## 🚀 使用方法

### Linux/macOS

```bash
# 进入测试目录
cd tests/bench

# 添加执行权限
chmod +x bench.sh

# 执行测试
./bench.sh
```

### Windows

```cmd
# 进入测试目录
cd tests\bench

# 执行测试
bench.bat
```

## ⚙️ 前置要求

1. **服务运行中**
   ```bash
   go run main.go
   ```

2. **安装 Apache Bench**
   - macOS: `brew install httpd` (自带)
   - Linux: `sudo apt-get install apache2-utils`
   - Windows: 下载 [Apache Lounge](https://www.apachelounge.com/download/)

3. **测试数据存在**
   - 确保 MongoDB 中存在测试数据
   - 默认查询 phone: `o8wiaiptftdvx0jrt3mm`

## 📊 测试输出

测试完成后会生成以下文件：

```
results/
├── test1_warmup_20251115_130000.txt
├── test2_medium_20251115_130010.txt
├── test3_high_20251115_130020.txt
├── test4_ultra_high_20251115_130030.txt
├── test5_sustained_20251115_130040.txt
└── summary_20251115_130050.txt
```

每个文件包含：
- QPS (每秒请求数)
- 响应时间分布 (50%, 90%, 95%, 99%)
- 失败请求统计
- 吞吐量信息

## 🔧 自定义配置

可编辑脚本顶部配置：

```bash
BASE_URL="http://localhost:8082"     # 服务地址
LOAN_ENDPOINT="/loan"                # 接口路径
TEST_DATA='{"phone":"your_test_id"}' # 测试数据
```

## 📈 性能指标

正常情况下预期指标：

- **QPS**: 7,000+ req/s
- **P99 延迟**: < 30ms
- **成功率**: 100%

## ⚠️ 注意事项

1. 压测期间会产生大量请求，请勿在生产环境执行
2. 确保 MongoDB 连接稳定
3. 建议先执行小规模测试，再逐步增加并发
4. 测试前确保系统资源充足（CPU、内存、网络）
