package storage

import (
	"errors"
	"io"
)

// ErrInvalidHash hash 非法（非 64 位十六进制 SHA256）
var ErrInvalidHash = errors.New("文件哈希非法")

// ErrNotFound 存储对象不存在
var ErrNotFound = errors.New("存储对象不存在")

// ErrPathTraversal 路径穿越尝试
var ErrPathTraversal = errors.New("非法存储路径")

// Storage 存储适配接口：本地默认，MinIO 可选（选型 C8）；业务方只认相对路径
type Storage interface {
	// Store 内容寻址存：同 hash 存在即复用，返回相对路径
	Store(hash string, src io.Reader) (string, error)
	// Open 流式读：下载直灌 ResponseWriter，禁 ReadAll
	Open(relPath string) (io.ReadCloser, error)
	// Stat 查大小：上传校验用
	Stat(relPath string) (int64, error)
	// Exists 秒传预检：同 hash 是否已存
	Exists(hash string) (string, bool)
	// Delete 删对象：孤儿清理/彻底删除用
	Delete(relPath string) error
}
