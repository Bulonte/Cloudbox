package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"cloudbox/internal/model"
	"cloudbox/internal/repository"
)

// ErrFileNotFound 文件不存在（含越权/已删统一伪装），复用仓储层哨兵
var ErrFileNotFound = repository.ErrFileNotFound

// ErrInvalidParent 父目录非法，复用仓储层哨兵
var ErrInvalidParent = repository.ErrInvalidParent

// ErrFileConflict 同名文件已存在，复用仓储层哨兵
var ErrFileConflict = repository.ErrFileConflict

// FileService 文件业务：薄层，归属判定留在仓储 SQL，只做参数规则
type FileService struct {
	files *repository.FileRepository
}

// NewFileService 构造注入文件仓储
func NewFileService(files *repository.FileRepository) *FileService {
	return &FileService{files: files}
}

// validFileName 文件名规则：1~255 字符，禁 / \，trim 后非空
func validFileName(name string) bool {
	n := strings.TrimSpace(name)
	if n == "" || len(name) > 255 {
		return false
	}
	return !strings.ContainsAny(n, "/\\\x00")
}

// folderHashPlaceholder 文件夹占位 hash：uk_hash 非空约束，随机串保证唯一（S6 真文件永不叫此名前缀）
func folderHashPlaceholder() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("folder-%s", hex.EncodeToString(b[:]))
	}
	return fmt.Sprintf("folder-%s", hex.EncodeToString(b[:]))
}

// CreateFolder 建文件夹：file_hash 用唯一占位串（uk_hash 非空约束，S6 真文件永不叫此名）
func (s *FileService) CreateFolder(ownerID uint64, name string, parentID uint64) (*model.File, error) {
	if !validFileName(name) {
		return nil, ErrInvalidParams
	}
	f := &model.File{
		FileName: strings.TrimSpace(name),
		FileHash: folderHashPlaceholder(),
		ParentID: parentID,
		OwnerID:  ownerID,
		IsFolder: 1,
	}
	if parentID != 0 {
		parent, err := s.files.FindByID(parentID, ownerID)
		if err != nil {
			return nil, err
		}
		if parent.IsFolder != 1 {
			return nil, ErrInvalidParent
		}
	}
	if err := s.files.CreateFile(f); err != nil {
		return nil, err
	}
	return f, nil
}

// Rename 重命名
func (s *FileService) Rename(ownerID, id uint64, name string) (*model.File, error) {
	if !validFileName(name) {
		return nil, ErrInvalidParams
	}
	if err := s.files.Rename(id, ownerID, strings.TrimSpace(name)); err != nil {
		return nil, err
	}
	return s.files.FindByID(id, ownerID)
}

// Move 移动（仓储内校验目标夹同人+文件夹）
func (s *FileService) Move(ownerID, id, parentID uint64) (*model.File, error) {
	// 禁止把文件夹搬进自己或子孙（防环）：查出自身 parent 链，命中即拒
	if err := s.rejectCycle(ownerID, id, parentID); err != nil {
		return nil, err
	}
	if err := s.files.Move(id, ownerID, parentID); err != nil {
		return nil, err
	}
	return s.files.FindByID(id, ownerID)
}

// rejectCycle 目标 parent 不得是自己或自己的子孙
func (s *FileService) rejectCycle(ownerID, id, parentID uint64) error {
	for p := parentID; p != 0; {
		if p == id {
			return ErrInvalidParent
		}
		parent, err := s.files.FindByID(p, ownerID)
		if err != nil {
			// 父链中断交给 Move 仓储判（统一 ErrInvalidParent/NotFound 口径）
			if errors.Is(err, ErrFileNotFound) {
				return ErrInvalidParent
			}
			return err
		}
		p = parent.ParentID
	}
	return nil
}

// Delete 软删（不释放配额，W9 回收站彻底删除时释放）
func (s *FileService) Delete(ownerID, id uint64) error {
	return s.files.SoftDelete(id, ownerID)
}

// List 分页列表：归一化分页后透传仓储（SQL 已带归属隔离，他人 parent 天然 total=0）
func (s *FileService) List(ownerID, parentID uint64, page, pageSize int) ([]*model.File, int64, int, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 1000 {
		pageSize = 1000
	}
	offset := (page - 1) * pageSize
	files, total, err := s.files.ListByParent(ownerID, parentID, offset, pageSize)
	if err != nil {
		return nil, 0, page, pageSize, err
	}
	return files, total, page, pageSize, nil
}
