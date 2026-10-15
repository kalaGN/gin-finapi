package mysql

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/config"
)

type fakeConnector struct {
	query              func(context.Context, string, []driver.NamedValue) (driver.Rows, error)
	exec               func(context.Context, string, []driver.NamedValue) (driver.Result, error)
	begin              func(driver.TxOptions) error
	commits, rollbacks int
}

func (f *fakeConnector) Connect(context.Context) (driver.Conn, error) { return &fakeConn{f}, nil }
func (f *fakeConnector) Driver() driver.Driver                        { return fakeDriver{} }

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) { return nil, errors.New("not used") }

type fakeConn struct{ f *fakeConnector }

func (c *fakeConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not used") }
func (c *fakeConn) Close() error                        { return nil }
func (c *fakeConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *fakeConn) BeginTx(_ context.Context, o driver.TxOptions) (driver.Tx, error) {
	if c.f.begin != nil {
		if err := c.f.begin(o); err != nil {
			return nil, err
		}
	}
	return &fakeTx{c.f}, nil
}
func (c *fakeConn) QueryContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.f.query(ctx, q, a)
}
func (c *fakeConn) ExecContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Result, error) {
	if c.f.exec != nil {
		return c.f.exec(ctx, q, a)
	}
	return driver.RowsAffected(1), nil
}

type fakeTx struct{ f *fakeConnector }

func (tx *fakeTx) Commit() error   { tx.f.commits++; return nil }
func (tx *fakeTx) Rollback() error { tx.f.rollbacks++; return nil }

type fixtureRows struct {
	width int
	rows  [][]driver.Value
}

func (r *fixtureRows) Columns() []string {
	names := make([]string, r.width)
	for i := range names {
		names[i] = strconv.Itoa(i)
	}
	return names
}
func (r *fixtureRows) Close() error { return nil }
func (r *fixtureRows) Next(dst []driver.Value) error {
	if len(r.rows) == 0 {
		return io.EOF
	}
	copy(dst, r.rows[0])
	r.rows = r.rows[1:]
	return nil
}

func testConfig() config.MySQLConfig {
	return config.MySQLConfig{Enabled: true, Host: "127.0.0.1", Port: 3306, Database: "synthetic", User: "synthetic", Password: "private-test-password", TLSMode: "disabled", ConnectTimeout: time.Second, QueryTimeout: time.Second, WriteTimeout: time.Second, MaxOpenConns: 2, MaxIdleConns: 1, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute, SecretKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)), SecretKeyID: "test-v1", SyncEnabled: true, SyncInterval: time.Minute, SyncTimeout: time.Second}
}
func testStore(t *testing.T, f *fakeConnector) *Store {
	t.Helper()
	cfg := testConfig()
	codec, err := newSecretCodec(cfg.SecretKey, cfg.SecretKeyID)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(f)
	db.SetMaxOpenConns(1)
	s := &Store{db: db, cfg: cfg, secrets: codec}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func TestReadCredentialAndCustomerState(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		keyStatus, customerStatus int
		expires                   int64
		corrupt, missing, failed  bool
	}{
		{name: "有效", keyStatus: 1, customerStatus: 1}, {name: "客户禁用", keyStatus: 1}, {name: "Key禁用", customerStatus: 1},
		{name: "到期", keyStatus: 1, customerStatus: 1, expires: 1}, {name: "密文损坏", corrupt: true}, {name: "缺失", missing: true}, {name: "存储故障", failed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeConnector{}
			s := testStore(t, f)
			digest := services.Digest("synthetic-key")
			encrypted, err := s.secrets.encrypt(digest, "synthetic-secret")
			if err != nil {
				t.Fatal(err)
			}
			f.query = func(_ context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
				if a[0].Value != digest || strings.Contains(q, digest) {
					t.Fatal("查询未参数化")
				}
				if tc.failed {
					return nil, errors.New("private-test-password raw database details")
				}
				if tc.missing {
					return &fixtureRows{width: 6}, nil
				}
				if tc.corrupt {
					encrypted = []byte{1}
				}
				return &fixtureRows{width: 6, rows: [][]driver.Value{{digest, encrypted, "test-v1", int64(tc.keyStatus), tc.expires, int64(tc.customerStatus)}}}, nil
			}
			v, found, err := s.ReadCredential(context.Background(), digest)
			if tc.failed || tc.corrupt {
				if found || !errors.Is(err, services.ErrAuthUnavailable) || strings.Contains(err.Error(), "private-test-password") {
					t.Fatal("故障未安全拒绝")
				}
				if tc.corrupt && !errors.Is(err, services.ErrInvalidRecord) {
					t.Fatal("损坏记录未标记")
				}
				return
			}
			if err != nil || found == tc.missing {
				t.Fatalf("found=%t err=%v", found, err)
			}
			if tc.missing {
				return
			}
			want := tc.keyStatus
			if tc.customerStatus != 1 {
				want = 0
			}
			if v.Status != want || v.Secret != "synthetic-secret" || v.ExpiresAt != tc.expires {
				t.Fatal("凭据快照不正确")
			}
		})
	}
}

func TestReadProductAuthorizationBoundaries(t *testing.T) {
	for _, tc := range []struct {
		grant, key, customer               int
		grantExpiry, keyExpiry, wantExpiry int64
		wantStatus                         int
	}{
		{1, 1, 1, 0, 0, 0, 1}, {1, 1, 0, 0, 0, 0, 0}, {0, 1, 1, 0, 0, 0, 0}, {1, 0, 1, 0, 0, 0, 0},
		{1, 1, 1, 200, 100, 100, 1}, {1, 1, 1, 0, 100, 100, 1}, {1, 1, 1, 100, 0, 100, 1},
	} {
		f := &fakeConnector{}
		s := testStore(t, f)
		digest := services.Digest("key")
		f.query = func(_ context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
			if len(a) != 2 || a[0].Value != digest || a[1].Value != int64(10001) {
				t.Fatal("授权条件不正确")
			}
			return &fixtureRows{width: 7, rows: [][]driver.Value{{digest, int64(10001), int64(tc.grant), tc.grantExpiry, int64(tc.key), tc.keyExpiry, int64(tc.customer)}}}, nil
		}
		value, found, err := s.ReadProductAccess(context.Background(), services.ProductKey{Digest: digest, Pcode: 10001})
		if err != nil || !found || value.Status != tc.wantStatus || value.ExpiresAt != tc.wantExpiry {
			t.Fatal("有效授权折算不正确")
		}
	}
}

func TestReadMappingInvalidAndDatabaseFailure(t *testing.T) {
	for _, raw := range []string{`{"a":0}`, `{"a":null}`, `{"a":0,"a":1}`, `{}`, `{"a":-1}`} {
		f := &fakeConnector{query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
			return &fixtureRows{width: 1, rows: [][]driver.Value{{raw}}}, nil
		}}
		s := testStore(t, f)
		value, found, err := s.ReadProductMap(context.Background(), 10001)
		if raw == `{"a":0}` {
			if err != nil || !found || value["a"] != 0 {
				t.Fatal("有效映射读取失败")
			}
		} else if !errors.Is(err, services.ErrInvalidRecord) || found {
			t.Fatal("非法映射未拒绝")
		}
	}
	f := &fakeConnector{query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		return nil, errors.New("raw database details")
	}}
	s := testStore(t, f)
	_, found, err := s.ReadProductMap(context.Background(), 10001)
	if found || !errors.Is(err, services.ErrUnavailable) || strings.Contains(err.Error(), "raw database") {
		t.Fatal("数据库错误未脱敏")
	}
}

func TestSnapshotConsistencyAndPartialFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		f := &fakeConnector{}
		s := testStore(t, f)
		digest := services.Digest("key")
		encrypted, err := s.secrets.encrypt(digest, "secret")
		if err != nil {
			t.Fatal(err)
		}
		f.begin = func(o driver.TxOptions) error {
			if !o.ReadOnly || o.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) {
				t.Fatal("不是一致性只读事务")
			}
			return nil
		}
		f.query = func(_ context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
			switch q {
			case credentialSelect:
				return &fixtureRows{width: 6, rows: [][]driver.Value{{digest, encrypted, "test-v1", int64(1), int64(0), int64(1)}}}, nil
			case accessSelect:
				if fail {
					return nil, errors.New("database failure")
				}
				return &fixtureRows{width: 7, rows: [][]driver.Value{{digest, int64(10001), int64(1), int64(0), int64(1), int64(0), int64(1)}}}, nil
			case "SELECT pcode,variable_map FROM api_products":
				return &fixtureRows{width: 2, rows: [][]driver.Value{{int64(10001), `{"a":0}`}}}, nil
			}
			t.Fatal("非预期查询")
			return nil, nil
		}
		snapshot, err := s.Snapshot(context.Background())
		if fail {
			if err == nil || snapshot.Credentials != nil || f.commits != 0 || f.rollbacks != 1 {
				t.Fatal("部分快照被当作完整结果")
			}
			continue
		}
		if err != nil || snapshot.Validate() != nil || len(snapshot.Credentials) != 1 || len(snapshot.Products) != 1 || f.commits != 1 {
			t.Fatal("完整快照读取失败")
		}
	}
}

func TestMySQLStoreCanceledAndClosed(t *testing.T) {
	f := &fakeConnector{query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		t.Fatal("取消后不应查询")
		return nil, nil
	}}
	s := testStore(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := s.ReadCredential(ctx, services.Digest("key")); !errors.Is(err, context.Canceled) {
		t.Fatal("取消未传播")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ReadCredential(context.Background(), services.Digest("key")); !errors.Is(err, services.ErrAuthUnavailable) {
		t.Fatal("关闭后仍可读取")
	}
}
