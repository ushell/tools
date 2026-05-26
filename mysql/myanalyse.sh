#!/bin/bash

###############################################################################
# MySQL信息收集脚本 - WordPress性能分析
# 用途：收集MySQL运行状态、表结构、慢查询等信息用于性能分析
# 注意：只读操作，不执行任何DDL或修改操作
###############################################################################

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# 配置区域 - 请根据实际情况修改
# MySQL命令路径 - 如果mysql不在PATH中，请设置完整路径
MYSQL_CMD_PATH="${MYSQL_CMD_PATH:-mysql}"

MYSQL_HOST="${MYSQL_HOST:-localhost}"
MYSQL_PORT="${MYSQL_PORT:-3306}"
MYSQL_USER="${MYSQL_USER:-root}"
MYSQL_PASSWORD="${MYSQL_PASSWORD:-}"
MYSQL_DATABASE="${MYSQL_DATABASE:-wordpress}"

# 输出目录
OUTPUT_DIR="./mysql_dump_$(date +%Y%m%d_%H%M%S)"
mkdir -p "$OUTPUT_DIR"

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}MySQL 信息收集脚本${NC}"
echo -e "${GREEN}========================================${NC}"
echo "输出目录: $OUTPUT_DIR"
echo ""

# 检查mysql命令是否存在
if ! command -v "$MYSQL_CMD_PATH" &> /dev/null; then
    echo -e "${RED}错误: 未找到mysql命令 ($MYSQL_CMD_PATH)${NC}"
    echo -e "${YELLOW}请设置MYSQL_CMD_PATH环境变量指向正确的mysql命令路径${NC}"
    echo -e "${YELLOW}例如: export MYSQL_CMD_PATH=/usr/local/mysql/bin/mysql${NC}"
    exit 1
fi

# 构建mysql连接命令
MYSQL_CMD="$MYSQL_CMD_PATH -h $MYSQL_HOST -P $MYSQL_PORT -u $MYSQL_USER"
if [ -n "$MYSQL_PASSWORD" ]; then
    MYSQL_CMD="$MYSQL_CMD -p$MYSQL_PASSWORD"
fi

###############################################################################
# 1. 基本信息
###############################################################################
echo -e "${YELLOW}[1/15] 收集MySQL基本信息...${NC}"
$MYSQL_CMD -e "
SELECT
    VERSION()              AS 'mysql_version',
    NOW()                  AS 'current_time',
    @@hostname             AS 'hostname',
    @@port                 AS 'port',
    @@basedir              AS 'basedir',
    @@datadir              AS 'datadir',
    @@max_connections      AS 'max_connections',
    @@table_open_cache     AS 'table_open_cache',
    @@thread_cache_size    AS 'thread_cache_size',
    (SELECT VARIABLE_VALUE 
     FROM performance_schema.global_variables 
     WHERE VARIABLE_NAME = 'query_cache_size' 
     LIMIT 1)              AS 'query_cache_size',
    @@tmp_table_size       AS 'tmp_table_size',
    @@max_heap_table_size  AS 'max_heap_table_size',
    @@innodb_buffer_pool_size   AS 'innodb_buffer_pool_size',
    @@innodb_log_file_size      AS 'innodb_log_file_size',
    @@innodb_flush_log_at_trx_commit AS 'innodb_flush_log_at_trx_commit',
    @@sync_binlog          AS 'sync_binlog',
    @@slow_query_log       AS 'slow_query_log',
    @@long_query_time      AS 'long_query_time';
" > "$OUTPUT_DIR/01_mysql_basic_info.txt" 2>/dev/null

###############################################################################
# 2. 数据库列表
###############################################################################

echo -e "${YELLOW}[2/15] 收集数据库列表...${NC}"
$MYSQL_CMD -e "SHOW DATABASES;" 2>/dev/null > "$OUTPUT_DIR/02_databases.txt"

###############################################################################
# 3. 表列表和统计信息
###############################################################################
echo -e "${YELLOW}[3/15] 收集表统计信息...${NC}"
$MYSQL_CMD -D "$MYSQL_DATABASE" -e "
SELECT
    TABLE_NAME,
    ENGINE,
    TABLE_ROWS,
    DATA_LENGTH / 1024 / 1024 as data_length_mb,
    INDEX_LENGTH / 1024 / 1024 as index_length_mb,
    DATA_FREE / 1024 / 1024 as data_free_mb,
    (DATA_FREE / (DATA_LENGTH + INDEX_LENGTH + 1)) * 100 as fragmentation_ratio,
    TABLE_COLLATION,
    CREATE_TIME,
    UPDATE_TIME
FROM information_schema.TABLES
WHERE TABLE_SCHEMA = '$MYSQL_DATABASE'
ORDER BY (DATA_LENGTH + INDEX_LENGTH) DESC;
" 2>/dev/null > "$OUTPUT_DIR/03_table_statistics.txt"

###############################################################################
# 4. 所有表的索引信息
###############################################################################
echo -e "${YELLOW}[4/15] 收集索引信息...${NC}"
$MYSQL_CMD -D "$MYSQL_DATABASE" -e "
SELECT
    TABLE_NAME,
    INDEX_NAME,
    SEQ_IN_INDEX,
    COLUMN_NAME,
    CARDINALITY,
    INDEX_TYPE,
    NON_UNIQUE,
    COLLATION,
    SUB_PART,
    NULLABLE
FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA = '$MYSQL_DATABASE'
ORDER BY TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX;
" 2>/dev/null > "$OUTPUT_DIR/04_index_information.txt"

# 生成索引统计摘要
$MYSQL_CMD -D "$MYSQL_DATABASE" -e "
SELECT
    TABLE_NAME,
    INDEX_NAME,
    GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX) as index_columns,
    COUNT(*) as column_count,
    INDEX_TYPE,
    NON_UNIQUE
FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA = '$MYSQL_DATABASE'
GROUP BY TABLE_NAME, INDEX_NAME, INDEX_TYPE, NON_UNIQUE
ORDER BY TABLE_NAME, INDEX_NAME;
" 2>/dev/null > "$OUTPUT_DIR/04_index_summary.txt"

###############################################################################
# 5. 核心表结构
###############################################################################
echo -e "${YELLOW}[5/15] 收集核心表结构...${NC}"
CORE_TABLES="wp_posts wp_postmeta wp_terms wp_term_relationships wp_term_taxonomy wp_options wp_users wp_usermeta wp_comments"
for table in $CORE_TABLES; do
    $MYSQL_CMD -D "$MYSQL_DATABASE" -e "SHOW CREATE TABLE $table\G" 2>/dev/null >> "$OUTPUT_DIR/05_table_structures.txt"
    echo "" >> "$OUTPUT_DIR/05_table_structures.txt"
done

###############################################################################
# 6. 当前进程列表
###############################################################################
echo -e "${YELLOW}[6/15] 收集当前进程列表...${NC}"
$MYSQL_CMD -e "SHOW FULL PROCESSLIST;" 2>/dev/null > "$OUTPUT_DIR/06_processlist.txt"

###############################################################################
# 7. InnoDB状态
###############################################################################
echo -e "${YELLOW}[7/15] 收集InnoDB状态...${NC}"
$MYSQL_CMD -e "SHOW ENGINE INNODB STATUS\G" 2>/dev/null > "$OUTPUT_DIR/07_innodb_status.txt"

###############################################################################
# 8. 表锁等待信息
###############################################################################
echo -e "${YELLOW}[8/15] 收集锁信息...${NC}"
$MYSQL_CMD -e "
SELECT
    r.trx_id as waiting_trx_id,
    r.trx_mysql_thread_id as waiting_thread,
    r.trx_query as waiting_query,
    b.trx_id as blocking_trx_id,
    b.trx_mysql_thread_id as blocking_thread,
    b.trx_query as blocking_query
FROM information_schema.innodb_lock_waits w
JOIN information_schema.innodb_trx b ON b.trx_id = w.blocking_trx_id
JOIN information_schema.innodb_trx r ON r.trx_id = w.requesting_trx_id;
" 2>/dev/null > "$OUTPUT_DIR/08_lock_waits.txt"

# 当前事务
$MYSQL_CMD -e "
SELECT
    trx_id,
    trx_state,
    trx_started,
    TIME_TO_SEC(TIMEDIFF(NOW(), trx_started)) as duration_seconds,
    trx_rows_locked,
    trx_rows_modified,
    trx_mysql_thread_id,
    trx_query
FROM information_schema.innodb_trx
ORDER BY trx_started;
" 2>/dev/null > "$OUTPUT_DIR/08_transactions.txt"

###############################################################################
# 9. 慢查询配置和统计
###############################################################################
echo -e "${YELLOW}[9/15] 收集慢查询配置...${NC}"
$MYSQL_CMD -e "
SHOW VARIABLES LIKE 'slow_query%';
SHOW VARIABLES LIKE 'long_query_time';
SHOW VARIABLES LIKE 'log_queries_not_using_indexes';
" 2>/dev/null > "$OUTPUT_DIR/09_slow_query_config.txt"

# 慢查询统计
$MYSQL_CMD -e "
SELECT
    SCHEMA_NAME as database_name,
    COUNT(*) as slow_query_count,
    AVG(QUERY_TIME) as avg_query_time,
    MAX(QUERY_TIME) as max_query_time,
    SUM(ROWS_SENT) as total_rows_sent,
    SUM(ROWS_EXAMINED) as total_rows_examined,
    SUM(ROWS_EXAMINED) / GREATEST(SUM(ROWS_SENT), 1) as rows_ratio
FROM mysql.slow_log
WHERE START_TIME > DATE_SUB(NOW(), INTERVAL 24 HOUR)
GROUP BY SCHEMA_NAME
ORDER BY slow_query_count DESC;
" 2>/dev/null > "$OUTPUT_DIR/09_slow_query_summary.txt" 2>&1

###############################################################################
# 10. 慢查询TOP SQL
###############################################################################
echo -e "${YELLOW}[10/15] 收集慢查询TOP SQL...${NC}"
$MYSQL_CMD -D "$MYSQL_DATABASE" -e "
SELECT
    SQL_TEXT,
    COUNT(*) as execution_count,
    AVG(QUERY_TIME) as avg_time,
    MAX(QUERY_TIME) as max_time,
    SUM(ROWS_EXAMINED) as total_rows_examined,
    AVG(ROWS_EXAMINED) as avg_rows_examined
FROM mysql.slow_log
WHERE START_TIME > DATE_SUB(NOW(), INTERVAL 24 HOUR)
  AND SQL_TEXT IS NOT NULL
  AND SQL_TEXT NOT LIKE '%SHOW%'
GROUP BY LEFT(SQL_TEXT, 100)
ORDER BY avg_time DESC
LIMIT 20;
" 2>/dev/null > "$OUTPUT_DIR/10_slow_query_top.txt" 2>&1

###############################################################################
# 11. 表统计信息（Cardinality）
###############################################################################
echo -e "${YELLOW}[11/15] 收集表统计详情...${NC}"
$MYSQL_CMD -D "$MYSQL_DATABASE" -e "
SELECT
    TABLE_NAME,
    COLUMN_NAME,
    CARDINALITY,
    NULLABLE,
    COLUMN_TYPE,
    COLUMN_KEY
FROM information_schema.STATISTICS s
JOIN information_schema.COLUMNS c USING (TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME)
WHERE s.TABLE_SCHEMA = '$MYSQL_DATABASE'
  AND s.TABLE_NAME IN ('wp_posts', 'wp_postmeta', 'wp_term_relationships', 'wp_term_taxonomy')
ORDER BY s.TABLE_NAME, s.INDEX_NAME, s.SEQ_IN_INDEX;
" 2>/dev/null > "$OUTPUT_DIR/11_table_cardinality.txt"

###############################################################################
# 12. 当前连接数统计
###############################################################################
echo -e "${YELLOW}[12/15] 收集连接统计...${NC}"
$MYSQL_CMD -e "
SHOW STATUS LIKE 'Threads_connected';
SHOW STATUS LIKE 'Threads_running';
SHOW STATUS LIKE 'Max_used_connections';
SHOW STATUS LIKE 'Connections';
SHOW STATUS LIKE 'Aborted_clients';
SHOW STATUS LIKE 'Aborted_connects';
" 2>/dev/null > "$OUTPUT_DIR/12_connection_stats.txt"

# 连接详情
$MYSQL_CMD -e "
SELECT
    ID,
    USER,
    HOST,
    DB,
    COMMAND,
    TIME,
    STATE,
    LEFT(INFO, 200) as QUERY_PREVIEW
FROM information_schema.PROCESSLIST
WHERE DB = '$MYSQL_DATABASE' OR COMMAND != 'Sleep'
ORDER BY TIME DESC;
" 2>/dev/null > "$OUTPUT_DIR/12_connections_detail.txt"

###############################################################################
# 13. 性能计数器
###############################################################################
echo -e "${YELLOW}[13/15] 收集性能计数器...${NC}"
$MYSQL_CMD -e "
SHOW STATUS LIKE 'Questions';
SHOW STATUS LIKE 'Queries';
SHOW STATUS LIKE 'Com_%';
SHOW STATUS LIKE 'Innodb_%';
SHOW STATUS LIKE 'Handler_%';
SHOW STATUS LIKE 'Created_%';
SHOW STATUS LIKE 'Select_%';
SHOW STATUS LIKE 'Sort_%';
SHOW STATUS LIKE 'Table_locks_%';
" 2>/dev/null > "$OUTPUT_DIR/13_performance_counters.txt"

# 关键性能指标汇总
$MYSQL_CMD -e "
SELECT
    'Questions per second' as metric,
    ROUND(VARIABLE_VALUE / (SELECT VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME = 'UPTIME'), 2) as value
FROM performance_schema.global_status
WHERE VARIABLE_NAME = 'Questions'

UNION ALL

SELECT
    'Slow queries' as metric,
    VARIABLE_VALUE as value
FROM performance_schema.global_status
WHERE VARIABLE_NAME = 'Slow_queries'

UNION ALL

SELECT
    'Table scans' as metric,
    VARIABLE_VALUE as value
FROM performance_schema.global_status
WHERE VARIABLE_NAME = 'Handler_read_rnd_next';
" 2>/dev/null > "$OUTPUT_DIR/13_key_metrics.txt" 2>&1

###############################################################################
# 14. WordPress特定信息
###############################################################################
echo -e "${YELLOW}[14/15] 收集WordPress特定信息...${NC}"

# 文章数量统计
$MYSQL_CMD -D "$MYSQL_DATABASE" -e "
SELECT
    post_status,
    post_type,
    COUNT(*) as count
FROM wp_posts
GROUP BY post_type, post_status
ORDER BY count DESC;
" 2>/dev/null > "$OUTPUT_DIR/14_wp_posts_stats.txt"

# 分类关联统计
$MYSQL_CMD -D "$MYSQL_DATABASE" -e "
SELECT
    tt.taxonomy,
    tt.count as term_count,
    COUNT(tr.object_id) as relationship_count
FROM wp_term_taxonomy tt
LEFT JOIN wp_term_relationships tr ON tt.term_taxonomy_id = tr.term_taxonomy_id
GROUP BY tt.term_taxonomy_id
ORDER BY relationship_count DESC;
" 2>/dev/null > "$OUTPUT_DIR/14_wp_taxonomy_stats.txt"

# postmeta统计
$MYSQL_CMD -D "$MYSQL_DATABASE" -e "
SELECT
    COUNT(*) as total_meta_count,
    COUNT(DISTINCT post_id) as posts_with_meta,
    AVG(meta_count) as avg_meta_per_post
FROM (
    SELECT post_id, COUNT(*) as meta_count
    FROM wp_postmeta
    GROUP BY post_id
) t;
" 2>/dev/null > "$OUTPUT_DIR/14_wp_postmeta_stats.txt"

# 最大postmeta记录
$MYSQL_CMD -D "$MYSQL_DATABASE" -e "
SELECT
    post_id,
    COUNT(*) as meta_count
FROM wp_postmeta
GROUP BY post_id
HAVING COUNT(*) > 50
ORDER BY meta_count DESC
LIMIT 20;
" 2>/dev/null > "$OUTPUT_DIR/14_wp_postmeta_top.txt"

###############################################################################
# 15. 问题SQL的EXPLAIN
###############################################################################
echo -e "${YELLOW}[15/15] 收集问题SQL的EXPLAIN...${NC}"

# 问题SQL1: 分类查询
cat > "$OUTPUT_DIR/15_explain_queries.sql" << 'EOF'
-- 问题SQL1: WordPress分类查询（原始）
EXPLAIN
SELECT wp_posts.ID
FROM wp_posts
LEFT JOIN wp_term_relationships ON (wp_posts.ID = wp_term_relationships.object_id)
WHERE 1=1
  AND wp_posts.ID NOT IN (109095)
  AND (wp_term_relationships.term_taxonomy_id IN (4))
  AND ((wp_posts.post_type = 'post' AND (wp_posts.post_status = 'publish')))
GROUP BY wp_posts.ID
ORDER BY wp_posts.post_date DESC
LIMIT 0, 10;

-- 问题SQL2: 优化版本1（FORCE INDEX）
EXPLAIN
SELECT wp_posts.ID
FROM wp_posts
INNER JOIN wp_term_relationships FORCE INDEX (idx_term_taxonomy_object)
  ON (wp_posts.ID = wp_term_relationships.object_id)
WHERE wp_term_relationships.term_taxonomy_id = 4
  AND wp_posts.post_type = 'post'
  AND wp_posts.post_status = 'publish'
  AND wp_posts.ID != 109095
GROUP BY wp_posts.ID
ORDER BY wp_posts.post_date DESC
LIMIT 10;

-- 问题SQL3: 优化版本2（EXISTS）
EXPLAIN
SELECT wp_posts.ID
FROM wp_posts
WHERE wp_posts.post_type = 'post'
  AND wp_posts.post_status = 'publish'
  AND wp_posts.ID != 109095
  AND EXISTS (
    SELECT 1 FROM wp_term_relationships
    WHERE term_taxonomy_id = 4
      AND object_id = wp_posts.ID
  )
ORDER BY wp_posts.post_date DESC
LIMIT 10;

-- 问题SQL4: 调整JOIN顺序
EXPLAIN
SELECT p.ID
FROM wp_term_relationships tr
INNER JOIN wp_posts p FORCE INDEX (type_status_date) ON (p.ID = tr.object_id)
WHERE tr.term_taxonomy_id = 4
  AND p.post_type = 'post'
  AND p.post_status = 'publish'
  AND p.ID != 109095
GROUP BY p.ID
ORDER BY p.post_date DESC
LIMIT 10;
EOF

$MYSQL_CMD -D "$MYSQL_DATABASE" < "$OUTPUT_DIR/15_explain_queries.sql" 2>/dev/null > "$OUTPUT_DIR/15_explain_results.txt"

###############################################################################
# 生成摘要报告
###############################################################################
echo -e "${YELLOW}生成摘要报告...${NC}"

cat > "$OUTPUT_DIR/SUMMARY_REPORT.txt" << 'EOF'
================================================================================
                     MySQL 性能分析 - 数据收集报告
================================================================================
收集时间: PLACEHOLDER_TIME
数据库: PLACEHOLDER_DATABASE

一、文件清单
================================================================================

文件名                          | 说明
--------------------------------|------------------------------------------
01_mysql_basic_info.txt         | MySQL版本、配置参数
02_databases.txt                | 数据库列表
03_table_statistics.txt         | 表统计信息（大小、行数、碎片率）
04_index_information.txt        | 详细索引信息
04_index_summary.txt            | 索引摘要
05_table_structures.txt         | 核心表结构定义
06_processlist.txt              | 当前进程列表
07_innodb_status.txt            | InnoDB引擎状态
08_lock_waits.txt               | 锁等待信息
08_transactions.txt             | 当前活跃事务
09_slow_query_config.txt        | 慢查询配置
09_slow_query_summary.txt       | 慢查询统计（24小时）
10_slow_query_top.txt           | TOP 20慢查询
11_table_cardinality.txt        | 表统计详情（选择性）
12_connection_stats.txt         | 连接数统计
12_connections_detail.txt       | 当前连接详情
13_performance_counters.txt     | 性能计数器
13_key_metrics.txt              | 关键性能指标
14_wp_posts_stats.txt           | WordPress文章统计
14_wp_taxonomy_stats.txt        | WordPress分类统计
14_wp_postmeta_stats.txt        | Postmeta统计
14_wp_postmeta_top.txt          | Postmeta记录最多的文章
15_explain_queries.sql          | EXPLAIN查询语句
15_explain_results.txt          | EXPLAIN执行结果

================================================================================
二、快速检查清单
================================================================================

请检查以下关键指标：

1. CPU利用率
   - 查看: 06_processlist.txt (当前活跃查询)
   - 查看: 13_key_metrics.txt (Questions per second)

2. 慢查询
   - 查看: 09_slow_query_summary.txt (按数据库统计)
   - 查看: 10_slow_query_top.txt (TOP慢查询)

3. 锁等待
   - 查看: 08_lock_waits.txt (是否有锁等待)
   - 查看: 08_transactions.txt (长事务)

4. 表碎片
   - 查看: 03_table_statistics.txt (fragmentation_ratio列)
   - 碎片率 > 10% 需要考虑OPTIMIZE TABLE

5. 索引问题
   - 查看: 04_index_summary.txt (索引定义)
   - 查看: 11_table_cardinality.txt (索引选择性)
   - 查看: 15_explain_results.txt (执行计划)

6. 连接数
   - 查看: 12_connection_stats.txt
   - Threads_connected 接近 max_connections 需要关注

7. InnoDB状态
   - 查看: 07_innodb_status.txt
   - 关注: SEMAPHORES (锁竞争), TRANSACTIONS (事务)

================================================================================
三、常见问题诊断
================================================================================

问题1: CPU利用率高
  -> 检查: 06_processlist.txt (当前运行的查询)
  -> 检查: 10_slow_query_top.txt (高频慢查询)
  -> 检查: 15_explain_results.txt (执行计划中的type列和rows列)

问题2: 慢查询
  -> 检查: 10_slow_query_top.txt (找到最慢的SQL)
  -> 检查: 15_explain_results.txt (分析执行计划)
  -> 关注: type=ALL (全表扫描), rows数量大, Extra有Using temporary/filesort

问题3: 锁等待
  -> 检查: 08_lock_waits.txt
  -> 检查: 08_transactions.txt (长事务阻塞)

问题4: 表碎片高
  -> 检查: 03_table_statistics.txt (fragmentation_ratio)
  -> 碎片率 > 10% 考虑 OPTIMIZE TABLE

问题5: 索引失效
  -> 检查: 11_table_cardinality.txt (CARDINALITY过低=索引选择性差)
  -> 检查: 04_index_summary.txt (复合索引列顺序是否合理)

================================================================================
四、下一步操作建议
================================================================================

1. 根据EXPLAIN结果优化慢查询SQL
2. 添加缺失的索引（基于04_index_summary.txt分析）
3. 对高碎片表执行 OPTIMIZE TABLE
4. 调整MySQL配置参数（基于01_mysql_basic_info.txt）
5. 解决锁等待问题（优化长事务）

================================================================================
EOF

# 替换占位符
sed -i.bak "s/PLACEHOLDER_TIME/$(date '+%Y-%m-%d %H:%M:%S')/" "$OUTPUT_DIR/SUMMARY_REPORT.txt"
sed -i.bak "s/PLACEHOLDER_DATABASE/$MYSQL_DATABASE/" "$OUTPUT_DIR/SUMMARY_REPORT.txt"
rm -f "$OUTPUT_DIR/SUMMARY_REPORT.txt.bak"

###############################################################################
# 完成
###############################################################################
echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}数据收集完成！${NC}"
echo -e "${GREEN}========================================${NC}"
echo -e "输出目录: ${GREEN}$OUTPUT_DIR${NC}"
echo ""
echo "请查看摘要报告: $OUTPUT_DIR/SUMMARY_REPORT.txt"
echo ""
echo "使用方法："
echo "  1. 设置环境变量（可选）"
echo "     export MYSQL_HOST=your_host"
echo "     export MYSQL_PORT=3306"
echo "     export MYSQL_USER=your_user"
echo "     export MYSQL_PASSWORD=your_password"
echo "     export MYSQL_DATABASE=wordpress"
echo ""
echo "  2. 运行脚本"
echo "     bash mysql_collect.sh"
echo ""
