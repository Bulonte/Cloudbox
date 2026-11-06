package permission

import (
	"strings"

	"cloudbox/internal/model"
)

// Checker 文件级权限集中判定：唯一允许散落 if 的地方之外的收口点。
// 策略（fail-closed，默认拒绝）：
// 1. 空/已删一律拒绝；2. super_admin 放行；3. 所有者放行；
// 4. 读 public 放行；5. 读 department 同部门放行；其余一律拒绝。
// 非法 visibility 按 private 收口；department_id 为空永不匹配。
type Checker struct{}

// NewChecker 构造集中判定器（无状态，main.go 显式注入 File/Share Service）
func NewChecker() *Checker { return &Checker{} }

// Can 集中判定：userID/role/dept 由调用方（Service）传入，handler 禁自判。
func (c *Checker) Can(userID uint64, userRole, userDept string, f *model.File, act Action) bool {
	if c == nil || f == nil || f.IsDeleted == 1 {
		return false
	}
	if userRole == model.RoleSuperAdmin {
		return true
	}
	if userID != 0 && f.OwnerID == userID {
		return true
	}
	if act != ActionRead {
		return false
	}
	vis := strings.TrimSpace(strings.ToLower(f.Visibility))
	switch vis {
	case VisibilityPublic:
		return true
	case VisibilityDepartment:
		dept := strings.TrimSpace(userDept)
		if dept == "" || !f.DepartmentID.Valid {
			return false
		}
		return strings.TrimSpace(f.DepartmentID.String) == dept
	default:
		return false
	}
}
