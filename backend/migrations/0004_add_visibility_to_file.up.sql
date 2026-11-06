-- CloudBox 0004：file 表补可见性（S3 文件级权限地基）
-- 执行：mysql -h127.0.0.1 -P3307 -ucloudbox -p cloudbox < 0004_add_visibility_to_file.up.sql
-- 回滚：见 0004_add_visibility_to_file.down.sql
-- 冻结说明：task_plan 唯一 A 级变更；visibility 默认 private 存量自动回填；department_id 可空
ALTER TABLE `file`
    ADD COLUMN `visibility` VARCHAR(16) NOT NULL DEFAULT 'private' COMMENT '可见性:private=仅所有者 department=同部门 public=经分享链接' AFTER `is_deleted`,
    ADD COLUMN `department_id` VARCHAR(64) DEFAULT NULL COMMENT '部门编码，可空；仅visibility=department时有效' AFTER `visibility`,
    ADD KEY `idx_visibility` (`visibility`),
    ADD KEY `idx_dept_vis` (`department_id`, `visibility`);
