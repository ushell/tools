# 🔧 Tools

日常工具脚本集合

## 工具列表

| 工具 | 语言 | 说明 |
|------|------|------|
| `cursor/usage_stats.py` | Python | Cursor 使用数据分析，生成 HTML/文本报表 |
| `git/git_codeline_stats.py` | Python | Git 代码行统计，按作者汇总新增/删除行数 |
| `memcache/memcc.go` | Go | Memcached CLI 客户端，支持 get/set/delete/stats |
| `mysql/mysql_packet_parser.py` | Python | 从 tcpdump 抓包还原 MySQL 查询 |
| `nginx/nginx_log_analyse.go` | Go | Nginx 日志分析，统计 IP/URL/UA/状态码 Top10 |

## 快速使用

```bash
# Cursor 使用分析
python cursor/usage_stats.py -d ~/cursor-data

# Git 统计
pip install GitPython
./git/git_codeline_stats.py --since 2025-01-01 --until 2025-12-31

# Memcached 操作
go run memcache/memcc.go -H localhost get mykey

# MySQL 抓包分析
pip install scapy
python mysql/mysql_packet_parser.py capture.pcap

# Nginx 日志分析
go run nginx/nginx_log_analyse.go access.log
```
