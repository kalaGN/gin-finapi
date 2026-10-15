package mysql

import (
	"context"
	_ "embed"
	"errors"
	"strings"
)

//go:embed schema.sql
var schemaSQL string

// ApplySchema 仅供显式初始化使用，不在应用启动时建表；DDL 可能部分成功，允许安全重试。
func (s *Store) ApplySchema(ctx context.Context) error {
	for _, statement := range strings.Split(schemaSQL, ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		writeCtx, cancel := context.WithTimeout(ctx, s.cfg.WriteTimeout)
		_, err := s.db.ExecContext(writeCtx, statement)
		cancel()
		if err != nil {
			return errors.New("创建 MySQL 表失败，可能已部分完成；请检查权限后重试")
		}
	}
	return nil
}
