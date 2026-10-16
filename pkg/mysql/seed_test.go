package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/kalaGN/gin-finapi/app/services"
)

func TestSeedSamplesInsertsEncryptedRecords(t *testing.T) {
	writes := 0
	f := &fakeConnector{
		begin: func(o driver.TxOptions) error {
			if o.ReadOnly || o.Isolation != driver.IsolationLevel(sql.LevelSerializable) {
				t.Fatal("样例写入事务隔离不正确")
			}
			return nil
		},
		query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
			return &fixtureRows{}, nil
		},
	}
	s := testStore(t, f)
	f.exec = func(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
		writes++
		if strings.HasPrefix(q, "INSERT INTO api_keys ") {
			digest, ok := args[2].Value.(string)
			if !ok || !services.ValidDigest(digest) {
				t.Fatal("未保存摘要")
			}
			encrypted, ok := args[3].Value.([]byte)
			if !ok {
				t.Fatal("未保存密文")
			}
			if _, err := s.secrets.decrypt(digest, args[4].Value.(string), encrypted); err != nil {
				t.Fatal("样例密文无法解密")
			}
		}
		return driver.RowsAffected(1), nil
	}
	stats, err := s.SeedSamples(context.Background())
	if err != nil || stats != (SeedStats{1, 2, 2, 3}) || writes != 8 || f.commits != 1 || f.rollbacks != 0 {
		t.Fatal("样例事务或新增数量错误")
	}
}

func TestSeedSamplesPreservesExistingAndRollsBackConflict(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		f := &fakeConnector{}
		f.query = func(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
			var values []driver.Value
			switch {
			case strings.Contains(q, "FROM api_customers"):
				name := sampleCustomerName
				if conflict {
					name = "另一个客户"
				}
				values = []driver.Value{name}
			case strings.Contains(q, "FROM api_products"):
				values = []driver.Value{args[0].Value}
			case strings.Contains(q, "FROM api_keys"):
				values = []driver.Value{args[0].Value, int64(1), args[1].Value}
			case strings.Contains(q, "FROM api_key_products"):
				values = []driver.Value{int64(0)}
			default:
				t.Fatal("非预期查询")
			}
			return &fixtureRows{width: len(values), rows: [][]driver.Value{values}}, nil
		}
		f.exec = func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
			t.Fatal("已有样例不应被更新或插入")
			return nil, errors.New("unexpected write")
		}
		s := testStore(t, f)
		stats, err := s.SeedSamples(context.Background())
		if stats != (SeedStats{}) || (err != nil) != conflict {
			t.Fatal("已有值未保留或冲突未报告")
		}
		if conflict && (f.rollbacks != 1 || f.commits != 0) || !conflict && (f.commits != 1 || f.rollbacks != 0) {
			t.Fatal("事务结束方式错误")
		}
	}
}

func TestApplySchemaStopsOnFailure(t *testing.T) {
	for _, failAt := range []int{0, 2} {
		calls := 0
		f := &fakeConnector{exec: func(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
			calls++
			if !strings.Contains(q, "CREATE TABLE IF NOT EXISTS") || strings.Contains(q, "DROP") {
				t.Fatal("Schema包含非幂等建表操作")
			}
			if calls == failAt {
				return nil, errors.New("private-driver-detail")
			}
			return driver.RowsAffected(0), nil
		}}
		err := testStore(t, f).ApplySchema(context.Background())
		if (err != nil) != (failAt != 0) || failAt == 0 && calls != 4 || failAt != 0 && calls != failAt {
			t.Fatal("Schema错误未停止")
		}
		if err != nil && strings.Contains(err.Error(), "private-driver-detail") {
			t.Fatal("Schema泄露驱动错误")
		}
	}
}

func TestSeedSamplesRollsBackAtEveryWriteFailure(t *testing.T) {
	for failAt := 1; failAt <= 8; failAt++ {
		t.Run(fmt.Sprintf("第%d次写入失败", failAt), func(t *testing.T) {
			writes := 0
			f := &fakeConnector{query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) { return &fixtureRows{}, nil }, exec: func(context.Context, string, []driver.NamedValue) (driver.Result, error) {
				writes++
				if writes == failAt {
					return nil, errors.New("synthetic-private-write-detail")
				}
				return driver.RowsAffected(1), nil
			}}
			stats, err := testStore(t, f).SeedSamples(context.Background())
			if err == nil || strings.Contains(err.Error(), "private-write") || stats != (SeedStats{}) || f.commits != 0 || f.rollbacks != 1 || writes != failAt {
				t.Fatal("部分种子写入未回滚或继续执行")
			}
		})
	}
	for _, stage := range []string{"begin", "commit", "key-conflict"} {
		t.Run(stage, func(t *testing.T) {
			f := &fakeConnector{query: func(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
				if stage == "key-conflict" {
					switch {
					case strings.Contains(q, "FROM api_customers"):
						return &fixtureRows{width: 1, rows: [][]driver.Value{{sampleCustomerName}}}, nil
					case strings.Contains(q, "FROM api_products"):
						return &fixtureRows{width: 1, rows: [][]driver.Value{{int64(10001)}}}, nil
					case strings.Contains(q, "FROM api_keys"):
						return &fixtureRows{width: 3, rows: [][]driver.Value{{int64(99), int64(2), services.Digest("other")}}}, nil
					}
				}
				return &fixtureRows{}, nil
			}}
			failure := errors.New("synthetic-private-transaction-detail")
			if stage == "begin" {
				f.begin = func(driver.TxOptions) error { return failure }
			}
			if stage == "commit" {
				f.commitErr = failure
			}
			stats, err := testStore(t, f).SeedSamples(context.Background())
			if err == nil || stats != (SeedStats{}) || strings.Contains(err.Error(), "private-transaction") {
				t.Fatal("事务错误未安全返回")
			}
			if stage == "key-conflict" && (f.commits != 0 || f.rollbacks != 1) {
				t.Fatal("Key冲突未回滚")
			}
		})
	}
}
