@echo off
REM Gin FinAPI 系统压力测试脚本 (Windows)
REM 测试目标: loan 业务接口
REM 工具: Apache Bench (ab)

setlocal enabledelayedexpansion

REM 配置
set BASE_URL=http://localhost:8082
set LOAN_ENDPOINT=/loan
set TEST_DATA={"phone":"o8wiaiptftdvx0jrt3mm"}
set RESULTS_DIR=.\results
set TIMESTAMP=%date:~0,4%%date:~5,2%%date:~8,2%_%time:~0,2%%time:~3,2%%time:~6,2%
set TIMESTAMP=%TIMESTAMP: =0%

echo ======================================
echo    Gin FinAPI 系统压力测试
echo ======================================
echo.

REM 检查 ab 工具
where ab >nul 2>&1
if %errorlevel% neq 0 (
    echo 错误: 未找到 Apache Bench (ab) 工具
    echo 请从 https://www.apachelounge.com/download/ 下载 Apache 并添加到 PATH
    exit /b 1
)

REM 检查服务是否运行
echo [1/8] 检查服务状态...
curl -s %BASE_URL%/health >nul 2>&1
if %errorlevel% neq 0 (
    echo 错误: 服务未运行，请先启动服务
    echo 启动命令: go run main.go
    exit /b 1
)
echo 服务运行正常
echo.

REM 创建结果目录
if not exist %RESULTS_DIR% mkdir %RESULTS_DIR%
echo [2/8] 创建结果目录: %RESULTS_DIR%
echo.

REM 创建请求数据文件
echo %TEST_DATA% > %TEMP%\loan_bench_request.json
echo [3/8] 准备测试数据
echo   请求数据: %TEST_DATA%
echo.

echo [4/8] 开始压力测试...
echo.

REM 测试 1: 低并发预热
echo ================================
echo 测试: test1_warmup
echo 请求数: 100 ^| 并发数: 10
echo ================================
ab -n 100 -c 10 -p %TEMP%\loan_bench_request.json -T application/json %BASE_URL%%LOAN_ENDPOINT% > %RESULTS_DIR%\test1_warmup_%TIMESTAMP%.txt 2>&1
type %RESULTS_DIR%\test1_warmup_%TIMESTAMP%.txt
echo.
timeout /t 2 /nobreak >nul

REM 测试 2: 中等并发
echo ================================
echo 测试: test2_medium
echo 请求数: 500 ^| 并发数: 50
echo ================================
ab -n 500 -c 50 -p %TEMP%\loan_bench_request.json -T application/json %BASE_URL%%LOAN_ENDPOINT% > %RESULTS_DIR%\test2_medium_%TIMESTAMP%.txt 2>&1
type %RESULTS_DIR%\test2_medium_%TIMESTAMP%.txt
echo.
timeout /t 2 /nobreak >nul

REM 测试 3: 高并发
echo ================================
echo 测试: test3_high
echo 请求数: 1000 ^| 并发数: 100
echo ================================
ab -n 1000 -c 100 -p %TEMP%\loan_bench_request.json -T application/json %BASE_URL%%LOAN_ENDPOINT% > %RESULTS_DIR%\test3_high_%TIMESTAMP%.txt 2>&1
type %RESULTS_DIR%\test3_high_%TIMESTAMP%.txt
echo.
timeout /t 2 /nobreak >nul

REM 测试 4: 超高并发
echo [5/8] 执行超高并发测试...
echo ================================
echo 测试: test4_ultra_high
echo 请求数: 2000 ^| 并发数: 150
echo ================================
ab -n 2000 -c 150 -p %TEMP%\loan_bench_request.json -T application/json %BASE_URL%%LOAN_ENDPOINT% > %RESULTS_DIR%\test4_ultra_high_%TIMESTAMP%.txt 2>&1
type %RESULTS_DIR%\test4_ultra_high_%TIMESTAMP%.txt
echo.
timeout /t 2 /nobreak >nul

REM 测试 5: 持续压力测试
echo [6/8] 执行持续压力测试...
echo ================================
echo 测试: test5_sustained
echo 请求数: 5000 ^| 并发数: 100
echo ================================
ab -n 5000 -c 100 -p %TEMP%\loan_bench_request.json -T application/json %BASE_URL%%LOAN_ENDPOINT% > %RESULTS_DIR%\test5_sustained_%TIMESTAMP%.txt 2>&1
type %RESULTS_DIR%\test5_sustained_%TIMESTAMP%.txt
echo.

echo [7/8] 生成测试摘要...
echo.

REM 生成汇总报告
set SUMMARY_FILE=%RESULTS_DIR%\summary_%TIMESTAMP%.txt
(
echo ========================================
echo Gin FinAPI 压力测试汇总报告
echo ========================================
echo 测试时间: %date% %time%
echo 目标接口: %BASE_URL%%LOAN_ENDPOINT%
echo 测试数据: %TEST_DATA%
echo.
echo 测试场景:
echo 1. 低并发预热    - 100 请求 / 10 并发
echo 2. 中等并发      - 500 请求 / 50 并发
echo 3. 高并发        - 1000 请求 / 100 并发
echo 4. 超高并发      - 2000 请求 / 150 并发
echo 5. 持续压力      - 5000 请求 / 100 并发
echo.
echo 详细结果文件保存在: %RESULTS_DIR%/
echo.
echo ========================================
) > %SUMMARY_FILE%

type %SUMMARY_FILE%

echo.
echo [8/8] 清理临时文件...
del %TEMP%\loan_bench_request.json

echo.
echo ========================================
echo    所有测试完成！
echo ========================================
echo 结果目录: %RESULTS_DIR%
echo 汇总报告: %SUMMARY_FILE%
echo.

endlocal
