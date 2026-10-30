package repository

import (
	"errors"
	"strings"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// isUniqueViolation 判断是否唯一约束冲突（uk_username / uk_email / uk_hash）
// SQLite 与 MySQL 错误类型不同，这里同时兼容；消息匹配仅作兜底
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// MySQL: Error 1062 Duplicate entry
	var myErr *mysql.MySQLError
	if errors.As(err, &myErr) {
		return myErr.Number == 1062
	}
	// SQLite: sqlite3.Error{Code: 19} ErrConstraintUnique
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") || strings.Contains(msg, "duplicate entry")
}
