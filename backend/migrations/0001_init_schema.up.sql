-- CloudBox 0001 初始化：7 张核心表
-- 规范：COMMENT 必备 / InnoDB+utf8mb4 / BIGINT UNSIGNED 主键 / DATETIME 时间 / TINYINT 布尔
-- 执行：mysql -h127.0.0.1 -P3307 -ucloudbox -p cloudbox < 0001_init_schema.up.sql
-- 回滚：见 0001_init_schema.down.sql

-- 1. 用户表（认证信息、角色、配额）
CREATE TABLE `user` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    `username` VARCHAR(64) NOT NULL COMMENT '用户名，登录唯一标识',
    `email` VARCHAR(128) NOT NULL COMMENT '邮箱，登录/找回唯一标识',
    `password_hash` VARCHAR(255) NOT NULL COMMENT 'bcrypt哈希，cost=12',
    `role` VARCHAR(16) NOT NULL DEFAULT 'user' COMMENT '角色:user=普通用户 admin=管理员',
    `quota_bytes` BIGINT UNSIGNED NOT NULL DEFAULT 10737418240 COMMENT '配额字节数，默认10GB',
    `used_bytes` BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '已用字节数',
    `status` TINYINT NOT NULL DEFAULT 1 COMMENT '状态:1=正常 0=禁用',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_username` (`username`),
    UNIQUE KEY `uk_email` (`email`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户表';

-- 2. 文件表（元数据、分类、指纹引用）
CREATE TABLE `file` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    `file_name` VARCHAR(255) NOT NULL COMMENT '文件名',
    `file_hash` CHAR(64) NOT NULL COMMENT 'SHA256哈希值，用于秒传去重',
    `file_size` BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '文件大小(字节)',
    `file_path` VARCHAR(500) NOT NULL COMMENT '存储路径（内容寻址，不含用户输入）',
    `parent_id` BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '父文件夹ID，0为根目录',
    `owner_id` BIGINT UNSIGNED NOT NULL COMMENT '所有者ID',
    `is_folder` TINYINT NOT NULL DEFAULT 0 COMMENT '是否为文件夹:0=文件 1=文件夹',
    `is_deleted` TINYINT NOT NULL DEFAULT 0 COMMENT '软删除标记:0=正常 1=已删除',
    `category` VARCHAR(32) DEFAULT NULL COMMENT '智能分类结果，9大类之一',
    `confidence` DECIMAL(4,3) DEFAULT NULL COMMENT '分类置信度[0,1]，低于0.7标待确认',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_hash` (`file_hash`),
    KEY `idx_owner_deleted` (`owner_id`, `is_deleted`),
    KEY `idx_parent_deleted` (`parent_id`, `is_deleted`),
    KEY `idx_category` (`category`),
    FULLTEXT KEY `ft_name` (`file_name`),
    CONSTRAINT `fk_file_owner` FOREIGN KEY (`owner_id`) REFERENCES `user` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='文件表';

-- 3. 分享表（分享码、密码、有效期）
CREATE TABLE `file_share` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    `share_code` CHAR(16) NOT NULL COMMENT '16位分享码，crypto/rand+Base62',
    `file_id` BIGINT UNSIGNED NOT NULL COMMENT '被分享文件ID',
    `owner_id` BIGINT UNSIGNED NOT NULL COMMENT '分享人ID',
    `password_hash` VARCHAR(255) DEFAULT NULL COMMENT '分享密码bcrypt哈希，可空=无密码',
    `expire_at` DATETIME DEFAULT NULL COMMENT '过期时间，可空=永不过期',
    `max_downloads` INT UNSIGNED DEFAULT NULL COMMENT '最大下载次数，可空=不限',
    `download_count` INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '已下载次数',
    `status` TINYINT NOT NULL DEFAULT 1 COMMENT '状态:1=有效 0=取消/失效',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_share_code` (`share_code`),
    KEY `idx_owner` (`owner_id`),
    KEY `idx_file` (`file_id`),
    CONSTRAINT `fk_share_file` FOREIGN KEY (`file_id`) REFERENCES `file` (`id`),
    CONSTRAINT `fk_share_owner` FOREIGN KEY (`owner_id`) REFERENCES `user` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='分享表';

-- 4. 内容指纹表（SimHash + pHash）
CREATE TABLE `file_fingerprint` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    `file_id` BIGINT UNSIGNED NOT NULL COMMENT '关联文件ID',
    `algorithm` VARCHAR(16) NOT NULL COMMENT '指纹算法:simhash/phash',
    `fingerprint` CHAR(16) NOT NULL COMMENT '64位指纹，16位十六进制字符串',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_file_algo` (`file_id`, `algorithm`),
    KEY `idx_algo_fp` (`algorithm`, `fingerprint`),
    CONSTRAINT `fk_fp_file` FOREIGN KEY (`file_id`) REFERENCES `file` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='内容指纹表';

-- 5. 分片上传记录表
CREATE TABLE `upload_chunk` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    `upload_id` CHAR(36) NOT NULL COMMENT '一次上传会话ID',
    `file_hash` CHAR(64) NOT NULL COMMENT '客户端声明的文件SHA256',
    `chunk_index` INT UNSIGNED NOT NULL COMMENT '分片序号，从0开始',
    `total_chunks` INT UNSIGNED NOT NULL COMMENT '分片总数',
    `chunk_size` BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '本分片字节数',
    `chunk_path` VARCHAR(500) NOT NULL COMMENT '分片临时存储路径',
    `owner_id` BIGINT UNSIGNED NOT NULL COMMENT '上传人ID',
    `status` TINYINT NOT NULL DEFAULT 0 COMMENT '状态:0=上传中 1=已完成 2=已取消',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_upload_chunk` (`upload_id`, `chunk_index`),
    KEY `idx_owner_hash` (`owner_id`, `file_hash`),
    CONSTRAINT `fk_chunk_owner` FOREIGN KEY (`owner_id`) REFERENCES `user` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='分片上传记录表';

-- 6. 操作日志表（审计，只增不改，保留180天）
CREATE TABLE `operation_log` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    `user_id` BIGINT UNSIGNED NOT NULL COMMENT '操作人ID',
    `action` VARCHAR(64) NOT NULL COMMENT '动作，如upload/rename/share',
    `target_type` VARCHAR(32) DEFAULT NULL COMMENT '目标类型，如file/share',
    `target_id` BIGINT UNSIGNED DEFAULT NULL COMMENT '目标ID',
    `ip` VARCHAR(64) DEFAULT NULL COMMENT '操作IP',
    `detail` TEXT DEFAULT NULL COMMENT '详情JSON',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    PRIMARY KEY (`id`),
    KEY `idx_user_time` (`user_id`, `created_at`),
    KEY `idx_created` (`created_at`),
    CONSTRAINT `fk_log_user` FOREIGN KEY (`user_id`) REFERENCES `user` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='操作日志表';

-- 7. 分类关键词规则表（W9同义词复用type字段，不建第8张表）
CREATE TABLE `category_keyword` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '主键ID',
    `type` VARCHAR(16) NOT NULL DEFAULT 'category' COMMENT '规则类型:category=分类关键词 synonym=同义词',
    `category` VARCHAR(32) NOT NULL COMMENT '9大类之一：合同/简历/报表/图片/视频/音频/文档/压缩包/其他',
    `keyword` VARCHAR(64) NOT NULL COMMENT '关键词',
    `weight` DECIMAL(5,2) NOT NULL DEFAULT 1.00 COMMENT '权重',
    `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_type_cat_kw` (`type`, `category`, `keyword`),
    KEY `idx_category` (`category`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='分类关键词规则表';
