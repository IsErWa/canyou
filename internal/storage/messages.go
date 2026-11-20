package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const (
	defaultChatLimit = 200
	MaxChatLimit     = 10000
	dbTimeout        = 5 * time.Second
)

type Message struct {
	ID       int64  `json:"id"`
	ClientID string `json:"clientId"`
	No       int64  `json:"no"`    // 发送方在 allowed_devices 的主键编号，读取时映射填充，不落库
	Color    int    `json:"color"` // 发送方预设底色索引，读取时映射填充，不落库；-1 表示未设置
	Name     string `json:"name"`
	IP       string `json:"ip"`
	Time     string `json:"time"`
	Text     string `json:"text"`
}

type Store struct{ db *sql.DB }

func (s *Store) initialize() error {
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
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
	if limit < 1 || limit > MaxChatLimit {
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

func (s *Store) Snapshot() ([]Message, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
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
	messages := make([]Message, 0)
	for rows.Next() {
		var m Message
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

// AppendMessage 写入与裁剪在同一事务内，调用方只广播已提交的数据。
func (s *Store) AppendMessage(m Message) (Message, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
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

func (s *Store) SetLimit(limit int) (int64, error) {
	if limit < 1 || limit > MaxChatLimit {
		return 0, errors.New("保留条数必须为 1–10000 的整数")
	}
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
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

func (s *Store) Close() error { return s.db.Close() }
