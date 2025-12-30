#!/usr/bin/env python3
"""
git_codeline_stats
统计指定时间范围内 Git 提交的新增/删除行数，并按作者汇总。

依赖：
    pip install GitPython

示例：
    ./git_codeline_stats.py --since 2025-09-01 --until 2025-12-12
"""

import argparse
from datetime import datetime
import os
import sys
from typing import Dict, Tuple

try:
    import git
except ImportError:
    print("缺少依赖 GitPython，请先安装：pip install GitPython", file=sys.stderr)
    sys.exit(1)

ASCII_LOGO = r"""
   ____ _ _   ____          _      _ _            _        _       
  / ___(_) |_|  _ \ ___  __| | ___| (_)_ __   ___| |_ __ _| |_ ___ 
 | |  _| | __| |_) / _ \/ _` |/ _ \ | | '_ \ / _ \ __/ _` | __/ _ \
 | |_| | | |_|  _ <  __/ (_| |  __/ | | | | |  __/ || (_| | ||  __/
  \____|_|\__|_| \_\___|\__,_|\___|_|_|_| |_|\___|\__\__,_|\__\___|
"""


def parse_date(date_str: str) -> datetime:
    """将 YYYY-MM-DD 字符串解析为 datetime 对象。"""
    try:
        return datetime.strptime(date_str, "%Y-%m-%d")
    except ValueError:
        raise argparse.ArgumentTypeError("日期格式应为 YYYY-MM-DD")


def collect_stats(repo_path: str, since: datetime, until: datetime) -> Tuple[int, int, Dict[str, Dict[str, int]]]:
    """遍历指定时间范围的提交，返回总新增/删除行数及按作者统计。"""
    try:
        repo = git.Repo(repo_path)
    except git.exc.InvalidGitRepositoryError:
        print("错误: 指定路径不是有效的 Git 仓库", file=sys.stderr)
        sys.exit(1)

    insertions_total = 0
    deletions_total = 0
    stats: Dict[str, Dict[str, int]] = {}

    for commit in repo.iter_commits(since=since, until=until):
        stats_data = commit.stats
        insertions = stats_data.total["insertions"]
        deletions = stats_data.total["deletions"]
        insertions_total += insertions
        deletions_total += deletions

        author = commit.author.name
        if author not in stats:
            stats[author] = {"insertions": 0, "deletions": 0}
        stats[author]["insertions"] += insertions
        stats[author]["deletions"] += deletions

    return insertions_total, deletions_total, stats


def print_report(since: datetime, until: datetime, insertions: int, deletions: int, stats: Dict[str, Dict[str, int]]) -> None:
    net_change = insertions - deletions
    print(ASCII_LOGO)
    print(f"时间段: {since.date()} 到 {until.date()}")
    print(f"总新增行数: {insertions}")
    print(f"总删除行数: {deletions}")
    print(f"净变化: {net_change} 行")
    print("\n按作者统计:")
    for author, data in sorted(stats.items(), key=lambda x: x[1]["insertions"], reverse=True):
        net = data["insertions"] - data["deletions"]
        print(f"  {author}: +{data['insertions']} / -{data['deletions']} (净: {net})")


def main() -> None:
    parser = argparse.ArgumentParser(
        description="统计时间范围内的 Git 提交行数（新增/删除），并按作者汇总",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=(
            "示例:\n"
            "  ./git_codeline_stats.py --since 2025-09-01 --until 2025-12-12\n"
            "  ./git_codeline_stats.py --repo /path/to/repo --since 2024-01-01 --until 2024-03-31\n"
        ),
    )
    parser.add_argument("--repo", default=os.getcwd(), help="Git 仓库路径，默认为当前目录")
    parser.add_argument("--since", type=parse_date, required=True, help="起始日期，格式 YYYY-MM-DD")
    parser.add_argument("--until", type=parse_date, required=True, help="结束日期，格式 YYYY-MM-DD")
    parser.add_argument(
        "-V",
        "--version",
        action="version",
        version="git_codeline_stats 1.0.0",
        help="显示版本号并退出",
    )
    args = parser.parse_args()

    if args.since > args.until:
        parser.error("--since 不能晚于 --until")

    insertions, deletions, stats = collect_stats(args.repo, args.since, args.until)
    print_report(args.since, args.until, insertions, deletions, stats)


if __name__ == "__main__":
    main()
