package mysql

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"os"
	"path/filepath"
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
	ping               func(context.Context) error
	commitErr          error
	commits, rollbacks int
}

func (f *fakeConnector) Connect(context.Context) (driver.Conn, error) { return &fakeConn{f}, nil }
func (f *fakeConnector) Driver() driver.Driver                        { return fakeDriver{} }

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) { return nil, errors.New("not used") }

type fakeConn struct{ f *fakeConnector }

func (c *fakeConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not used") }
func (c *fakeConn) Close() error                        { return nil }
func (c *fakeConn) Ping(ctx context.Context) error {
	if c.f.ping != nil {
		return c.f.ping(ctx)
	}
	return ctx.Err()
}
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

func (tx *fakeTx) Commit() error   { tx.f.commits++; return tx.f.commitErr }
func (tx *fakeTx) Rollback() error { tx.f.rollbacks++; return nil }

type fixtureRows struct {
	width int
	rows  [][]driver.Value
	err   error
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
		if r.err != nil {
			return r.err
		}
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

func TestMySQLConstructorTLSAndPoolConfiguration(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, private.Public(), private)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.pem")
	invalidCA := filepath.Join(dir, "invalid.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(invalidCA, []byte("synthetic-private-invalid-ca"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*config.MySQLConfig)
		bad    bool
	}{
		{"本地配置", func(*config.MySQLConfig) {}, false},
		{"验证系统CA", func(c *config.MySQLConfig) { c.TLSMode = "verify_identity" }, false},
		{"自定义CA和名称", func(c *config.MySQLConfig) {
			c.TLSMode = "verify_identity"
			c.TLSCAFile = ca
			c.TLSServerName = "synthetic.invalid"
		}, false},
		{"关闭MySQL", func(c *config.MySQLConfig) { c.Enabled = false }, true},
		{"缺失用户", func(c *config.MySQLConfig) { c.User = "" }, true},
		{"CA文件缺失", func(c *config.MySQLConfig) {
			c.TLSMode = "verify_identity"
			c.TLSCAFile = filepath.Join(dir, "missing.pem")
		}, true},
		{"CA损坏", func(c *config.MySQLConfig) { c.TLSMode = "verify_identity"; c.TLSCAFile = invalidCA }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig()
			tc.change(&cfg)
			store, err := NewStore(cfg)
			if (err != nil) != tc.bad {
				t.Fatal("构造校验结果错误")
			}
			if err != nil {
				if strings.Contains(err.Error(), cfg.Password) || strings.Contains(err.Error(), "private-invalid-ca") {
					t.Fatal("构造泄露敏感信息")
				}
				return
			}
			if stats := store.db.Stats(); stats.OpenConnections != 0 || stats.MaxOpenConnections != cfg.MaxOpenConns {
				t.Fatal("构造发生连接或未配置连接池")
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMySQLPingPreservesSafeFailureClassification(t *testing.T) {
	for _, failure := range []error{nil, errors.New("synthetic-private-connect-detail"), context.DeadlineExceeded, context.Canceled} {
		f := &fakeConnector{ping: func(ctx context.Context) error {
			if _, ok := ctx.Deadline(); !ok {
				t.Error("连接探测无超时预算")
			}
			return failure
		}}
		err := testStore(t, f).Ping(context.Background())
		if failure == nil {
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		if !errors.Is(err, services.ErrUnavailable) || strings.Contains(err.Error(), "private-connect") {
			t.Fatal("连接错误未安全转换")
		}
		if (failure == context.Canceled || failure == context.DeadlineExceeded) && !errors.Is(err, failure) {
			t.Fatal("取消或超时语义丢失")
		}
	}
}

func TestMySQLProductAccessRejectsMissingCorruptAndMismatchedRows(t *testing.T) {
	digest := services.Digest("key")
	key := services.ProductKey{Digest: digest, Pcode: 10001}
	for _, tc := range []struct {
		name     string
		row      []driver.Value
		queryErr error
		want     error
	}{
		{"缺失", nil, nil, nil},
		{"SQL故障", nil, errors.New("synthetic-private-detail"), services.ErrAuthUnavailable},
		{"返回错误产品", []driver.Value{digest, int64(10002), int64(1), int64(0), int64(1), int64(0), int64(1)}, nil, services.ErrInvalidRecord},
		{"非法状态", []driver.Value{digest, int64(10001), int64(2), int64(0), int64(1), int64(0), int64(1)}, nil, services.ErrInvalidRecord},
		{"超大到期", []driver.Value{digest, int64(10001), int64(1), "18446744073709551615", int64(1), int64(0), int64(1)}, nil, services.ErrInvalidRecord},
		{"扫描失败", []driver.Value{digest, "not-an-int", int64(1), int64(0), int64(1), int64(0), int64(1)}, nil, services.ErrAuthUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeConnector{query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
				if tc.queryErr != nil {
					return nil, tc.queryErr
				}
				rows := &fixtureRows{width: 7}
				if tc.row != nil {
					rows.rows = [][]driver.Value{tc.row}
				}
				return rows, nil
			}}
			_, found, err := testStore(t, f).ReadProductAccess(context.Background(), key)
			if found || !errors.Is(err, tc.want) {
				t.Fatal("授权坏数据或缺失未安全拒绝")
			}
			if err != nil && strings.Contains(err.Error(), "private-detail") {
				t.Fatal("错误泄漏")
			}
		})
	}
	f := &fakeConnector{query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		t.Fatal("无效参数触及SQL")
		return nil, nil
	}}
	s := testStore(t, f)
	if _, _, err := s.ReadCredential(context.Background(), "raw-key"); !errors.Is(err, services.ErrInvalidRecord) {
		t.Fatal("无效摘要未拒绝")
	}
	for _, invalid := range []services.ProductKey{{Digest: "raw-key", Pcode: 10001}, {Digest: digest, Pcode: 10000}, {Digest: digest, Pcode: 100000}} {
		if _, _, err := s.ReadProductAccess(context.Background(), invalid); !errors.Is(err, services.ErrInvalidRecord) {
			t.Fatal("非法授权参数未拒绝")
		}
	}
}

func TestSnapshotRejectsEveryPartialFailure(t *testing.T) {
	for _, scenario := range []string{"begin", "credential-query", "credential-scan", "credential-duplicate", "credential-iteration", "mapping-query", "mapping-scan", "mapping-invalid", "mapping-duplicate", "mapping-iteration", "access-query", "access-scan", "access-duplicate", "access-iteration", "orphan", "commit"} {
		t.Run(scenario, func(t *testing.T) {
			failure := errors.New("synthetic-private-database-detail")
			f := &fakeConnector{}
			s := testStore(t, f)
			digest := services.Digest("key")
			encrypted, err := s.secrets.encrypt(digest, "secret")
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "begin" {
				f.begin = func(driver.TxOptions) error { return failure }
			}
			if scenario == "commit" {
				f.commitErr = failure
			}
			f.query = func(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
				stage := "mapping"
				row := []driver.Value{int64(10001), `{"a":0}`}
				if q == credentialSelect {
					stage = "credential"
					row = []driver.Value{digest, encrypted, "test-v1", int64(1), int64(0), int64(1)}
				}
				if q == accessSelect {
					stage = "access"
					row = []driver.Value{digest, int64(10001), int64(1), int64(0), int64(1), int64(0), int64(1)}
				}
				if scenario == stage+"-query" {
					return nil, failure
				}
				if scenario == stage+"-scan" {
					row[0] = nil
				}
				if scenario == "mapping-invalid" && stage == "mapping" {
					row[1] = `{}`
				}
				if scenario == "orphan" && stage == "access" {
					row[1] = int64(10002)
				}
				rows := &fixtureRows{width: len(row), rows: [][]driver.Value{row}}
				if scenario == stage+"-duplicate" {
					rows.rows = append(rows.rows, row)
				}
				if scenario == stage+"-iteration" {
					rows.err = failure
				}
				return rows, nil
			}
			snapshot, err := s.Snapshot(context.Background())
			if err == nil || snapshot.Credentials != nil || snapshot.Products != nil || snapshot.Mappings != nil || strings.Contains(err.Error(), "private-database") {
				t.Fatal("失败暴露部分结果或敏感信息")
			}
			if scenario == "commit" {
				if f.commits != 1 {
					t.Fatal("未模拟提交错误")
				}
			} else if f.commits != 0 {
				t.Fatal("部分失败仍提交")
			}
			if scenario != "begin" && scenario != "commit" && f.rollbacks != 1 {
				t.Fatal("失败事务未回滚")
			}
		})
	}
}

func TestMySQLMissingMappingAndCompleteEmptySnapshot(t *testing.T) {
	f := &fakeConnector{query: func(context.Context, string, []driver.NamedValue) (driver.Rows, error) { return &fixtureRows{}, nil }}
	s := testStore(t, f)
	if value, found, err := s.ReadProductMap(context.Background(), 10001); err != nil || found || value != nil {
		t.Fatal("缺失映射不应伪装成故障或默认映射")
	}
	snapshot, err := s.Snapshot(context.Background())
	if err != nil || snapshot.Validate() != nil || len(snapshot.Credentials)+len(snapshot.Products)+len(snapshot.Mappings) != 0 || f.commits != 1 {
		t.Fatal("完整空库与读取失败未区分")
	}
}
