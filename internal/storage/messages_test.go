package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestChatStorePersistenceAndRetention(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "nested", "chat.db")
	store, err := Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	for i := 0; i < 205; i++ {
		m, limit, err := store.AppendMessage(Message{ClientID: "device", Name: "测试设备", IP: "127.0.0.1", Time: "2026-01-01T00:00:00Z", Text: "保留记录"})
		if err != nil || m.ID != int64(i+1) || limit != 200 {
			t.Fatalf("写入 %d: %+v, %d, %v", i, m, limit, err)
		}
	}
	history, limit, err := store.Snapshot()
	if err != nil || limit != 200 || len(history) != 200 || history[0].ID != 6 {
		t.Fatalf("默认裁剪失败: 数量=%d 上限=%d err=%v", len(history), limit, err)
	}
	oldest, err := store.SetLimit(2)
	if err != nil || oldest != 204 {
		t.Fatalf("减少上限: %d, %v", oldest, err)
	}
	for _, n := range []int{0, -1, 10001} {
		if _, err := store.SetLimit(n); err == nil {
			t.Fatalf("接受了非法上限 %d", n)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	history, limit, err = store.Snapshot()
	if err != nil || limit != 2 || len(history) != 2 || history[0].Name != "测试设备" {
		t.Fatalf("重新打开未恢复: %+v, %d, %v", history, limit, err)
	}
	if _, err = store.SetLimit(500); err != nil {
		t.Fatal(err)
	}
	history, _, err = store.Snapshot()
	if err != nil || len(history) != 2 {
		t.Fatal("扩大上限不应恢复已删除记录")
	}
}

func TestChatStoreInitializationFailures(t *testing.T) {
	root := t.TempDir()
	corrupt := filepath.Join(root, "corrupt.db")
	if err := os.WriteFile(corrupt, []byte("不是 SQLite 数据库"), 0o600); err != nil {
		t.Fatal(err)
	}
	if store, err := Open(corrupt); err == nil {
		store.Close()
		t.Fatal("损坏的数据库不应被覆盖或降级")
	}
	data, err := os.ReadFile(corrupt)
	if err != nil || string(data) != "不是 SQLite 数据库" {
		t.Fatal("损坏文件已被修改")
	}
	if store, err := Open(filepath.Join(corrupt, "chat.db")); err == nil {
		store.Close()
		t.Fatal("父路径不可创建时应失败")
	}
	if store, err := Open(root); err == nil {
		store.Close()
		t.Fatal("数据库路径为目录时应失败")
	}
}

func TestChatStoreBusyWaitIsBounded(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "chat.db")
	store, err := Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	locker, err := Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Close()
	if _, err := locker.db.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	defer locker.db.Exec("ROLLBACK")
	started := time.Now()
	if _, _, err := store.AppendMessage(Message{Text: "写锁被占用"}); err == nil {
		t.Fatal("数据库被锁定时不应提交消息")
	}
	if elapsed := time.Since(started); elapsed > dbTimeout+time.Second {
		t.Fatalf("等待数据库锁超过上限: %v", elapsed)
	}
}

func TestChatStoreFailedTransactionsDoNotLoseHistory(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.SetLimit(2); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, _, err := store.AppendMessage(Message{Text: "需要保留的记录"}); err != nil {
			t.Fatal(err)
		}
	}
	// 人为制造事务后半段失败，验证前面的写入和设置修改一并回滚。
	if _, err := store.db.Exec(`CREATE TRIGGER reject_prune BEFORE DELETE ON messages BEGIN SELECT RAISE(FAIL, '模拟磁盘写入失败'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetLimit(1); err == nil {
		t.Fatal("裁剪失败不应提交新上限")
	}
	if _, _, err := store.AppendMessage(Message{Text: "不能提交的消息"}); err == nil {
		t.Fatal("裁剪失败不应提交新消息")
	}
	history, limit, err := store.Snapshot()
	if err != nil || len(history) != 2 || limit != 2 || history[1].ID != 2 {
		t.Fatalf("失败事务损坏了历史或设置: %+v %d %v", history, limit, err)
	}
	if _, err := store.db.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AppendMessage(Message{Text: "只读错误"}); err == nil {
		t.Fatal("数据库不可写时应明确失败")
	}
}
