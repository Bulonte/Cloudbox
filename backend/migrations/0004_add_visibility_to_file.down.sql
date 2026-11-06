-- CloudBox 0004 回滚：删可见性两列两索引
-- 执行：mysql -h127.0.0.1 -P3307 -ucloudbox -p cloudbox < 0004_add_visibility_to_file.down.sql
ALTER TABLE `file` DROP KEY `idx_dept_vis`, DROP KEY `idx_visibility`,
    DROP COLUMN `department_id`, DROP COLUMN `visibility`;
