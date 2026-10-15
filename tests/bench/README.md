# Gin FinAPI 压力测试脚本

本目录包含可隔离复跑的当前版本合成基准及历史外部压测脚本。

## 当前版本进程内基准

在仓库根目录执行（不加载 `.env`、不监听端口、不连接 Redis/Mongo）：

```bash
go test ./tests/bench -run '^$' -bench BenchmarkAPI -benchtime=5000x -count=3 -cpu=4
```

`api_test.go` 使用真实路由装配、认证签名、产品授权、Handler、SID、配置缓存和异步文件日志。
存储替换为立即返回的合成数据源/查询函数；不包括 Redis 协议、Mongo 查询及 gzip 解压。
固定一个合成 API Key、一个产品、32 字节 phone、6 个结果变量；每场预热 100 次。
覆盖缓存启用/禁用及并发 1/32，`-cpu=4` 固定 GOMAXPROCS；缓存容量/回源并发使用默认值。
不启动五分钟刷新，限流阈值设为整型最大值以避免限流污染成功请求测量。

报告 req/s、avg_us、p95_us、p99_us、failure_pct、B/op、allocs/op 和 source_reads/op。
单请求耗时包括 httptest 请求构造、完整处理链及响应解析，不含网络，也不等待该条日志落盘。
计时结束后以 5s 预算排空日志并核对行数；请求失败、日志缺失都会让基准失败。
日志输出到 Go 测试临时目录（队列 10,000 条/64 MiB），不污染项目目录；写文件不等于 fsync 持久化。
请保留有界的 `-benchtime=5000x`，不要把本结果用作生产 QPS/SLO。

实测参数和全部三次结果见 [2026-10-15 基线报告](../../doc/knowledge/performance_baseline_20261015.md)。

## 历史脚本（不符合当前契约）

> 历史脚本的短 phone 样例已不符合当前 32/64 字节长度约束，且请求缺少完整认证参数；
> 不可用于成功查询的性能验收。执行前必须准备合法长度的测试标识、实时认证参数及隔离数据，
> 本次仅标记限制，不运行压测或改写历史压测逻辑。

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
LOAN_ENDPOINT="/api"                 # 接口路径
TEST_DATA='{"phone":"your_test_id"}' # 测试数据
```

## 📈 性能指标

历史预期值不作为当前门禁。真实 Redis/Mongo、Redis 日志及生产容量基线待在授权隔离环境建立；
前后比较必须保持 Go、硬件、数据规模、并发、日志配置和压测持续时间一致。

## ⚠️ 注意事项

1. 压测期间会产生大量请求，请勿在生产环境执行
2. 确保 MongoDB 连接稳定
3. 建议先执行小规模测试，再逐步增加并发
4. 测试前确保系统资源充足（CPU、内存、网络）
