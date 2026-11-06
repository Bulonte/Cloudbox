package permission

import (
	"database/sql"
	"testing"

	"cloudbox/internal/model"
)

func fileOf(owner uint64, vis, dept string, deleted int8) *model.File {
	f := &model.File{ID: 1, OwnerID: owner, Visibility: vis, IsDeleted: deleted}
	if dept != "" {
		f.DepartmentID = sql.NullString{String: dept, Valid: true}
	}
	return f
}

func TestImplies(t *testing.T) {
	if !ActionManage.Implies(ActionRead) || !ActionManage.Implies(ActionWrite) || !ActionManage.Implies(ActionManage) {
		t.Fatal("manage 应包含一切")
	}
	if !ActionWrite.Implies(ActionRead) || ActionWrite.Implies(ActionManage) {
		t.Fatal("write 仅含 read")
	}
	if !ActionRead.Implies(ActionRead) || ActionRead.Implies(ActionWrite) {
		t.Fatal("read 仅自身")
	}
}

func TestCanMatrix(t *testing.T) {
	c := NewChecker()
	ownerID := uint64(1)
	otherID := uint64(2)

	cases := []struct {
		name string
		uid  uint64
		role string
		dept string
		f    *model.File
		act  Action
		want bool
	}{
		{"nil拒绝", otherID, "user", "rd", nil, ActionRead, false},
		{"已删超管也拒", otherID, model.RoleSuperAdmin, "rd", fileOf(ownerID, VisibilityPrivate, "", 1), ActionRead, false},
		{"已删所有者也拒", ownerID, "user", "rd", fileOf(ownerID, VisibilityPublic, "", 1), ActionRead, false},
		{"超管读私密放行", otherID, model.RoleSuperAdmin, "", fileOf(ownerID, VisibilityPrivate, "", 0), ActionRead, true},
		{"超管写私密放行", otherID, model.RoleSuperAdmin, "", fileOf(ownerID, VisibilityPrivate, "", 0), ActionWrite, true},
		{"超管管私密放行", otherID, model.RoleSuperAdmin, "", fileOf(ownerID, VisibilityPrivate, "", 0), ActionManage, true},
		{"所有者读写管全放", ownerID, "user", "", fileOf(ownerID, VisibilityPrivate, "", 0), ActionManage, true},
		{"所有者读部门文件放行", ownerID, "user", "other", fileOf(ownerID, VisibilityDepartment, "rd", 0), ActionRead, true},
		{"私密异人读拒", otherID, "user", "rd", fileOf(ownerID, VisibilityPrivate, "", 0), ActionRead, false},
		{"私密异人写拒", otherID, "user", "rd", fileOf(ownerID, VisibilityPrivate, "", 0), ActionWrite, false},
		{"私密异人管拒", otherID, "user", "rd", fileOf(ownerID, VisibilityPrivate, "", 0), ActionManage, false},
		{"部门同部门读放", otherID, "user", "rd", fileOf(ownerID, VisibilityDepartment, "rd", 0), ActionRead, true},
		{"部门同部门写拒", otherID, "user", "rd", fileOf(ownerID, VisibilityDepartment, "rd", 0), ActionWrite, false},
		{"部门同部门管拒", otherID, "user", "rd", fileOf(ownerID, VisibilityDepartment, "rd", 0), ActionManage, false},
		{"部门异部门读拒", uint64(3), "user", "other", fileOf(ownerID, VisibilityDepartment, "rd", 0), ActionRead, false},
		{"部门空部门读拒", otherID, "user", "", fileOf(ownerID, VisibilityDepartment, "rd", 0), ActionRead, false},
		{"部门空dept列读拒", otherID, "user", "rd", fileOf(ownerID, VisibilityDepartment, "", 0), ActionRead, false},
		{"公开异人读放", otherID, "user", "", fileOf(ownerID, VisibilityPublic, "", 0), ActionRead, true},
		{"公开异人写拒", otherID, "user", "", fileOf(ownerID, VisibilityPublic, "", 0), ActionWrite, false},
		{"公开异人管拒", otherID, "user", "", fileOf(ownerID, VisibilityPublic, "", 0), ActionManage, false},
		{"非法visibility按private拒", otherID, "user", "rd", fileOf(ownerID, "weird", "rd", 0), ActionRead, false},
		{"空visibility按private拒", otherID, "user", "rd", fileOf(ownerID, "", "rd", 0), ActionRead, false},
		{"大小写public放行", otherID, "user", "", fileOf(ownerID, "Public", "", 0), ActionRead, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := c.Can(tc.uid, tc.role, tc.dept, tc.f, tc.act); got != tc.want {
				t.Fatalf("Can=%v, 期望 %v (uid=%d role=%s dept=%q act=%s vis=%v)", got, tc.want, tc.uid, tc.role, tc.dept, tc.act, tc.f)
			}
		})
	}

	var nilChecker *Checker
	if nilChecker.Can(otherID, "user", "rd", fileOf(ownerID, VisibilityPublic, "", 0), ActionRead) {
		t.Fatal("nil Checker 应拒绝")
	}
}
