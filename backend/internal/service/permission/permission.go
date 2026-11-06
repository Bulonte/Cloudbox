package permission

// Action 权限动作：manage ⊃ write ⊃ read（包含关系由 Checker 隐含，不散落各处）
type Action string

const (
	ActionRead   Action = "read"
	ActionWrite  Action = "write"
	ActionManage Action = "manage"
)

// Visibility 文件可见性：与 0004 迁移一致；非法值一律按 private 收口（fail-closed）
const (
	VisibilityPrivate    = "private"
	VisibilityDepartment = "department"
	VisibilityPublic     = "public"
)

// Implies 动作包含：manage 可做一切，write 可读，read 仅读
func (a Action) Implies(need Action) bool {
	if a == need {
		return true
	}
	if a == ActionManage {
		return true
	}
	if a == ActionWrite && need == ActionRead {
		return true
	}
	return false
}
