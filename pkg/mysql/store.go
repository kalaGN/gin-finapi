// Package mysql 提供配置持久化适配器，不保存贷款业务数据。
package mysql

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"math"
	"net"
	"os"
	"strconv"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/config"
)

type Store struct {
	db      *sql.DB
	cfg     config.MySQLConfig
	secrets *secretCodec
}

// NewStore 懒连接；不输出 DSN、密码、密文或驱动原始错误。
func NewStore(cfg config.MySQLConfig) (*Store, error) {
	if !cfg.Enabled {
		return nil, errors.New("MYSQL_ENABLED must be true")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	codec, err := newSecretCodec(cfg.SecretKey, cfg.SecretKeyID)
	if err != nil {
		return nil, err
	}
	dc := driver.NewConfig()
	dc.User, dc.Passwd, dc.Net, dc.Addr, dc.DBName = cfg.User, cfg.Password, "tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)), cfg.Database
	dc.Timeout, dc.ReadTimeout, dc.WriteTimeout = cfg.ConnectTimeout, cfg.QueryTimeout, cfg.WriteTimeout
	dc.ParseTime, dc.Loc = true, time.UTC
	dc.Params = map[string]string{"time_zone": "'+00:00'"}
	if err := dc.Apply(driver.Charset("utf8mb4", "")); err != nil {
		return nil, errors.New("invalid MySQL charset configuration")
	}
	dc.Logger = &driver.NopLogger{}
	if cfg.TLSMode == "verify_identity" {
		serverName := cfg.TLSServerName
		if serverName == "" {
			serverName = cfg.Host
		}
		dc.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}
		if cfg.TLSCAFile != "" {
			pem, err := os.ReadFile(cfg.TLSCAFile)
			if err != nil {
				return nil, errors.New("cannot read MYSQL_TLS_CA_FILE")
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, errors.New("invalid MYSQL_TLS_CA_FILE")
			}
			dc.TLS.RootCAs = pool
		}
	}
	connector, err := driver.NewConnector(dc)
	if err != nil {
		return nil, errors.New("invalid MySQL connector configuration")
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	return &Store{db: db, cfg: cfg, secrets: codec}, nil
}

func (s *Store) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.ConnectTimeout)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		return unavailable(err, false)
	}
	return nil
}

func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		return services.ErrUnavailable
	}
	return nil
}

func unavailable(err error, auth bool) error {
	base := services.ErrUnavailable
	if auth {
		base = services.ErrAuthUnavailable
	}
	if errors.Is(err, context.Canceled) {
		return errors.Join(base, context.Canceled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.Join(base, context.DeadlineExceeded)
	}
	return base
}

func invalid(auth bool) error {
	base := services.ErrUnavailable
	if auth {
		base = services.ErrAuthUnavailable
	}
	return errors.Join(base, services.ErrInvalidRecord)
}

type scanner interface{ Scan(...any) error }

const credentialSelect = `SELECT k.api_key_digest, k.secret_ciphertext, k.secret_key_id, k.status, k.expires_at, c.status
FROM api_keys k JOIN api_customers c ON c.id=k.customer_id`

func (s *Store) scanCredential(row scanner) (string, services.Credential, error) {
	var digest, keyID string
	var encrypted []byte
	var keyStatus, customerStatus int
	var expires uint64
	if err := row.Scan(&digest, &encrypted, &keyID, &keyStatus, &expires, &customerStatus); err != nil {
		return "", services.Credential{}, err
	}
	if keyStatus < 0 || keyStatus > 1 || customerStatus < 0 || customerStatus > 1 || expires > math.MaxInt64 {
		return "", services.Credential{}, services.ErrInvalidRecord
	}
	secret, err := s.secrets.decrypt(digest, keyID, encrypted)
	if err != nil {
		return "", services.Credential{}, services.ErrInvalidRecord
	}
	status := keyStatus
	if customerStatus != 1 {
		status = 0
	}
	return digest, services.Credential{Version: 1, Secret: secret, Status: status, ExpiresAt: int64(expires)}, nil
}

func (s *Store) ReadCredential(ctx context.Context, digest string) (services.Credential, bool, error) {
	if !services.ValidDigest(digest) {
		return services.Credential{}, false, invalid(true)
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.QueryTimeout)
	defer cancel()
	got, value, err := s.scanCredential(s.db.QueryRowContext(ctx, credentialSelect+" WHERE k.api_key_digest=?", digest))
	if errors.Is(err, sql.ErrNoRows) {
		return services.Credential{}, false, nil
	}
	if errors.Is(err, services.ErrInvalidRecord) {
		return services.Credential{}, false, invalid(true)
	}
	if err != nil {
		return services.Credential{}, false, unavailable(err, true)
	}
	if got != digest {
		return services.Credential{}, false, invalid(true)
	}
	return value, true, nil
}

const accessSelect = `SELECT k.api_key_digest, a.pcode, a.status, a.expires_at, k.status, k.expires_at, c.status
FROM api_key_products a JOIN api_keys k ON k.id=a.api_key_id JOIN api_customers c ON c.id=k.customer_id`

func scanAccess(row scanner) (services.ProductKey, services.ProductAccess, error) {
	var key services.ProductKey
	var access services.ProductAccess
	var grantStatus, keyStatus, customerStatus int
	var grantExpiry, keyExpiry uint64
	if err := row.Scan(&key.Digest, &key.Pcode, &grantStatus, &grantExpiry, &keyStatus, &keyExpiry, &customerStatus); err != nil {
		return key, access, err
	}
	if !services.ValidDigest(key.Digest) || key.Pcode < 10001 || key.Pcode > 99999 || grantStatus < 0 || grantStatus > 1 || keyStatus < 0 || keyStatus > 1 || customerStatus < 0 || customerStatus > 1 || grantExpiry > math.MaxInt64 || keyExpiry > math.MaxInt64 {
		return key, access, services.ErrInvalidRecord
	}
	status := grantStatus
	if keyStatus != 1 || customerStatus != 1 {
		status = 0
	}
	expires := grantExpiry
	if expires == 0 || keyExpiry != 0 && keyExpiry < expires {
		expires = keyExpiry
	}
	return key, services.ProductAccess{Version: 1, Status: status, ExpiresAt: int64(expires)}, nil
}

func (s *Store) ReadProductAccess(ctx context.Context, key services.ProductKey) (services.ProductAccess, bool, error) {
	if !services.ValidDigest(key.Digest) || key.Pcode < 10001 || key.Pcode > 99999 {
		return services.ProductAccess{}, false, invalid(true)
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.QueryTimeout)
	defer cancel()
	got, value, err := scanAccess(s.db.QueryRowContext(ctx, accessSelect+" WHERE k.api_key_digest=? AND a.pcode=?", key.Digest, key.Pcode))
	if errors.Is(err, sql.ErrNoRows) {
		return services.ProductAccess{}, false, nil
	}
	if errors.Is(err, services.ErrInvalidRecord) {
		return services.ProductAccess{}, false, invalid(true)
	}
	if err != nil {
		return services.ProductAccess{}, false, unavailable(err, true)
	}
	if got != key {
		return services.ProductAccess{}, false, invalid(true)
	}
	return value, true, nil
}

func (s *Store) ReadProductMap(ctx context.Context, pcode int) (map[string]int, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.QueryTimeout)
	defer cancel()
	var raw string
	err := s.db.QueryRowContext(ctx, "SELECT variable_map FROM api_products WHERE pcode=?", pcode).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, unavailable(err, false)
	}
	value, err := services.ParseProductMap(raw)
	if err != nil {
		return nil, false, invalid(false)
	}
	return value, true, nil
}
