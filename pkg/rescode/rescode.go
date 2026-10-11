package rescode

import "fmt"

// CodeDefine 定义了一个错误代码及其对应的错误消息。
type CodeDefine struct {
	Code int    `json:"code"` // Code 是错误代码
	Msg  string `json:"msg"`  // Msg 是错误消息
}

// NewCodeDefine 创建一个新的 CodeDefine 实例。
func NewCodeDefine(code int, msg string) *CodeDefine {
	return &CodeDefine{
		Code: code,
		Msg:  msg,
	}
}

// Error 实现 error 接口
func (c *CodeDefine) Error() string {
	return c.Msg
}

// GetCode 获取错误码
func (c *CodeDefine) GetCode() int {
	return c.Code
}

// GetMsg 获取错误消息
func (c *CodeDefine) GetMsg() string {
	return c.Msg
}

// ==================== 错误码定义 ====================
// 错误码分段规则：
// 0        - 成功
// 1xxx     - 参数错误
// 2xxx     - 认证/鉴权错误
// 3xxx     - 业务逻辑错误
// 4xxx     - 数据库错误
// 5xxx     - 外部服务错误
// 6xxx     - 产品错误
// 9xxx     - 系统错误

const (
	// ========== 成功 ==========
	SuccessCode = 0

	// ========== 参数错误 (1xxx) ==========
	ErrInvalidParam     = 1000 // 参数错误
	ErrMissingParam     = 1001 // 缺少必要参数
	ErrInvalidPhone     = 1002 // 手机号格式错误
	ErrInvalidPcode     = 1003 // pcode参数错误
	ErrInvalidJSON      = 1004 // JSON格式错误
	ErrInvalidTimestamp = 1005 // 时间戳无效或过期

	// ========== 认证/鉴权错误 (2xxx) ==========
	ErrUnauthorized     = 2000 // 未授权
	ErrInvalidApikey    = 2001 // apikey无效
	ErrInvalidSign      = 2002 // 签名验证失败
	ErrTokenExpired     = 2003 // token已过期
	ErrPermissionDenied = 2004 // 权限不足

	// ========== 业务逻辑错误 (3xxx) ==========
	ErrDataNotFound      = 3001 // 数据不存在
	ErrDataAlreadyExist  = 3002 // 数据已存在
	ErrBusinessLogic     = 3003 // 业务逻辑错误
	ErrRateLimitExceeded = 3004 // 超过限流

	// ========== 数据库错误 (4xxx) ==========
	ErrDatabase        = 4000 // 数据库错误
	ErrDatabaseConnect = 4001 // 数据库连接失败
	ErrDatabaseQuery   = 4002 // 数据库查询失败
	ErrDatabaseInsert  = 4003 // 数据库插入失败
	ErrDatabaseUpdate  = 4004 // 数据库更新失败
	ErrDatabaseDelete  = 4005 // 数据库删除失败

	// ========== 外部服务错误 (5xxx) ==========
	ErrExternalService = 5000 // 外部服务错误
	ErrRedisError      = 5001 // Redis错误
	ErrHttpRequest     = 5002 // HTTP请求失败

	// ========== 产品错误 (6xxx) ==========
	ErrProductException = 6001 // 产品异常

	// ========== 系统错误 (9xxx) ==========
	ErrSystemError        = 9000 // 系统错误
	ErrInternalError      = 9001 // 内部错误
	ErrServiceUnavailable = 9002 // 服务不可用
	ErrTimeout            = 9003 // 请求超时

	// ========== 兼容旧错误码 ==========
	Err1110 = 1110 // 旧版：invalid parameter cid
	Err1101 = 1101 // 旧版：illegal parameter apikey
)

// ==================== 错误消息映射 ====================
var codeMessages = map[int]string{
	// 成功
	SuccessCode: "success",

	// 参数错误
	ErrInvalidParam:     "参数错误",
	ErrMissingParam:     "缺少必要参数",
	ErrInvalidPhone:     "手机号格式错误",
	ErrInvalidPcode:     "pcode参数错误，必须在10001-99999之间",
	ErrInvalidJSON:      "JSON格式错误",
	ErrInvalidTimestamp: "时间戳无效或已过期",

	// 认证/鉴权错误
	ErrUnauthorized:     "未授权访问",
	ErrInvalidApikey:    "apikey无效",
	ErrInvalidSign:      "签名验证失败",
	ErrTokenExpired:     "token已过期",
	ErrPermissionDenied: "权限不足",

	// 业务逻辑错误
	ErrDataNotFound:      "数据不存在",
	ErrDataAlreadyExist:  "数据已存在",
	ErrBusinessLogic:     "业务逻辑错误",
	ErrRateLimitExceeded: "请求过于频繁，请稍后再试",

	// 数据库错误
	ErrDatabase:        "数据库错误",
	ErrDatabaseConnect: "数据库连接失败",
	ErrDatabaseQuery:   "数据库查询失败",
	ErrDatabaseInsert:  "数据库插入失败",
	ErrDatabaseUpdate:  "数据库更新失败",
	ErrDatabaseDelete:  "数据库删除失败",

	// 外部服务错误
	ErrExternalService: "外部服务错误",
	ErrRedisError:      "缓存服务错误",
	ErrHttpRequest:     "HTTP请求失败",

	ErrProductException: "产品异常",

	// 系统错误
	ErrSystemError:        "系统错误",
	ErrInternalError:      "服务器内部错误",
	ErrServiceUnavailable: "服务暂时不可用",
	ErrTimeout:            "请求超时",

	// 兼容旧错误码
	Err1110: "invalid parameter cid",
	Err1101: "illegal parameter apikey",
}

// GetCodeMsg 根据错误代码返回对应的错误消息。
func GetCodeMsg(code int) string {
	if msg, ok := codeMessages[code]; ok {
		return msg
	}
	return fmt.Sprintf("未知错误码: %d", code)
}

// GetCodeDefine 根据错误代码返回 CodeDefine 对象
func GetCodeDefine(code int) *CodeDefine {
	return NewCodeDefine(code, GetCodeMsg(code))
}

// IsSuccess 判断是否成功
func IsSuccess(code int) bool {
	return code == SuccessCode
}

// IsParamError 判断是否参数错误
func IsParamError(code int) bool {
	return code >= 1000 && code < 2000
}

// IsAuthError 判断是否认证错误
func IsAuthError(code int) bool {
	return code >= 2000 && code < 3000
}

// IsBusinessError 判断是否业务错误
func IsBusinessError(code int) bool {
	return code >= 3000 && code < 4000
}

// IsDatabaseError 判断是否数据库错误
func IsDatabaseError(code int) bool {
	return code >= 4000 && code < 5000
}

// IsSystemError 判断是否系统错误
func IsSystemError(code int) bool {
	return code >= 9000 && code < 10000
}
