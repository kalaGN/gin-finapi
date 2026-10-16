# HTTP 接口与鉴权

核对日期：2026-10-16。来源：`routes/api.go`、`app/middleware/loan_auth.go`、
`app/services/phone_lookup.go`、`app/Http/controllers/`。本文从原README整理，不改变接口行为。

[文档索引](../README.md) · [配置与存储](configuration.md) · [开发、部署与日志](operations.md)

## 清除本进程配置缓存

配置独立 `CONFIG_CACHE_TOKEN` 并重启后启用 `DELETE /config/cache`；默认空值不注册路由（404）。
只从 `Authorization: Bearer <token>` 认证，不接受客户API Key、Query或Body中的Token；
未认证返回401，读取器已关闭或调用取消返回503。应使用HTTPS并限制管理路径的网络访问。

```bash
curl --request DELETE 'http://127.0.0.1:8082/config/cache' \
  --header 'Authorization: Bearer <独立管理Token>'
```

成功返回实际清理数量，缓存关闭或重复清理时可为0：

```json
{"cleared":{"credentials":1,"product_access":1,"product_maps":1}}
```

清理凭据、产品授权、产品映射的内存快照，不读写Redis/MySQL/Mongo；清理前在途读取不会回填旧值，
但已接受的请求仍可完成，后续请求可立即回源并重新缓存。多实例须分别调用，不能据此承诺立即撤权。
如果修改的是MySQL，须先完成MySQL→Redis同步，否则清理L1后仍可能读取Redis旧配置。
该管理接口无业务status字段、不计费；完成日志input/status为null、success/charge为0，另记录SID及清理数量审计。
管理接口不采集Body/Header/Query，错误响应为 `{"error":"未授权访问"}` 或 `{"error":"服务暂时不可用"}`。
详见 [清理设计](../specs/20261015-内存缓存清理-设计.md)。

## 健康检查

```text
GET /health
```

返回：`ok`

## 数据接口

```text
POST /api
Content-Type: application/json
```

**请求体：**

```json
{
  "phone": "0123456789abcdef0123456789abcdef",
  "pcode": 10001,
  "apikey": "your_api_key",
  "nonce": "3093",
  "timestamp": 1730000000000,
  "sign": "UPPERCASE_MD5_32_CHARS"
}
```

phone 必须是长度为 32 或 64 字节的非空字符串，其他非空长度返回 HTTP 400/1002；旧 20 位标识不再接受。
所有 32 位字符串统一截前 20 位查 Mongo t，不要求十六进制、保留大小写；64 位十六进制 SHA-256
转换服务尚未接入（TODO），返回 HTTP 503/9002，不直接查库或对摘要再哈希。
64 位非十六进制字符串仍原样查询；不对标识重新哈希或自动补齐。
签名对 apikey、nonce、timestamp 按 key 排序拼接，加该 API Key 对应的 secret 后做大写 MD5：

```text
apikey=...&nonce=...&timestamp=...&key=<secret>
```

nonce 必须是非空 JSON 字符串，保留前导零，不转成数字；缺失/空串为 400/1001，类型错误为 400/1000。
不接受旧四字段或五字段签名，客户端须同步更新；本次未增加 nonce 去重，仍不能阻止窗口内的重复请求。
pcode、phone 不参与签名，但仍校验业务参数和产品授权；签名不保护这两个字段的完整性。
请求 apikey 必须对应有效配置源中启用且未到期的凭据，且对请求 pcode 有启用授权；timestamp 为过去五分钟内的 Unix 毫秒。
上面的 timestamp 和 sign 只是结构示意，联调时需要实时生成。
[设计与请求示例](../specs/20261011-请求加密鉴权-设计.md)。

请求必须使用 application/json，Body 最大 64 KiB。无效 API Key 返回 400/2001；
phone 类型错误返回 400/1002、缺失或空字符串返回 400/1001。其他错误映射保持原约定。
URL 参数不参与 JSON 验签，不替代 JSON nonce；PHONE_AES_KEY 不再读取或必填。

API Key 产品授权从 Redis String `gin-finapi:auth:apikey-product:v1:<sha256(apikey)>:<pcode>` 读取，
Value 为 `{"version":1,"status":1,"expires_at":0}`。凭据、授权和产品映射统一经 `app/configdata` 读取：
内存优先，未命中读 Redis 并回填，每 5 分钟后台刷新已缓存项；请求不判断缓存年龄。
`MYSQL_ENABLED=true` 时，Redis 缺失、损坏、连接故障或自身超时后读取 MySQL；有效禁用/到期记录不回退。
关闭 MySQL 时保持原行为。请求回源不写 Redis，成功可回填内存；调用方取消后不启动下一层读取。
`REDIS_CACHE_ENABLED=false` 关闭三类内存缓存，不回填、不合并读取、不启动 L1 刷新，但仍保留已启用的 MySQL 回源。
默认 `true`；修改后重启生效。两层存储均不可用才返回服务异常，没有旧内存快照兜底；认证与到期检查不变。
授权缺失、禁用或到期返回 HTTP 403/2004；回源故障或损坏授权返回 HTTP 503/9002。
状态与业务到期时间每次请求检查。后台临时故障保留旧快照；明确删除或损坏清除旧值，禁用替换为拒绝状态。
撤销/轮换通常在下一轮同步完成后生效；持续故障时传播延迟无硬上限，不适合立即撤销。

**响应：**

```json
{
  "status": 0,
  "msg": "success",
  "sid": "xxxxxxxxxxxx",
  "data": {
    "var100001": 0,
    "var100002": 1,
    "var100003": 2,
    "var100004": 3,
    "var100005": 4,
    "var100006": 5
  }
}
```
