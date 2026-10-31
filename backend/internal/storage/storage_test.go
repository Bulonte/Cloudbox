package storage

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"sync"
	"testing"
)

func newLocal(t *testing.T) *LocalStorage {
	t.Helper()
	s, err := NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatalf("新建存储失败: %v", err)
	}
	return s
}

func randBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("随机失败: %v", err)
	}
	return b
}

func shaHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestStoreOpenStat(t *testing.T) {
	s := newLocal(t)
	data := randBytes(t, 1<<20) // 1MB
	hash := shaHex(data)

	rel, err := s.Store(hash, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Store 失败: %v", err)
	}
	if len(rel) == 0 {
		t.Fatal("relPath 为空")
	}

	size, err := s.Stat(rel)
	if err != nil || size != int64(len(data)) {
		t.Fatalf("Stat 不对: size=%d err=%v", size, err)
	}

	rc, err := s.Open(rel)
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	defer func() { _ = rc.Close() }()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("读失败: %v", err)
	}
	if shaHex(got) != hash {
		t.Fatal("读回内容 SHA256 不一致")
	}

	// Exists 预检命中
	if p, ok := s.Exists(hash); !ok || p != rel {
		t.Fatalf("Exists 应命中: %q %v", p, ok)
	}
	if _, ok := s.Exists(shaHex([]byte("nope"))); ok {
		t.Fatal("不存在的 hash 不应命中")
	}
}

func TestStoreIdempotent(t *testing.T) {
	s := newLocal(t)
	data := randBytes(t, 4096)
	hash := shaHex(data)

	r1, err := s.Store(hash, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("首次 Store 失败: %v", err)
	}
	r2, err := s.Store(hash, bytes.NewReader(data))
	if err != nil || r2 != r1 {
		t.Fatalf("二次 Store 应复用同路径: %q %q err=%v", r1, r2, err)
	}
	if err := s.Delete(r1); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
	if _, err := s.Open(r1); err == nil {
		t.Fatal("删后 Open 应失败")
	}
	// 二次删幂等成功
	if err := s.Delete(r1); err != nil {
		t.Fatalf("二次删应幂等成功: %v", err)
	}
}

// TestPathTraversal 穿越拦截：Open/Delete/Stat 全入口 + 非法 hash
func TestPathTraversal(t *testing.T) {
	s := newLocal(t)
	for _, p := range []string{"../../etc/passwd", "..", "/abs/path", "a/../../b", ""} {
		if _, err := s.Open(p); err == nil {
			t.Fatalf("Open(%q) 应拦截", p)
		}
		if err := s.Delete(p); err == nil {
			t.Fatalf("Delete(%q) 应拦截", p)
		}
		if _, err := s.Stat(p); err == nil {
			t.Fatalf("Stat(%q) 应拦截", p)
		}
	}
	for _, h := range []string{"", "abc", "zzzz" + string(make([]byte, 60))} {
		if _, err := s.Store(h, bytes.NewReader([]byte("x"))); err == nil {
			t.Fatalf("Store 非法 hash %q 应失败", h)
		}
	}
	if _, ok := s.Exists("short"); ok {
		t.Fatal("非法 hash Exists 应 false")
	}
	// 绝对路径构造必须被 resolve 拦住
	if _, err := s.Open("/etc/hosts"); err == nil {
		t.Fatal("绝对路径应拦截")
	}
}

func TestStoreConcurrent(t *testing.T) {
	s := newLocal(t)
	data := randBytes(t, 64<<10)
	hash := shaHex(data)

	const n = 20
	rels := make([]string, n)
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rel, err := s.Store(hash, bytes.NewReader(data))
			rels[i], errs[i] = rel, err
		}(i)
	}
	wg.Wait()
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("并发 Store %d 失败: %v", i, errs[i])
		}
		if rels[i] != rels[0] {
			t.Fatalf("并发应同路径: %q vs %q", rels[i], rels[0])
		}
	}
}

func createFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, DefaultFileMode)
}

func TestNewLocalStorageRoot(t *testing.T) {
	s := newLocal(t)
	if s == nil {
		t.Fatal("不应为 nil")
	}
	// root 是已存在文件应报错
	dir := t.TempDir()
	blocker := dir + "/blocker"
	f, err := createFile(blocker)
	if err != nil {
		t.Fatalf("建占位文件失败: %v", err)
	}
	_ = f.Close()
	if _, err := NewLocalStorage(blocker); err == nil {
		t.Fatal("root 是文件时应报错")
	}
}
