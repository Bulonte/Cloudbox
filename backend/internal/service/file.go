package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"cloudbox/internal/model"
	"cloudbox/internal/repository"
	"cloudbox/internal/service/permission"
	"cloudbox/internal/storage"
)

// ErrFileNotFound 文件不存在（含越权/已删统一伪装），复用仓储层哨兵
var ErrFileNotFound = repository.ErrFileNotFound

// ErrInvalidParent 父目录非法，复用仓储层哨兵
var ErrInvalidParent = repository.ErrInvalidParent

// ErrFileConflict 同名文件已存在，复用仓储层哨兵
var ErrFileConflict = repository.ErrFileConflict

// ErrNotDownloadable 文件夹/不可下载类型
var ErrNotDownloadable = errors.New("文件夹不支持下载")

// FileService 文件业务：薄层，归属判定留在仓储 SQL，只做参数规则
// S3：细粒度读经 permission.Checker 集中判（handler 禁自判）；写/删仍收口到 Checker，拒绝一律 ErrFileNotFound 伪装
// S4：审计旁路 WithAuditor（nil=不记，仅单测兼容；download 明细只记 file_id/file_name 快照）
type FileService struct {
	files   *repository.FileRepository
	store   storage.Storage
	checker *permission.Checker
	auditor *AuditService
}

// NewFileService 构造注入文件仓储
func NewFileService(files *repository.FileRepository) *FileService {
	return &FileService{files: files}
}

// WithChecker 注入集中判定器（S3 文件级权限地基；nil=老归属口径，仅单测兼容）
func (s *FileService) WithChecker(c *permission.Checker) *FileService {
	s.checker = c
	return s
}

// WithAuditor 注入审计旁路（S4；nil=不记，仅单测兼容）
func (s *FileService) WithAuditor(a *AuditService) *FileService {
	s.auditor = a
	return s
}

// auditRecord 审计旁路统一入口：nil 审计直接跳过；Record 内部已 fail-open，调用方禁判错
func (s *FileService) auditRecord(in AuditInput) {
	if s == nil || s.auditor == nil {
		return
	}
	_ = s.auditor.Record(in)
}

// loadForRead 读加载：owner 快路径 → 404 时若有 Checker 则经 FindByIDAny + Can(Read) 放行公共/同部门
// 拒绝一律 ErrFileNotFound 伪装（防枚举；粗粒度 AdminOnly 另走 403/20003）
func (s *FileService) loadForRead(userID uint64, role, dept string, id uint64) (*model.File, error) {
	if f, err := s.files.FindByID(id, userID); err == nil {
		return f, nil
	} else if !errors.Is(err, ErrFileNotFound) {
		return nil, err
	}
	if s.checker == nil {
		return nil, ErrFileNotFound
	}
	f, err := s.files.FindByIDAny(id)
	if err != nil {
		return nil, err
	}
	if !s.checker.Can(userID, role, dept, f, permission.ActionRead) {
		return nil, ErrFileNotFound
	}
	return f, nil
}

// loadForWrite 写加载：经 FindByIDAny + Can(act) 集中判；拒绝一律 ErrFileNotFound 伪装
// owner 快路径等价于 Can 通过（Checker 内所有者放行），统一走 Any 保持单收口
func (s *FileService) loadForWrite(userID uint64, role, dept string, id uint64, act permission.Action) (*model.File, error) {
	if s.checker == nil {
		return s.files.FindByID(id, userID)
	}
	f, err := s.files.FindByIDAny(id)
	if err != nil {
		return nil, err
	}
	if !s.checker.Can(userID, role, dept, f, act) {
		return nil, ErrFileNotFound
	}
	return f, nil
}

// WithStorage 注入存储适配（S7 流式下载用），返回同一实例方便链式调用
func (s *FileService) WithStorage(st storage.Storage) *FileService {
	s.store = st
	return s
}

// validFileName 文件名规则：1~255 字符（按 rune 计，utf8mb4 VARCHAR(255) 语义），禁 / \，trim 后非空
func validFileName(name string) bool {
	n := strings.TrimSpace(name)
	if n == "" || utf8.RuneCountInString(n) > 255 {
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

// Rename 重命名（老口径：owner-only；S3 新链路请用 RenameChecked 传 role/dept）
func (s *FileService) Rename(ownerID, id uint64, name string) (*model.File, error) {
	return s.RenameChecked(ownerID, "", "", id, name)
}

// RenameChecked 重命名（S3 集中判：write；拒绝一律 ErrFileNotFound 伪装）
func (s *FileService) RenameChecked(userID uint64, role, dept string, id uint64, name string) (*model.File, error) {
	if !validFileName(name) {
		return nil, ErrInvalidParams
	}
	if _, err := s.loadForWrite(userID, role, dept, id, permission.ActionWrite); err != nil {
		return nil, err
	}
	// 仓储层仍带 owner_id 做二次收口：非 owner 走 Any 判过后需按行 Owner 更新，避免越权改名
	f, err := s.files.FindByIDAny(id)
	if err != nil {
		return nil, err
	}
	if err := s.files.Rename(f.ID, f.OwnerID, strings.TrimSpace(name)); err != nil {
		// 仓储按 owner 判 0 行转 404（与旧口径一致的伪装）
		if errors.Is(err, ErrFileNotFound) {
			return nil, ErrFileNotFound
		}
		return nil, err
	}
	return s.files.FindByIDAny(id)
}

// Move 移动（老口径：owner-only；S3 新链路请用 MoveChecked 传 role/dept）
func (s *FileService) Move(ownerID, id, parentID uint64) (*model.File, error) {
	return s.MoveChecked(ownerID, "", "", id, parentID)
}

// MoveChecked 移动（S3 集中判：write；拒绝一律 ErrFileNotFound 伪装）
func (s *FileService) MoveChecked(userID uint64, role, dept string, id, parentID uint64) (*model.File, error) {
	f, err := s.loadForWrite(userID, role, dept, id, permission.ActionWrite)
	if err != nil {
		return nil, err
	}
	if err := s.rejectCycleChecked(f.OwnerID, id, parentID); err != nil {
		return nil, err
	}
	if err := s.files.Move(f.ID, f.OwnerID, parentID); err != nil {
		if errors.Is(err, ErrFileNotFound) {
			return nil, ErrFileNotFound
		}
		return nil, err
	}
	return s.files.FindByIDAny(id)
}

// rejectCycle 目标 parent 不得是自己或自己的子孙（老口径：owner 视角）
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

// rejectCycleChecked 防环（S3 集中判视角：按文件属主 Owner 校验父链归属）
func (s *FileService) rejectCycleChecked(fileOwnerID, id, parentID uint64) error {
	for p := parentID; p != 0; {
		if p == id {
			return ErrInvalidParent
		}
		parent, err := s.files.FindByIDAny(p)
		if err != nil {
			if errors.Is(err, ErrFileNotFound) {
				return ErrInvalidParent
			}
			return err
		}
		if parent.OwnerID != fileOwnerID {
			return ErrInvalidParent
		}
		p = parent.ParentID
	}
	return nil
}

// Delete 软删（老口径：owner-only；S3 新链路请用 DeleteChecked 传 role/dept）
func (s *FileService) Delete(ownerID, id uint64) error {
	return s.DeleteChecked(ownerID, "", "", id)
}

// DeleteChecked 软删（S3 集中判：manage；拒绝一律 ErrFileNotFound 伪装）
func (s *FileService) DeleteChecked(userID uint64, role, dept string, id uint64) error {
	f, err := s.loadForWrite(userID, role, dept, id, permission.ActionManage)
	if err != nil {
		return err
	}
	return s.files.SoftDelete(f.ID, f.OwnerID)
}

// Download 下载编排（老口径：owner-only；S3 新链路请用 DownloadChecked 传 role/dept）
func (s *FileService) Download(ownerID, id uint64) (*model.File, io.ReadCloser, int64, error) {
	return s.DownloadChecked(ownerID, "", "", id)
}

// DownloadChecked 下载编排（S3 集中判：read；归属 → Stat 取真值 → Open 取流；调用方负责 Close）
// S4：成功后审计 download（C方案：target=file/ID，detail={file_id,file_name}；旁路失败不阻断；IP 经可变参透传，缺省记 NULL）
func (s *FileService) DownloadChecked(userID uint64, role, dept string, id uint64, ips ...string) (*model.File, io.ReadCloser, int64, error) {
	if userID == 0 || id == 0 {
		return nil, nil, 0, ErrInvalidParams
	}
	if s.store == nil {
		return nil, nil, 0, fmt.Errorf("存储未注入")
	}
	f, err := s.loadForRead(userID, role, dept, id)
	if err != nil {
		return nil, nil, 0, err
	}
	if err := checkDownloadable(f); err != nil {
		return nil, nil, 0, err
	}
	size, err := s.store.Stat(f.FilePath)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("读取存储大小: %w", err)
	}
	rc, err := s.store.Open(f.FilePath)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("打开存储对象: %w", err)
	}
	ip := ""
	if len(ips) > 0 {
		ip = ips[0]
	}
	s.auditRecord(AuditInput{
		UserID:     userID,
		Action:     repository.ActionDownload,
		TargetType: "file",
		TargetID:   f.ID,
		IP:         ip,
		Detail: map[string]any{
			"file_id":   f.ID,
			"file_name": f.FileName,
		},
	})
	return f, rc, size, nil
}

// checkDownloadable 文件夹不可下载
func checkDownloadable(f *model.File) error {
	if f.IsFolder == 1 {
		return ErrNotDownloadable
	}
	return nil
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
