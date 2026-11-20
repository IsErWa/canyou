package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"Canyou/internal/config"

	_ "modernc.org/sqlite"
)

func Open(filename string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return nil, fmt.Errorf("创建聊天记录目录失败: %w", err)
	}
	db, err := sql.Open("sqlite", filename)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{db: db}
	if err := store.initialize(); err != nil {
		db.Close()
		return nil, fmt.Errorf("打开聊天数据库失败: %w", err)
	}
	return store, nil
}

// withDeviceDB 在聊天库（config.DatabasePath）上执行操作：每次短时开关连接，避免与聊天室长期互持句柄；
// allowed_devices 表不占用 user_version，不会触发聊天存储的"版本较新"拦截
func withDeviceDB(fn func(db *sql.DB) error) error {
	filename, err := config.DatabasePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", filename)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout=3000"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS allowed_devices (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 did TEXT NOT NULL UNIQUE,
 name TEXT NOT NULL DEFAULT '',
 authorized_at TEXT NOT NULL
)`); err != nil {
		return err
	}
	// 旧版表（did 作主键、无 id 列）就地迁移到带主键 ID 与设备名的新结构，保留存量允许名单
	var idColumns int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('allowed_devices') WHERE name='id'`).Scan(&idColumns); err != nil {
		return err
	}
	if idColumns == 0 {
		if _, err := db.ExecContext(ctx, `
ALTER TABLE allowed_devices RENAME TO allowed_devices_legacy;
CREATE TABLE allowed_devices (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 did TEXT NOT NULL UNIQUE,
 name TEXT NOT NULL DEFAULT '',
 authorized_at TEXT NOT NULL
);
INSERT INTO allowed_devices(did, name, authorized_at) SELECT did, '', authorized_at FROM allowed_devices_legacy;
DROP TABLE allowed_devices_legacy;`); err != nil {
			return err
		}
	}
	// 迁移：为存量表补 color 列（预设头像底色索引，-1 表示未设置，前端按编号散列兜底）
	var colorColumns int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('allowed_devices') WHERE name='color'`).Scan(&colorColumns); err != nil {
		return err
	}
	if colorColumns == 0 {
		if _, err := db.ExecContext(ctx, `ALTER TABLE allowed_devices ADD COLUMN color INTEGER NOT NULL DEFAULT -1`); err != nil {
			return err
		}
	}
	// 主题偏好独立于系统当前深浅色，旧设备默认跟随系统。
	var themeColumns int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('allowed_devices') WHERE name='theme'`).Scan(&themeColumns); err != nil {
		return err
	}
	if themeColumns == 0 {
		if _, err := db.ExecContext(ctx, `ALTER TABLE allowed_devices ADD COLUMN theme TEXT NOT NULL DEFAULT 'system' CHECK(theme IN ('dark','light','system'))`); err != nil {
			return err
		}
	}
	return fn(db)
}
