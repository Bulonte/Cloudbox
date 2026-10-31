-- CloudBox 0002 回滚：删 mime_type 列
-- 执行：mysql -h127.0.0.1 -P3307 -ucloudbox -p cloudbox < 0002_add_mime_to_file.down.sql
ALTER TABLE `file` DROP COLUMN `mime_type`;
