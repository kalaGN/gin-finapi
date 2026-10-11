# Apifox 回归用例
生成依据：2026-10-11 的 routes/api.go、LoanAuth、LoanHandler、pkg/mongo 和 rescode。
本文件只说明测试资产，不改变业务或部署配置。

## 导入
1. Apifox 项目设置 → 导入数据 → Postman，选择 gin-finapi.postman_collection.json。
2. 同样导入 gin-finapi.postman_environment.json；选中导入的测试环境。
3. 在本机环境配置 api_key、secret_key，使其匹配测试服务 API_KEY、SECRET_KEY；不要提交真实值。
4. 确认服务前置URL为 base_url，默认 http://127.0.0.1:8083；导入转换后可能需要在环境的服务中手动确认。
5. 检查每个用例前置脚本、后置断言均已导入。前置脚本重写JSON Body，生成当前毫秒时间戳和大写MD5签名。
6. 如需批量回归，在自动化测试中引用导入的接口用例；不要默认加入手动故障组。

官方说明：
- [Postman 导入](https://docs.apifox.com/import-postman)
- [兼容脚本](https://docs.apifox.com/scripts)

## 内容
共18个请求：health、20位查询、MD5截断查询、Query/JSON不同、未查得、
SHA256转换待接入、缺少phone、null/数字phone、字符串/越界pcode、
错误apikey、过期/未来timestamp、错误/缺少sign、损坏gzip、Mongo不可用。
每个用例包含确定的HTTP和业务码断言；成功用例检查SID与六个变量。

当前签名只包含 apikey、pcode、phone、timestamp，按key排序形成
apikey=...&pcode=...&phone=...&timestamp=...&key=SECRET_KEY，再MD5并转大写。
nonce和URL query不参与签名。使用请求原始完整phone，不能先截断再签名。
旧样例的20位形态和nonce已保留，标识替换为合成数据，避免持久保存真实请求信息。

## Fixture（仅独立测试库）
这些文件不插入或删除任何Mongo数据；需要人工准备：
- short_phone：t等于该环境值，v为BSON二进制 gzip("1,2,3,4,5,6")。
- md5_phone：t为其前20位，v同上。
- missing_phone：确保没有匹配文档。
- corrupt_phone：t等于该值，v为非gzip二进制。
服务的 MONGODB_DATABASE / MONGODB_COLLECTION 必须指向该独立测试数据。
禁止运行 cmd/init_mongo.go 为这些用例准备数据；它会删除重建集合。

Mongo不可用用例仅针对专用故障服务，配置不可连接的Mongo端口。
不允许停止业务数据库。该例期待503/9002；Context截止超时是另一分支504/9003，
当前文件不通过任意sleep或停止真实数据库伪造该超时场景。

## 验证边界
JSON结构、前置脚本签名和用例断言可在本地离线检查。
生成成功不代表已在Apifox实导入或已请求真实Mongo；执行后需分别记录这些结果。
重命名用例或再次导入时先预览，避免覆盖现有接口/重复创建用例。
