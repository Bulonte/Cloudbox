package storage

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// 目录/文件权限（规范 G-12 禁魔法数字）
const (
	DefaultDirMode  = 0o755
	DefaultFileMode = 0o644
	// copyBufferSize 流式拷贝缓冲 32KB
	copyBufferSize = 32 * 1024
)

// LocalStorage 本地内容寻址实现：relPath = hash[:2]/hash（前 2 位分桶）
type LocalStorage struct {
	root string
}

// NewLocalStorage 新建本地存储；root 不存在自动建，是文件则报错
func NewLocalStorage(root string) (*LocalStorage, error) {
	clean := filepath.Clean(root)
	if fi, err := os.Stat(clean); err == nil {
		if !fi.IsDir() {
			return nil, fmt.Errorf("存储根不是目录: %s", clean)
		}
	} else if os.IsNotExist(err) {
		if err := os.MkdirAll(clean, DefaultDirMode); err != nil {
			return nil, fmt.Errorf("创建存储根: %w", err)
		}
	} else {
		return nil, fmt.Errorf("检查存储根: %w", err)
	}
	abs, err := filepath.Abs(clean)
	if err != nil {
		return nil, fmt.Errorf("解析存储根: %w", err)
	}
	return &LocalStorage{root: abs}, nil
}

// validHash SHA256 十六进制 64 位校验
func validHash(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	for _, c := range hash {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// relFor hash 定址：前 2 位分桶防单目录爆炸
func relFor(hash string) string {
	lower := strings.ToLower(hash)
	return filepath.Join(lower[:2], lower)
}

// resolve 相对路径锚定：先拦 .. / 绝对路径，再 Join + 前缀双保险（规范 §5.3.2）
func (s *LocalStorage) resolve(relPath string) (string, error) {
	if relPath == "" || filepath.IsAbs(relPath) {
		return "", ErrPathTraversal
	}
	cleaned := filepath.Clean(relPath)
	slashed := filepath.ToSlash(cleaned)
	if cleaned == "." || slashed == ".." || strings.HasPrefix(slashed, "../") || strings.HasPrefix(slashed, "/") || filepath.IsAbs(cleaned) {
		return "", ErrPathTraversal
	}
	abs := filepath.Join(s.root, cleaned)
	if abs != s.root && !strings.HasPrefix(abs, s.root+string(filepath.Separator)) {
		return "", ErrPathTraversal
	}
	return abs, nil
}

// Store 流式落盘：tmp 写完 Rename 原子生效；O_EXCL 抢建败者复用（并发同 hash 幂等）
func (s *LocalStorage) Store(hash string, src io.Reader) (string, error) {
	if !validHash(hash) {
		return "", ErrInvalidHash
	}
	rel := relFor(hash)
	abs, err := s.resolve(rel)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err == nil {
		// 已存在即复用（W5 秒传/被动秒传钩子）
		return rel, nil
	}
	if err := os.MkdirAll(filepath.Dir(abs), DefaultDirMode); err != nil {
		return "", fmt.Errorf("建分桶目录: %w", err)
	}
	var rnd [8]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", fmt.Errorf("随机后缀: %w", err)
	}
	tmp := fmt.Sprintf("%s.tmp-%s", abs, hex.EncodeToString(rnd[:]))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, DefaultFileMode)
	if err != nil {
		// 并发抢建/复用竞争：目标已出现即复用
		if _, statErr := os.Stat(abs); statErr == nil {
			return rel, nil
		}
		return "", fmt.Errorf("建临时文件: %w", err)
	}
	buf := make([]byte, copyBufferSize)
	if _, err := io.CopyBuffer(f, src, buf); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return "", fmt.Errorf("写入存储: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("关闭存储文件: %w", err)
	}
	if err := os.Rename(tmp, abs); err != nil {
		// 并发下对方先 Rename 成功：复用现成
		if _, statErr := os.Stat(abs); statErr == nil {
			_ = os.Remove(tmp)
			return rel, nil
		}
		_ = os.Remove(tmp)
		return "", fmt.Errorf("生效存储文件: %w", err)
	}
	return rel, nil
}

// Open 流式读：调用方负责 Close（defer 关，规范 §2.5.2）
func (s *LocalStorage) Open(relPath string) (io.ReadCloser, error) {
	abs, err := s.resolve(relPath)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("打开存储文件: %w", err)
	}
	return f, nil
}

// Stat 查大小
func (s *LocalStorage) Stat(relPath string) (int64, error) {
	abs, err := s.resolve(relPath)
	if err != nil {
		return 0, err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("查看存储文件: %w", err)
	}
	return fi.Size(), nil
}

// Exists 秒传预检
func (s *LocalStorage) Exists(hash string) (string, bool) {
	if !validHash(hash) {
		return "", false
	}
	rel := relFor(hash)
	abs, err := s.resolve(rel)
	if err != nil {
		return "", false
	}
	if _, err := os.Stat(abs); err != nil {
		return "", false
	}
	return rel, true
}

// Delete 删对象：不存在视为成功（幂等，孤儿清理好调）
func (s *LocalStorage) Delete(relPath string) error {
	abs, err := s.resolve(relPath)
	if err != nil {
		return err
	}
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除存储文件: %w", err)
	}
	return nil
}
