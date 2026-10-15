package services

import (
	"encoding/json"
	"io"
	"strconv"
	"strings"
)

// ParseProductMap 校验各存储共享的变量映射格式，不限制字段数量。
func ParseProductMap(raw string) (map[string]int, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrProductMapping
	}
	result := make(map[string]int)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, ErrProductMapping
		}
		name, ok := token.(string)
		if !ok || strings.TrimSpace(name) == "" {
			return nil, ErrProductMapping
		}
		if _, exists := result[name]; exists {
			return nil, ErrProductMapping
		}
		// 解析原始整数文本，拒绝 null、字符串、小数和指数表示。
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, ErrProductMapping
		}
		index, err := strconv.Atoi(string(value))
		if err != nil || index < 0 || index == int(^uint(0)>>1) {
			return nil, ErrProductMapping
		}
		result[name] = index
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') || len(result) == 0 {
		return nil, ErrProductMapping
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		return nil, ErrProductMapping
	}
	return result, nil
}
