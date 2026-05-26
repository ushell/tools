#!/bin/bash

################################################################################
# MySQL CPU 高占用排查脚本
# 用于快速定位导致 MySQL CPU 高的具体原因
################################################################################

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# MySQL 配置
MYSQL_HOST="${MYSQL_HOST:-localhost}"
MYSQL_PORT="${MYSQL_PORT:-3306}"
MYSQL_USER="${MYSQL_USER:-root}"
MYSQL_PASSWORD="${MYSQL_PASSWORD:-}"
MYSQL_DATABASE="${MYSQL_DATABASE:-wordpress}"
MYSQL_CMD_PATH="${MYSQL_CMD_PATH:-mysql}"

# 构建命令
MYSQL_CMD="$MYSQL_CMD_PATH -h $MYSQL_HOST -P $MYSQL_PORT -u $MYSQL_USER"
[ -n "$MYSQL_PASSWORD" ] && MYSQL_CMD="$MYSQL_CMD -p$MYSQL_PASSWORD"

OUTPUT_DIR="./cpu_diagnose_$(date +%Y%m%d_%H%M%S)"
mkdir -p "$OUTPUT_DIR"

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}MySQL CPU 高占用排查${NC}"
echo -e "${GREEN}========================================${NC}"
echo "输出目录: $OUTPUT_DIR"
echo ""

###############################################################################
# 1. 当前活跃查询 (最重要！)
###############################################################################
echo -e "${YELLOW}[1/10] 抓取当前活跃查询...${NC}"
$MYSQL_CMD -e "
SELECT
    Id,
    User,
    Host,
    DB,
    Command,
    Time as duration_sec,
    STATE,
    LEFT(INFO, 500) as query_preview
FROM information_schema.PROCESSLIST
WHERE COMMAND != 'Sleep'
  AND TIME > 0
ORDER BY TIME DESC;
" 2>/dev/null > "$OUTPUT_DIR/01_active_queries.txt"

echo "当前活跃慢查询:"
cat "$OUTPUT_DIR/01_active_queries.txt"
echo ""

###############################################################################
# 2. 当前 CPU 状态
###############################################################################
echo -e "${YELLOW}[2/10] 检查 MySQL 线程...${NC}"
$MYSQL_CMD -e "
SHOW STATUS LIKE 'Threads_running';
SHOW STATUS LIKE 'Threads_connected';
SHOW STATUS LIKE 'Qcache_hits';
" 2>/dev/null > "$OUTPUT_DIR/02_threads_status.txt"

cat "$OUTPUT_DIR/02_threads_status.txt"
echo ""

###############################################################################
# 3. 慢查询统计 (最近1小时)
###############################################################################
echo -e "${YELLOW}[3/10] 统计慢查询模式...${NC}"

# 按查询模式分组统计
$MYSQL_CMD -D "$MYSQL_DATABASE" -e "
SELECT
    LEFT(SQL_TEXT, 100) as query_pattern,
    COUNT(*) as exec_count,
    ROUND(AVG(QUERY_TIME), 3) as avg_time,
    ROUND(MAX(QUERY_TIME), 3) as max_time,
    SUM(ROWS_EXAMINED) as total_rows_examined,
    ROUND(AVG(ROWS_EXAMINED), 0) as avg_rows_examined
FROM mysql.slow_log
WHERE START_TIME > DATE_SUB(NOW(), INTERVAL 1 HOUR)
  AND SQL_TEXT IS NOT NULL
  AND SQL_TEXT NOT LIKE '%SHOW%'
  AND SQL_TEXT NOT LIKE '%information_schema%'
GROUP BY LEFT(SQL_TEXT, 100)
ORDER BY avg_time DESC
LIMIT 20;
" 2>/dev/null > "$OUTPUT_DIR/03_slow_query_patterns.txt" 2>&1

echo "慢查询 TOP 模式:"
head -20 "$OUTPUT_DIR/03_slow_query_patterns.txt"
echo ""

###############################################################################
# 4. 高开销查询 (扫描行数多)
###############################################################################
echo -e "${YELLOW}[4/10] 扫描行数最多的查询...${NC}"
$MYSQL_CMD -D "$MYSQL_DATABASE" -e "
SELECT
    LEFT(SQL_TEXT, 150) as query,
    ROWS_EXAMINED as rows_scanned,
    ROWS_SENT as rows_returned,
    ROUND(QUERY_TIME, 3) as query_time,
    START_TIME
FROM mysql.slow_log
WHERE START_TIME > DATE_SUB(NOW(), INTERVAL 1 HOUR)
  AND ROWS_EXAMINED > 10000
ORDER BY ROWS_EXAMINED DESC
LIMIT 20;
" 2>/dev/null > "$OUTPUT_DIR/04_high_scan_queries.txt" 2>&1

head -20 "$OUTPUT_DIR/04_high_scan_queries.txt"
echo ""

###############################################################################
# 5. 当前正在执行的慢查询详情
###############################################################################
echo -e "${YELLOW}[5/10] 分析当前慢查询的 EXPLAIN...${NC}"

# 从 processlist 获取慢查询并 EXPLAIN
$MYSQL_CMD -D "$MYSQL_DATABASE" -e "
SELECT CONCAT('EXPLAIN ', LEFT(INFO, 1000))
FROM information_schema.PROCESSLIST
WHERE COMMAND = 'Query'
  AND TIME > 2
  AND INFO IS NOT NULL
LIMIT 5;
" 2>/dev/null > "$OUTPUT_DIR/05_current_slow_queries.sql"

# 执行 EXPLAIN
if [ -s "$OUTPUT_DIR/05_current_slow_queries.sql" ]; then
    $MYSQL_CMD -D "$MYSQL_DATABASE" < "$OUTPUT_DIR/05_current_slow_queries.sql" 2>/dev/null > "$OUTPUT_DIR/05_explain_results.txt"
    echo "EXPLAIN 结果:"
    cat "$OUTPUT_DIR/05_explain_results.txt"
else
    echo "当前没有慢查询需要 EXPLAIN"
fi
echo ""

###############################################################################
# 6. InnoDB 状态
###############################################################################
echo -e "${YELLOW}[6/10] 检查 InnoDB 状态...${NC}"
$MYSQL_CMD -e "SHOW ENGINE INNODB STATUS\G" 2>/dev/null > "$OUTPUT_DIR/06_innodb_status.txt"

# 提取关键信息
echo -e "最近死锁:"
grep -A 20 "LATEST DETECTED DEADLOCK" "$OUTPUT_DIR/06_innodb_status.txt" | head -25 || echo "无死锁"
echo ""

echo -e "事务:"
grep -A 10 "TRANSACTIONS" "$OUTPUT_DIR/06_innodb_status.txt" | head -15
echo ""

###############################################################################
# 7. 表访问统计
###############################################################################
echo -e "${YELLOW}[7/10] 表访问统计...${NC}"
$MYSQL_CMD -e "
SELECT
    OBJECT_SCHEMA as table_schema,
    OBJECT_NAME as table_name,
    COUNT_READ as read_count,
    COUNT_WRITE as write_count,
    SUM_TIMER_WAIT/1000000000000 as total_time_sec
FROM performance_schema.table_io_waits_summary_by_table
WHERE OBJECT_SCHEMA = '$MYSQL_DATABASE'
ORDER BY SUM_TIMER_WAIT DESC
LIMIT 20;
" 2>/dev/null > "$OUTPUT_DIR/07_table_access_stats.txt"

cat "$OUTPUT_DIR/07_table_access_stats.txt"
echo ""

###############################################################################
# 8. 索引使用统计
###############################################################################
echo -e "${YELLOW}[8/10] 索引效率分析...${NC}"
$MYSQL_CMD -D "$MYSQL_DATABASE" -e "
SELECT
    TABLE_NAME,
    INDEX_NAME,
    COUNT_READ,
    COUNT_FETCH,
    COUNT_INSERT,
    COUNT_UPDATE,
    COUNT_DELETE
FROM performance_schema.table_io_waits_summary_by_index_usage
WHERE OBJECT_SCHEMA = '$MYSQL_DATABASE'
  AND INDEX_NAME IS NOT NULL
ORDER BY COUNT_READ DESC
LIMIT 20;
" 2>/dev/null > "$OUTPUT_DIR/08_index_usage.txt"

cat "$OUTPUT_DIR/08_index_usage.txt"
echo ""

###############################################################################
# 9. 全表扫描检测
###############################################################################
echo -e "${YELLOW}[9/10] 检测全表扫描...${NC}"
$MYSQL_CMD -e "
SELECT
    OBJECT_SCHEMA,
    OBJECT_NAME,
    COUNT_READ,
    COUNT_FETCH,
    SUM_TIMER_WAIT/1000000000000 as total_time_sec
FROM performance_schema.table_io_waits_summary_by_index_usage
WHERE OBJECT_SCHEMA = '$MYSQL_DATABASE'
  AND INDEX_NAME = 'PRIMARY'
  AND COUNT_READ > 1000
ORDER BY COUNT_READ DESC
LIMIT 20;
" 2>/dev/null > "$OUTPUT_DIR/09_full_scan_stats.txt"

cat "$OUTPUT_DIR/09_full_scan_stats.txt"
echo ""

###############################################################################
# 10. 缓存命中率
###############################################################################
echo -e "${YELLOW}[10/10] 缓存效率...${NC}"
$MYSQL_CMD -e "
SHOW STATUS LIKE 'Innodb_buffer_pool_read%';
SHOW STATUS LIKE 'Qcache%';
SHOW STATUS LIKE 'Table_locks_waited';
" 2>/dev/null > "$OUTPUT_DIR/10_cache_stats.txt"

cat "$OUTPUT_DIR/10_cache_stats.txt"

# 计算缓存命中率
READ_REQUESTS=$($MYSQL_CMD -N -e "SHOW STATUS LIKE 'Innodb_buffer_pool_read_requests';" 2>/dev/null | awk '{print $2}')
READS=$($MYSQL_CMD -N -e "SHOW STATUS LIKE 'Innodb_buffer_pool_reads';" 2>/dev/null | awk '{print $2}')

if [ -n "$READ_REQUESTS" ] && [ "$READ_REQUESTS" -gt 0 ]; then
    HIT_RATE=$(echo "scale=4; (1 - $READS / $READ_REQUESTS) * 100" | bc -l 2>/dev/null || echo "N/A")
    echo ""
    echo -e "${GREEN}InnoDB 缓存命中率: ${HIT_RATE}%${NC}"
    if [ "${HIT_RATE%%.*}" -lt 90 ]; then
        echo -e "${YELLOW}⚠️  命中率低于 90%，考虑增加 innodb_buffer_pool_size${NC}"
    fi
fi
echo ""

###############################################################################
# 生成诊断报告
###############################################################################
echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}诊断完成！${NC}"
echo -e "${GREEN}========================================${NC}"
echo -e "详细报告保存在: ${GREEN}$OUTPUT_DIR${NC}"
echo ""

# 快速诊断建议
echo -e "${YELLOW}快速诊断建议:${NC}"
echo ""

# 检查是否有长查询
LONG_QUERIES=$(grep -c "duration_sec" "$OUTPUT_DIR/01_active_queries.txt" 2>/dev/null || echo 0)
if [ "$LONG_QUERIES" -gt 0 ]; then
    echo -e "${RED}1. 发现活跃慢查询 - 查看 01_active_queries.txt${NC}"
fi

# 检查缓存命中率
if [ -n "$HIT_RATE" ] && [ "${HIT_RATE%%.*}" -lt 90 ]; then
    echo -e "${RED}2. InnoDB 缓存命中率低 (${HIT_RATE_RATE}%) - 建议增加 innodb_buffer_pool_size${NC}"
fi

# 检查线程数
THREADS_RUNNING=$($MYSQL_CMD -N -e "SHOW STATUS LIKE 'Threads_running';" 2>/dev/null | awk '{print $2}')
if [ -n "$THREADS_RUNNING" ] && [ "$THREADS_RUNNING" -gt 10 ]; then
    echo -e "${RED}3. 并发线程高 ($THREADS_RUNNING) - 可能有锁等待或慢查询${NC}"
fi

echo ""
echo -e "${BLUE}下一步操作:${NC}"
echo "  1. 查看慢查询: cat $OUTPUT_DIR/03_slow_query_patterns.txt"
echo "  2. 查看活跃查询: cat $OUTPUT_DIR/01_active_queries.txt"
echo "  3. 查看 EXPLAIN: cat $OUTPUT_DIR/05_explain_results.txt"
echo ""
