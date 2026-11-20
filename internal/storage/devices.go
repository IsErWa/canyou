package storage

import (
	"context"
	"database/sql"
	"time"
)

// LoadAllowedDeviceIDs 用短连接加载已登记设备，读取失败时保留已读部分。
func LoadAllowedDeviceIDs() ([]string, error) {
	var ids []string
	err := withDeviceDB(func(db *sql.DB) error {
		rows, err := db.Query("SELECT did FROM allowed_devices")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var did string
			if err := rows.Scan(&did); err != nil {
				return err
			}
			ids = append(ids, did)
		}
		return rows.Err()
	})
	return ids, err
}

// RegisterDevice 仅新增未登记的设备，不覆盖已存在的名称、主题或授权时间。
func RegisterDevice(did, name string, color int) error {
	return withDeviceDB(func(db *sql.DB) error {
		_, err := db.Exec(`INSERT OR IGNORE INTO allowed_devices(did, name, authorized_at, color) VALUES(?, ?, ?, ?)`, did, name, time.Now().Format(time.RFC3339), color)
		return err
	})
}

// DeviceInfo 使用门禁短连接读取设备资料，由业务层决定缺失或非法值的回退。
func DeviceInfo(did string) (int64, string, string, error) {
	var id int64
	var name, theme string
	err := withDeviceDB(func(db *sql.DB) error {
		return db.QueryRow(`SELECT id, name, theme FROM allowed_devices WHERE did=?`, did).Scan(&id, &name, &theme)
	})
	return id, name, theme, err
}

// SetDeviceTheme 只更新既有设备的主题；未命中时保持原来的失败语义。
func SetDeviceTheme(did, theme string) error {
	return withDeviceDB(func(db *sql.DB) error {
		result, err := db.Exec(`UPDATE allowed_devices SET theme=? WHERE did=?`, theme, did)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err == nil && count != 1 {
			return sql.ErrNoRows
		}
		return err
	})
}

// deviceRowID 返回门禁 allowed_devices 中该设备的主键编号，供聊天标注设备序号；未登记或表不存在时返回 0
func (s *Store) deviceRowID(did string) int64 {
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()
	var id int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM allowed_devices WHERE did=?`, did).Scan(&id); err != nil {
		return 0
	}
	return id
}

// DeviceMeta 一次读取设备的主键编号与预设底色；未登记或表不存在时返回 (0, -1)
func (s *Store) DeviceMeta(did string) (int64, int) {
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()
	var id int64
	var color int
	if err := s.db.QueryRowContext(ctx, `SELECT id, color FROM allowed_devices WHERE did=?`, did).Scan(&id, &color); err != nil {
		return 0, -1
	}
	return id, color
}

// DeviceName 读取门禁在 allowed_devices 登记的权威设备名；表不存在或该设备未登记时返回空串
func (s *Store) DeviceName(did string) string {
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()
	var name string
	if err := s.db.QueryRowContext(ctx, `SELECT name FROM allowed_devices WHERE did=?`, did).Scan(&name); err != nil {
		return ""
	}
	return name
}

// UpdateDeviceName 改名时同步门禁名单里的权威设备名：命中已有 did 只更新 name、保留原授权时间；
// 若该设备尚未登记（如未走解锁）则补登记一行。allowed_devices 表由门禁启动时创建，与聊天同库可直接访问
func (s *Store) UpdateDeviceName(did, name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO allowed_devices(did, name, authorized_at) VALUES(?, ?, ?)
		 ON CONFLICT(did) DO UPDATE SET name=excluded.name`,
		did, name, time.Now().Format(time.RFC3339))
	return err
}

// UpdateDeviceColor 手动更新本设备头像底色索引并写回 allowed_devices；
// 采用与改名相同的 upsert：命中已有 did 只更新 color、保留原名称与授权时间，
// 万一该设备尚未登记（如未走门禁解锁）则补登记一行，确保手动选色一定能持久化
func (s *Store) UpdateDeviceColor(did, name string, color int) error {
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO allowed_devices(did, name, authorized_at, color) VALUES(?, ?, ?, ?)
		 ON CONFLICT(did) DO UPDATE SET color=excluded.color`,
		did, name, time.Now().Format(time.RFC3339), color)
	return err
}
