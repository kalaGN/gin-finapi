package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/kalaGN/gin-finapi/app/services"
)

const sampleCustomerName = "Gin FinAPI 本地示例客户"
const sampleMapping = `{"var100001":0,"var100002":1,"var100003":2,"var100004":3,"var100005":4,"var100006":5}`

type SeedStats struct{ CustomersAdded, KeysAdded, ProductsAdded, GrantsAdded int }

// SeedSamples 只添加缺失的合成样例；固定 ID 冲突即回滚，不修改已有 secret、禁用状态或映射。
func (s *Store) SeedSamples(ctx context.Context) (stats SeedStats, err error) {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.WriteTimeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return stats, unavailable(err, false)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var name string
	err = tx.QueryRowContext(ctx, "SELECT name FROM api_customers WHERE id=1 FOR UPDATE").Scan(&name)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = tx.ExecContext(ctx, "INSERT INTO api_customers (id,name,status,remark) VALUES (1,?,1,?)", sampleCustomerName, "仅用于本地合成测试")
		if err == nil {
			stats.CustomersAdded++
		}
	case err == nil && name != sampleCustomerName:
		err = errors.New("样例客户 ID 已被其他客户使用，未写入数据")
	}
	if err != nil {
		return SeedStats{}, errors.New("准备样例客户失败，事务已回滚")
	}
	for _, pcode := range []int{10001, 10002} {
		var exists int
		err = tx.QueryRowContext(ctx, "SELECT pcode FROM api_products WHERE pcode=? FOR UPDATE", pcode).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			_, err = tx.ExecContext(ctx, "INSERT INTO api_products (pcode,name,variable_map,remark) VALUES (?,?,?,?)", pcode, "本地示例产品", sampleMapping, "与 init_redis 六变量格式一致")
			if err == nil {
				stats.ProductsAdded++
			}
		}
		if err != nil {
			return SeedStats{}, errors.New("准备样例产品失败，事务已回滚")
		}
	}
	for _, sample := range []struct {
		id                int
		key, secret, name string
	}{
		{1, "example-test-key", "example-test-secret", "正式业务示例 Key"},
		{2, "example-test-key-2", "example-test-secret-2", "独立授权示例 Key"},
	} {
		digest := services.Digest(sample.key)
		var id, customer int
		var existingDigest string
		err = tx.QueryRowContext(ctx, "SELECT id,customer_id,api_key_digest FROM api_keys WHERE id=? OR api_key_digest=? FOR UPDATE", sample.id, digest).Scan(&id, &customer, &existingDigest)
		if errors.Is(err, sql.ErrNoRows) {
			var encrypted []byte
			encrypted, err = s.secrets.encrypt(digest, sample.secret)
			if err == nil {
				_, err = tx.ExecContext(ctx, "INSERT INTO api_keys (id,customer_id,name,api_key_digest,secret_ciphertext,secret_key_id,status,expires_at,remark) VALUES (?,1,?,?,?,?,1,0,?)", sample.id, sample.name, digest, encrypted, s.cfg.SecretKeyID, "公开合成样例，禁止用于生产")
			}
			if err == nil {
				stats.KeysAdded++
			}
		} else if err == nil && (id != sample.id || customer != 1 || existingDigest != digest) {
			err = errors.New("样例 Key ID 或摘要冲突")
		}
		if err != nil {
			return SeedStats{}, errors.New("准备样例凭据失败或发生 ID 冲突，事务已回滚")
		}
	}
	for _, grant := range []struct{ keyID, pcode int }{{1, 10001}, {1, 10002}, {2, 10001}} {
		var exists int
		err = tx.QueryRowContext(ctx, "SELECT status FROM api_key_products WHERE api_key_id=? AND pcode=? FOR UPDATE", grant.keyID, grant.pcode).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			_, err = tx.ExecContext(ctx, "INSERT INTO api_key_products (api_key_id,pcode,status,expires_at,remark) VALUES (?,?,1,0,?)", grant.keyID, grant.pcode, "本地样例授权")
			if err == nil {
				stats.GrantsAdded++
			}
		}
		if err != nil {
			return SeedStats{}, errors.New("准备样例授权失败，事务已回滚")
		}
	}
	if err = tx.Commit(); err != nil {
		return SeedStats{}, unavailable(err, false)
	}
	return stats, nil
}
