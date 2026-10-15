package mysql

import (
	"context"
	"database/sql"

	"github.com/kalaGN/gin-finapi/app/services"
)

// Snapshot 在同一个只读可重复读事务内获取全部配置，任何错误都不返回可发布的部分结果。
func (s *Store) Snapshot(ctx context.Context) (snapshot services.ConfigSnapshot, err error) {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.SyncTimeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return snapshot, unavailable(err, false)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	snapshot = services.ConfigSnapshot{Credentials: map[string]services.Credential{}, Products: map[services.ProductKey]services.ProductAccess{}, Mappings: map[int]map[string]int{}}
	if err = s.readCredentialSnapshot(ctx, tx, &snapshot); err != nil {
		return services.ConfigSnapshot{}, err
	}
	if err = s.readMappingSnapshot(ctx, tx, &snapshot); err != nil {
		return services.ConfigSnapshot{}, err
	}
	if err = s.readAccessSnapshot(ctx, tx, &snapshot); err != nil {
		return services.ConfigSnapshot{}, err
	}
	if err = snapshot.Validate(); err != nil {
		return services.ConfigSnapshot{}, invalid(false)
	}
	if err = tx.Commit(); err != nil {
		return services.ConfigSnapshot{}, unavailable(err, false)
	}
	return snapshot, nil
}

func (s *Store) readCredentialSnapshot(ctx context.Context, tx *sql.Tx, snapshot *services.ConfigSnapshot) error {
	rows, err := tx.QueryContext(ctx, credentialSelect)
	if err != nil {
		return unavailable(err, false)
	}
	defer rows.Close()
	for rows.Next() {
		digest, value, err := s.scanCredential(rows)
		if err != nil {
			return invalid(false)
		}
		if _, exists := snapshot.Credentials[digest]; exists {
			return invalid(false)
		}
		snapshot.Credentials[digest] = value
	}
	if err := rows.Err(); err != nil {
		return unavailable(err, false)
	}
	return nil
}

func (s *Store) readMappingSnapshot(ctx context.Context, tx *sql.Tx, snapshot *services.ConfigSnapshot) error {
	rows, err := tx.QueryContext(ctx, "SELECT pcode,variable_map FROM api_products")
	if err != nil {
		return unavailable(err, false)
	}
	defer rows.Close()
	for rows.Next() {
		var pcode int
		var raw string
		if err := rows.Scan(&pcode, &raw); err != nil {
			return invalid(false)
		}
		mapping, err := services.ParseProductMap(raw)
		if err != nil {
			return invalid(false)
		}
		if _, exists := snapshot.Mappings[pcode]; exists {
			return invalid(false)
		}
		snapshot.Mappings[pcode] = mapping
	}
	if err := rows.Err(); err != nil {
		return unavailable(err, false)
	}
	return nil
}

func (s *Store) readAccessSnapshot(ctx context.Context, tx *sql.Tx, snapshot *services.ConfigSnapshot) error {
	rows, err := tx.QueryContext(ctx, accessSelect)
	if err != nil {
		return unavailable(err, false)
	}
	defer rows.Close()
	for rows.Next() {
		key, access, err := scanAccess(rows)
		if err != nil {
			return invalid(false)
		}
		if _, exists := snapshot.Products[key]; exists {
			return invalid(false)
		}
		snapshot.Products[key] = access
	}
	if err := rows.Err(); err != nil {
		return unavailable(err, false)
	}
	return nil
}
