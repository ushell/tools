#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Cursor 使用事件数据分析报表生成器 v2.0"""

import csv
import json
import argparse
import os
import glob
from datetime import datetime
from collections import defaultdict
from typing import Dict, List, Any, Optional
from dataclasses import dataclass
from pathlib import Path
import statistics

@dataclass
class UsageRecord:
    date: datetime
    kind: str
    model: str
    max_mode: str
    input_with_cache: int
    input_without_cache: int
    cache_read: int
    output_tokens: int
    total_tokens: int
    cost: float
    
    @property
    def is_successful(self): return self.kind == 'Included'
    @property
    def cache_efficiency(self):
        total = self.input_with_cache + self.input_without_cache + self.cache_read
        return (self.cache_read / total * 100) if total > 0 else 0
    @property
    def weekday(self): return self.date.weekday()
    @property
    def week_number(self): return self.date.strftime('%Y-W%W')
    @property
    def month(self): return self.date.strftime('%Y-%m')
    @property
    def date_str(self): return self.date.date().isoformat()
    @property
    def hour(self): return self.date.hour

@dataclass
class Statistics:
    count: int = 0
    cost: float = 0.0
    tokens: int = 0
    input_with_cache: int = 0
    input_without_cache: int = 0
    cache_read: int = 0
    output_tokens: int = 0
    
    def add(self, r):
        self.count += 1
        self.cost += r.cost
        self.tokens += r.total_tokens
        self.input_with_cache += r.input_with_cache
        self.input_without_cache += r.input_without_cache
        self.cache_read += r.cache_read
        self.output_tokens += r.output_tokens
    
    @property
    def avg_cost(self): return self.cost / self.count if self.count > 0 else 0
    @property
    def avg_tokens(self): return self.tokens / self.count if self.count > 0 else 0
    @property
    def cache_efficiency(self):
        total = self.input_with_cache + self.input_without_cache + self.cache_read
        return (self.cache_read / total * 100) if total > 0 else 0

class UsageAnalyzer:
    def __init__(self, data_dir='.'):
        self.data_dir = Path(data_dir)
        self.records = []
        self.csv_files = []
    
    def find_csv_files(self):
        files = set()
        for p in [self.data_dir / '**' / 'usage-events-*.csv', self.data_dir / 'usage-events-*.csv']:
            files.update(glob.glob(str(p), recursive=True))
        self.csv_files = sorted(files)
        return self.csv_files
    
    def parse_csv(self, fp):
        records = []
        with open(fp, 'r', encoding='utf-8') as f:
            for row in csv.DictReader(f):
                try:
                    records.append(UsageRecord(
                        date=datetime.fromisoformat(row['Date'].replace('Z', '+00:00')),
                        kind=row['Kind'], model=row['Model'], max_mode=row['Max Mode'],
                        input_with_cache=int(row['Input (w/ Cache Write)']),
                        input_without_cache=int(row['Input (w/o Cache Write)']),
                        cache_read=int(row['Cache Read']),
                        output_tokens=int(row['Output Tokens']),
                        total_tokens=int(row['Total Tokens']),
                        cost=float(row['Cost'])
                    ))
                except: pass
        return records
    
    def load_data(self, files=None, month=None):
        if files is None: files = self.find_csv_files()
        if not files: return 0
        self.records = []
        for fp in files:
            recs = self.parse_csv(fp)
            self.records.extend(recs)
            print(f"已加载: {fp} ({len(recs)} 条记录)")
        if month: self.records = [r for r in self.records if r.month == month]
        self.records.sort(key=lambda x: x.date)
        return len(self.records)
    
    def get_overall_stats(self):
        s = Statistics()
        for r in self.records: s.add(r)
        return s
    
    def get_successful_records(self): return [r for r in self.records if r.is_successful]
    
    def group_by(self, fn):
        g = defaultdict(Statistics)
        for r in self.records: g[fn(r)].add(r)
        return dict(g)
    
    def get_date_stats(self): return self.group_by(lambda r: r.date_str)
    def get_week_stats(self): return self.group_by(lambda r: r.week_number)
    def get_month_stats(self): return self.group_by(lambda r: r.month)
    def get_model_stats(self): return self.group_by(lambda r: r.model)
    def get_kind_stats(self): return self.group_by(lambda r: r.kind)
    
    def get_hour_stats(self):
        g = defaultdict(Statistics)
        for r in self.get_successful_records(): g[r.hour].add(r)
        return dict(g)
    
    def get_weekday_detail_stats(self):
        names = ['周一', '周二', '周三', '周四', '周五', '周六', '周日']
        g = {n: Statistics() for n in names}
        for r in self.get_successful_records(): g[names[r.weekday]].add(r)
        return g
    
    def get_top_cost_records(self, n=10):
        return sorted(self.get_successful_records(), key=lambda x: x.cost, reverse=True)[:n]
    
    def get_cost_statistics(self):
        costs = [r.cost for r in self.get_successful_records()]
        if not costs: return {}
        return {'mean': statistics.mean(costs), 'median': statistics.median(costs), 'min': min(costs), 'max': max(costs)}
    
    def estimate_monthly_cost(self):
        if not self.records: return {}
        ds = self.get_date_stats()
        if not ds: return {}
        dc = [s.cost for s in ds.values()]
        avg = statistics.mean(dc)
        first = min(r.date for r in self.records)
        last = max(r.date for r in self.records)
        days = (last.date() - first.date()).days + 1
        return {'current_total': sum(dc), 'daily_average': avg, 'days_used': days, 'estimated_total': sum(dc) + avg * max(0, 30 - days)}

class ReportGenerator:
    def __init__(self, analyzer): self.analyzer = analyzer
    
    def generate_text_report(self):
        lines = ["=" * 80, "Cursor 使用事件数据分析报表", "=" * 80, ""]
        o = self.analyzer.get_overall_stats()
        lines += [f"总记录数: {o.count:,}", f"总成本: ${o.cost:.2f}", f"总令牌数: {o.tokens:,}", f"缓存命中率: {o.cache_efficiency:.1f}%", ""]
        e = self.analyzer.estimate_monthly_cost()
        if e: lines += [f"日均成本: ${e['daily_average']:.2f}", f"预估月度: ${e['estimated_total']:.2f}", ""]
        for m, s in sorted(self.analyzer.get_model_stats().items(), key=lambda x: x[1].cost, reverse=True):
            lines.append(f"  {m}: ${s.cost:.2f}")
        lines += ["", "=" * 80, f"生成时间: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}"]
        return "\n".join(lines)
    
    def generate_html_report(self):
        o = self.analyzer.get_overall_stats()
        ds = self.analyzer.get_date_stats()
        ms = self.analyzer.get_model_stats()
        hs = self.analyzer.get_hour_stats()
        ws = self.analyzer.get_week_stats()
        mos = self.analyzer.get_month_stats()
        wd = self.analyzer.get_weekday_detail_stats()
        e = self.analyzer.estimate_monthly_cost()
        cs = self.analyzer.get_cost_statistics()
        tc = self.analyzer.get_top_cost_records(15)
        
        sd = sorted(ds.keys())
        dd = {'labels': sd, 'costs': [ds[d].cost for d in sd], 'counts': [ds[d].count for d in sd]}
        sh = sorted(hs.keys())
        hd = {'labels': [f"{h:02d}:00" for h in sh], 'costs': [hs[h].cost for h in sh]}
        mn = list(ms.keys())
        md = {'labels': mn, 'costs': [ms[m].cost for m in mn], 'counts': [ms[m].count for m in mn]}
        sw = sorted(ws.keys())
        wkd = {'labels': sw, 'costs': [ws[w].cost for w in sw]}
        sm = sorted(mos.keys())
        mod = {'labels': sm, 'costs': [mos[m].cost for m in sm], 'counts': [mos[m].count for m in sm]}
        wdl = list(wd.keys())
        wdd = {'labels': wdl, 'costs': [wd[d].cost for d in wdl]}
        
        nd = max(1, len(sd))
        dac = o.cost / nd
        dr = f"{sd[0]} ~ {sd[-1]}" if sd else "N/A"
        cf = ', '.join([os.path.basename(f) for f in self.analyzer.csv_files])
        
        dtr = ""
        for d in sorted(ds.keys(), reverse=True)[:15]:
            s = ds[d]
            dtr += f'<tr><td>{d}</td><td>{s.count:,}</td><td class="cost">${s.cost:.2f}</td><td>{s.tokens:,}</td><td>{s.cache_efficiency:.1f}%</td></tr>'
        
        tcr = ""
        for r in tc:
            tcr += f'<tr><td>{r.date.strftime("%Y-%m-%d %H:%M")}</td><td class="model-tag">{r.model}</td><td class="cost">${r.cost:.2f}</td><td>{r.total_tokens:,}</td></tr>'
        
        es = ""
        if e:
            es = f'<div class="estimate-card"><div class="ei"><div class="l">当前已使用</div><div class="v">${e["current_total"]:.2f}</div></div><div class="ei"><div class="l">日均成本</div><div class="v">${e["daily_average"]:.2f}</div></div><div class="ei"><div class="l">已用天数</div><div class="v">{e["days_used"]}</div></div><div class="ei"><div class="l">预估月度</div><div class="v hl">${e["estimated_total"]:.2f}</div></div></div>'
        
        mcs = ""
        mcj = ""
        if len(mod['labels']) > 1:
            mcs = '<div class="section"><h2>📅 月度对比</h2><div class="chart-container"><canvas id="monthChart"></canvas></div></div>'
            mcj = f'new Chart(document.getElementById("monthChart"),{{type:"bar",data:{{labels:{json.dumps(mod["labels"])},datasets:[{{label:"成本($)",data:{json.dumps(mod["costs"])},backgroundColor:"#6366f1"}}]}},options:{{responsive:true,maintainAspectRatio:false}}}});'
        
        return f"""<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1.0">
<title>Cursor 使用分析</title>
<script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.0/dist/chart.umd.min.js"></script>
<style>
:root{{--bg:#0f0f23;--card:#1e1e3f;--accent:#6366f1;--text:#f8fafc;--muted:#94a3b8}}
*{{margin:0;padding:0;box-sizing:border-box}}
body{{font-family:system-ui,sans-serif;background:var(--bg);color:var(--text);padding:20px;min-height:100vh}}
.container{{max-width:1400px;margin:0 auto}}
.header{{text-align:center;padding:40px;background:var(--card);border-radius:20px;margin-bottom:30px}}
.header h1{{font-size:2.5rem;background:linear-gradient(135deg,#6366f1,#a855f7);-webkit-background-clip:text;-webkit-text-fill-color:transparent}}
.header .sub{{color:var(--muted);margin-top:10px}}
.stats{{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:20px;margin-bottom:30px}}
.stat{{background:var(--card);padding:25px;border-radius:15px}}
.stat .l{{font-size:0.8rem;color:var(--muted);text-transform:uppercase;margin-bottom:8px}}
.stat .v{{font-size:1.8rem;font-weight:bold}}
.stat .v.cost{{color:#22c55e}}
.stat .v.rate{{color:#f59e0b}}
.estimate-card{{background:linear-gradient(135deg,rgba(99,102,241,0.15),rgba(168,85,247,0.15));border:1px solid rgba(99,102,241,0.3);border-radius:15px;padding:25px;margin-bottom:30px;display:grid;grid-template-columns:repeat(auto-fit,minmax(150px,1fr));gap:20px}}
.ei{{text-align:center}}
.ei .l{{font-size:0.75rem;color:var(--muted);text-transform:uppercase}}
.ei .v{{font-size:1.5rem;font-weight:600;margin-top:5px}}
.ei .v.hl{{color:#22c55e}}
.section{{margin-bottom:30px}}
.section h2{{font-size:1.3rem;margin-bottom:15px;padding-bottom:10px;border-bottom:1px solid rgba(99,102,241,0.2)}}
.chart-container{{background:var(--card);padding:20px;border-radius:15px;height:300px}}
.two-col{{display:grid;grid-template-columns:repeat(auto-fit,minmax(400px,1fr));gap:20px}}
table{{width:100%;border-collapse:collapse;background:var(--card);border-radius:15px;overflow:hidden}}
th{{background:rgba(99,102,241,0.1);padding:12px;text-align:left;font-size:0.8rem;text-transform:uppercase}}
td{{padding:12px;border-bottom:1px solid rgba(255,255,255,0.05);font-size:0.9rem}}
tr:hover td{{background:rgba(99,102,241,0.05)}}
.cost{{color:#22c55e}}
.model-tag{{background:rgba(139,92,246,0.15);color:#a855f7;padding:3px 8px;border-radius:4px;font-size:0.8rem}}
.footer{{text-align:center;padding:30px;color:var(--muted);font-size:0.85rem;border-top:1px solid rgba(99,102,241,0.2);margin-top:30px}}
</style>
</head>
<body>
<div class="container">
<header class="header"><h1>⚡ Cursor 使用分析</h1><p class="sub">📅 {dr}</p></header>
<div class="stats">
<div class="stat"><div class="l">总记录数</div><div class="v">{o.count:,}</div></div>
<div class="stat"><div class="l">总成本</div><div class="v cost">${o.cost:.2f}</div></div>
<div class="stat"><div class="l">总令牌</div><div class="v">{o.tokens:,}</div></div>
<div class="stat"><div class="l">缓存命中率</div><div class="v rate">{o.cache_efficiency:.1f}%</div></div>
<div class="stat"><div class="l">日均成本</div><div class="v cost">${dac:.2f}</div></div>
<div class="stat"><div class="l">最高单次</div><div class="v" style="color:#f43f5e">${cs.get('max',0):.2f}</div></div>
</div>
{es}
<div class="section"><h2>📈 每日趋势</h2><div class="chart-container"><canvas id="dateChart"></canvas></div></div>
{mcs}
<div class="section"><h2>📆 周度趋势</h2><div class="chart-container"><canvas id="weekChart"></canvas></div></div>
<div class="section"><h2>🤖 模型分析</h2><div class="two-col"><div class="chart-container"><canvas id="modelCostChart"></canvas></div><div class="chart-container"><canvas id="modelCountChart"></canvas></div></div></div>
<div class="section"><h2>⏰ 时间分布</h2><div class="two-col"><div class="chart-container"><canvas id="hourChart"></canvas></div><div class="chart-container"><canvas id="weekdayChart"></canvas></div></div></div>
<div class="section"><h2>📋 按日期统计</h2><table><thead><tr><th>日期</th><th>记录数</th><th>成本</th><th>令牌数</th><th>缓存率</th></tr></thead><tbody>{dtr}</tbody></table></div>
<div class="section"><h2>🔥 成本最高 Top 15</h2><table><thead><tr><th>时间</th><th>模型</th><th>成本</th><th>令牌数</th></tr></thead><tbody>{tcr}</tbody></table></div>
<footer class="footer"><p>生成时间: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}</p><p>数据文件: {cf}</p></footer>
</div>
<script>
Chart.defaults.color='#94a3b8';
Chart.defaults.borderColor='rgba(99,102,241,0.1)';
const gc=['#6366f1','#8b5cf6','#a855f7','#d946ef','#ec4899','#f43f5e','#ef4444','#f97316','#f59e0b','#eab308','#84cc16','#22c55e'];
new Chart(document.getElementById('dateChart'),{{type:'line',data:{{labels:{json.dumps(dd['labels'])},datasets:[{{label:'成本($)',data:{json.dumps(dd['costs'])},borderColor:'#6366f1',backgroundColor:'rgba(99,102,241,0.1)',fill:true,tension:0.4,yAxisID:'y'}},{{label:'次数',data:{json.dumps(dd['counts'])},borderColor:'#22c55e',tension:0.4,yAxisID:'y1'}}]}},options:{{responsive:true,maintainAspectRatio:false,scales:{{y:{{position:'left'}},y1:{{position:'right',grid:{{drawOnChartArea:false}}}}}}}}}});
new Chart(document.getElementById('weekChart'),{{type:'bar',data:{{labels:{json.dumps(wkd['labels'])},datasets:[{{data:{json.dumps(wkd['costs'])},backgroundColor:gc}}]}},options:{{responsive:true,maintainAspectRatio:false,plugins:{{legend:{{display:false}}}}}}}});
{mcj}
new Chart(document.getElementById('modelCostChart'),{{type:'doughnut',data:{{labels:{json.dumps(md['labels'])},datasets:[{{data:{json.dumps(md['costs'])},backgroundColor:gc}}]}},options:{{responsive:true,maintainAspectRatio:false,cutout:'60%',plugins:{{title:{{display:true,text:'成本分布'}}}}}}}});
new Chart(document.getElementById('modelCountChart'),{{type:'doughnut',data:{{labels:{json.dumps(md['labels'])},datasets:[{{data:{json.dumps(md['counts'])},backgroundColor:gc}}]}},options:{{responsive:true,maintainAspectRatio:false,cutout:'60%',plugins:{{title:{{display:true,text:'次数分布'}}}}}}}});
new Chart(document.getElementById('hourChart'),{{type:'bar',data:{{labels:{json.dumps(hd['labels'])},datasets:[{{data:{json.dumps(hd['costs'])},backgroundColor:'#6366f1'}}]}},options:{{responsive:true,maintainAspectRatio:false,plugins:{{legend:{{display:false}},title:{{display:true,text:'每小时成本'}}}}}}}});
new Chart(document.getElementById('weekdayChart'),{{type:'bar',data:{{labels:{json.dumps(wdd['labels'])},datasets:[{{data:{json.dumps(wdd['costs'])},backgroundColor:gc}}]}},options:{{responsive:true,maintainAspectRatio:false,plugins:{{legend:{{display:false}},title:{{display:true,text:'按星期分布'}}}}}}}});
</script>
</body>
</html>"""

def main():
    parser = argparse.ArgumentParser(description='Cursor 使用数据分析工具', epilog='示例: %(prog)s --with-text')
    parser.add_argument('-d', '--directory', default='.', help='数据目录')
    parser.add_argument('-f', '--files', nargs='+', help='指定CSV文件')
    parser.add_argument('-m', '--month', help='过滤月份 (YYYY-MM)')
    parser.add_argument('-o', '--output', default='usage_report', help='输出文件名前缀')
    parser.add_argument('--with-text', action='store_true', help='同时生成文本报表')
    parser.add_argument('--text-only', action='store_true', help='只生成文本报表')
    parser.add_argument('-v', '--verbose', action='store_true', help='显示详细信息')
    args = parser.parse_args()
    
    print("🚀 Cursor 使用数据分析工具 v2.0")
    print("=" * 50)
    
    analyzer = UsageAnalyzer(args.directory)
    count = analyzer.load_data(files=args.files, month=args.month)
    
    if count == 0:
        print("❌ 未找到有效数据")
        return 1
    
    print(f"\n✅ 共加载 {count:,} 条记录")
    
    generator = ReportGenerator(analyzer)
    
    if args.with_text or args.text_only:
        print("\n📝 生成文本报表...")
        with open(f"{args.output}.txt", 'w', encoding='utf-8') as f:
            txt = generator.generate_text_report()
            f.write(txt)
        print(f"   ✅ 已保存: {args.output}.txt")
        if args.verbose: print("\n" + txt)
    
    if not args.text_only:
        print("\n🎨 生成HTML报表...")
        with open(f"{args.output}.html", 'w', encoding='utf-8') as f:
            f.write(generator.generate_html_report())
        print(f"   ✅ 已保存: {args.output}.html")
    
    print("\n🎉 报表生成完成!")
    return 0

if __name__ == '__main__':
    exit(main())
