package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const (
	defaultChatLimit = 200
	maxChatLimit     = 10000
	chatDBTimeout    = 5 * time.Second
)

type chatMessage struct {
	ID       int64  `json:"id"`
	ClientID string `json:"clientId"`
	No       int64  `json:"no"`    // 发送方在 allowed_devices 的主键编号，读取时映射填充，不落库
	Color    int    `json:"color"` // 发送方预设底色索引，读取时映射填充，不落库；-1 表示未设置
	Name     string `json:"name"`
	IP       string `json:"ip"`
	Time     string `json:"time"`
	Text     string `json:"text"`
}

type chatStore struct{ db *sql.DB }

// chatDatabasePath 与原有 Roaming 配置分开，聊天记录放在用户指定的 Local 目录。
func chatDatabasePath() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, "AppData", "Local")
	}
	return filepath.Join(base, "Programs", "Canyou", "chat.db"), nil
}

func openChatStore(filename string) (*chatStore, error) {
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return nil, fmt.Errorf("创建聊天记录目录失败: %w", err)
	}
	db, err := sql.Open("sqlite", filename)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &chatStore{db: db}
	if err := store.initialize(); err != nil {
		db.Close()
		return nil, fmt.Errorf("打开聊天数据库失败: %w", err)
	}
	return store, nil
}

func (s *chatStore) initialize() error {
	ctx, cancel := context.WithTimeout(context.Background(), chatDBTimeout)
	defer cancel()
	if _, err := s.db.ExecContext(ctx, "PRAGMA busy_timeout=3000"); err != nil {
		return err
	}
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > 1 {
		return errors.New("数据库版本较新，请使用新版 Canyou")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if version == 0 {
		_, err = tx.ExecContext(ctx, `
CREATE TABLE messages (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 client_id TEXT NOT NULL, name TEXT NOT NULL, ip TEXT NOT NULL,
 sent_at TEXT NOT NULL, body TEXT NOT NULL
);
CREATE TABLE settings (id INTEGER PRIMARY KEY CHECK(id=1), retention INTEGER NOT NULL CHECK(retention BETWEEN 1 AND 10000));
INSERT INTO settings(id,retention) VALUES(1,200);
PRAGMA user_version=1;`)
		if err != nil {
			return err
		}
	}
	var limit int
	if err = tx.QueryRowContext(ctx, "SELECT retention FROM settings WHERE id=1").Scan(&limit); err != nil {
		return err
	}
	if limit < 1 || limit > maxChatLimit {
		return errors.New("聊天记录保留设置损坏")
	}
	if err = pruneChatMessages(ctx, tx, limit); err != nil {
		return err
	}
	return tx.Commit()
}

func pruneChatMessages(ctx context.Context, tx *sql.Tx, limit int) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE id NOT IN (SELECT id FROM messages ORDER BY id DESC LIMIT ?)`, limit)
	return err
}

func (s *chatStore) snapshot() ([]chatMessage, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), chatDBTimeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	var limit int
	if err = tx.QueryRowContext(ctx, "SELECT retention FROM settings WHERE id=1").Scan(&limit); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,client_id,name,ip,sent_at,body FROM messages ORDER BY id")
	if err != nil {
		return nil, 0, err
	}
	messages := make([]chatMessage, 0)
	for rows.Next() {
		var m chatMessage
		if err = rows.Scan(&m.ID, &m.ClientID, &m.Name, &m.IP, &m.Time, &m.Text); err != nil {
			rows.Close()
			return nil, 0, err
		}
		messages = append(messages, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	// 用门禁 allowed_devices 的 did→主键编号/底色标注历史消息的设备序号与头像底色；表不存在或查询失败时保持 0/-1
	if numRows, qErr := tx.QueryContext(ctx, `SELECT did, id, color FROM allowed_devices`); qErr == nil {
		numbers := map[string]int64{}
		colors := map[string]int{}
		for numRows.Next() {
			var did string
			var id int64
			var color int
			if numRows.Scan(&did, &id, &color) == nil {
				numbers[did] = id
				colors[did] = color
			}
		}
		numRows.Close()
		for i := range messages {
			messages[i].No = numbers[messages[i].ClientID]
			if color, ok := colors[messages[i].ClientID]; ok {
				messages[i].Color = color
			} else {
				messages[i].Color = -1
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, err
	}
	return messages, limit, nil
}

// appendMessage 写入与裁剪在同一事务内，调用方只广播已提交的数据。
func (s *chatStore) appendMessage(m chatMessage) (chatMessage, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), chatDBTimeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return m, 0, err
	}
	defer tx.Rollback()
	var limit int
	if err = tx.QueryRowContext(ctx, "SELECT retention FROM settings WHERE id=1").Scan(&limit); err != nil {
		return m, 0, err
	}
	result, err := tx.ExecContext(ctx, "INSERT INTO messages(client_id,name,ip,sent_at,body) VALUES(?,?,?,?,?)", m.ClientID, m.Name, m.IP, m.Time, m.Text)
	if err != nil {
		return m, 0, err
	}
	if m.ID, err = result.LastInsertId(); err != nil {
		return m, 0, err
	}
	if err = pruneChatMessages(ctx, tx, limit); err != nil {
		return m, 0, err
	}
	return m, limit, tx.Commit()
}

func (s *chatStore) setLimit(limit int) (int64, error) {
	if limit < 1 || limit > maxChatLimit {
		return 0, errors.New("保留条数必须为 1–10000 的整数")
	}
	ctx, cancel := context.WithTimeout(context.Background(), chatDBTimeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE settings SET retention=? WHERE id=1", limit); err != nil {
		return 0, err
	}
	if err = pruneChatMessages(ctx, tx, limit); err != nil {
		return 0, err
	}
	var oldest int64
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE(MIN(id),0) FROM messages").Scan(&oldest); err != nil {
		return 0, err
	}
	return oldest, tx.Commit()
}

// deviceRowID 返回门禁 allowed_devices 中该设备的主键编号，供聊天标注设备序号；未登记或表不存在时返回 0
func (s *chatStore) deviceRowID(did string) int64 {
	ctx, cancel := context.WithTimeout(context.Background(), chatDBTimeout)
	defer cancel()
	var id int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM allowed_devices WHERE did=?`, did).Scan(&id); err != nil {
		return 0
	}
	return id
}

// deviceMeta 一次读取设备的主键编号与预设底色；未登记或表不存在时返回 (0, -1)
func (s *chatStore) deviceMeta(did string) (int64, int) {
	ctx, cancel := context.WithTimeout(context.Background(), chatDBTimeout)
	defer cancel()
	var id int64
	var color int
	if err := s.db.QueryRowContext(ctx, `SELECT id, color FROM allowed_devices WHERE did=?`, did).Scan(&id, &color); err != nil {
		return 0, -1
	}
	return id, color
}

// deviceName 读取门禁在 allowed_devices 登记的权威设备名；表不存在或该设备未登记时返回空串
func (s *chatStore) deviceName(did string) string {
	ctx, cancel := context.WithTimeout(context.Background(), chatDBTimeout)
	defer cancel()
	var name string
	if err := s.db.QueryRowContext(ctx, `SELECT name FROM allowed_devices WHERE did=?`, did).Scan(&name); err != nil {
		return ""
	}
	return name
}

// updateDeviceName 改名时同步门禁名单里的权威设备名：命中已有 did 只更新 name、保留原授权时间；
// 若该设备尚未登记（如未走解锁）则补登记一行。allowed_devices 表由门禁启动时创建，与聊天同库可直接访问
func (s *chatStore) updateDeviceName(did, name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), chatDBTimeout)
	defer cancel()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO allowed_devices(did, name, authorized_at) VALUES(?, ?, ?)
		 ON CONFLICT(did) DO UPDATE SET name=excluded.name`,
		did, name, time.Now().Format(time.RFC3339))
	return err
}

// updateDeviceColor 手动更新本设备头像底色索引并写回 allowed_devices；
// 采用与改名相同的 upsert：命中已有 did 只更新 color、保留原名称与授权时间，
// 万一该设备尚未登记（如未走门禁解锁）则补登记一行，确保手动选色一定能持久化
func (s *chatStore) updateDeviceColor(did, name string, color int) error {
	ctx, cancel := context.WithTimeout(context.Background(), chatDBTimeout)
	defer cancel()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO allowed_devices(did, name, authorized_at, color) VALUES(?, ?, ?, ?)
		 ON CONFLICT(did) DO UPDATE SET color=excluded.color`,
		did, name, time.Now().Format(time.RFC3339), color)
	return err
}

func (s *chatStore) Close() error { return s.db.Close() }
