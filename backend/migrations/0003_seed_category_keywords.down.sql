-- 0003 回滚：只删 type='category' 种子行（不动 synonym 预留与用户后加行之外的类型）
DELETE FROM `category_keyword` WHERE `type` = 'category';
