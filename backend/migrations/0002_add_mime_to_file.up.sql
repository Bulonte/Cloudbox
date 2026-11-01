-- CloudBox 0002：file 表补 mime_type（S1 地基，S5 嗅探落盘 + S7 下载重判用）
-- 执行：mysql -h127.0.0.1 -P3307 -ucloudbox -p cloudbox < 0002_add_mime_to_file.up.sql
-- 回滚：见 0002_add_mime_to_file.down.sql（删列，需确认无业务依赖，W4 内可逆）
ALTER TABLE `file`
    ADD COLUMN `mime_type` VARCHAR(64) NOT NULL DEFAULT '' COMMENT '服务端嗅探的MIME，不信客户端' AFTER `file_path`;
