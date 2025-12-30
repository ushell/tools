#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
MySQL Packet Parser - 从 tcpdump 抓包文件中还原 MySQL 数据信息

功能特性:
    - 解析 pcap 文件中的 MySQL 协议数据包
    - 还原 SQL 语句 (包括预处理语句)
    - 解析服务器响应 (OK/Error/Result Set)
    - 支持中文等 UTF-8 字符
    - 纯字符串输出，无二进制信息

使用方法:
    python mysql_packet_parser.py <pcap_file> [-o output_file]

示例:
    python mysql_packet_parser.py mysql.pcap
    python mysql_packet_parser.py mysql.pcap -o report.txt

依赖:
    pip install scapy
"""

from __future__ import annotations

import argparse
import struct
import sys
from collections import defaultdict
from datetime import datetime
from typing import Any, Optional

try:
    from scapy.all import rdpcap, TCP
    from scapy.layers.inet import IP
except ImportError:
    print("错误: 请先安装 scapy 库")
    print("运行: pip install scapy")
    sys.exit(1)


# ============================================================================
# MySQL 协议常量
# ============================================================================

class MySQLCommand:
    """MySQL 命令类型常量"""
    COM_QUIT = 0x01
    COM_INIT_DB = 0x02
    COM_QUERY = 0x03
    COM_PING = 0x0E
    COM_STMT_PREPARE = 0x16
    COM_STMT_EXECUTE = 0x17
    COM_STMT_CLOSE = 0x19


class MySQLResponse:
    """MySQL 响应类型常量"""
    OK_PACKET = 0x00
    EOF_PACKET = 0xFE
    ERROR_PACKET = 0xFF
    NULL_VALUE = 0xFB


# ============================================================================
# MySQL 数据包解析器
# ============================================================================

class MySQLPacketParser:
    """
    MySQL 数据包解析器
    
    从 pcap 文件中提取并解析 MySQL 协议数据，
    还原 SQL 语句和服务器响应数据。
    
    Attributes:
        pcap_file: pcap 文件路径
        mysql_port: MySQL 服务端口 (默认 3306)
    """
    
    def __init__(self, pcap_file: str, mysql_port: int = 3306):
        self.pcap_file = pcap_file
        self.mysql_port = mysql_port
        self._packets: list = []
        self._connections: dict[str, list] = defaultdict(list)
    
    # ------------------------------------------------------------------------
    # 公共方法
    # ------------------------------------------------------------------------
    
    def parse(self) -> None:
        """解析 pcap 文件并提取 MySQL 数据"""
        self._load_pcap()
        self._extract_mysql_data()
    
    def generate_report(self) -> str:
        """
        生成分析报告
        
        Returns:
            格式化的分析报告文本
        """
        lines = []
        lines.append("=" * 80)
        lines.append("MySQL 数据包分析报告")
        lines.append("=" * 80)
        
        total_packets = sum(len(pkts) for pkts in self._connections.values())
        lines.append(f"\n总连接数: {len(self._connections)}")
        lines.append(f"总 MySQL 数据包数: {total_packets}")
        
        for conn_key, packets in self._connections.items():
            lines.append(f"\n{'=' * 80}")
            lines.append(f"连接: {conn_key}")
            lines.append(f"数据包数: {len(packets)}")
            lines.append("=" * 80)
            
            for pkt_info in packets:
                result = self._format_packet(pkt_info)
                if result:
                    lines.append(result)
        
        return "\n".join(lines)
    
    def save_report(self, output_file: str) -> None:
        """
        保存报告到文件
        
        Args:
            output_file: 输出文件路径
        """
        report = self.generate_report()
        with open(output_file, 'w', encoding='utf-8') as f:
            f.write(report)
        print(f"报告已保存到: {output_file}")
    
    def print_report(self) -> None:
        """打印报告到控制台"""
        print(self.generate_report())
    
    # ------------------------------------------------------------------------
    # 私有方法 - 数据加载
    # ------------------------------------------------------------------------
    
    def _load_pcap(self) -> None:
        """加载 pcap 文件"""
        print(f"正在读取: {self.pcap_file}")
        try:
            self._packets = rdpcap(self.pcap_file)
            print(f"成功读取 {len(self._packets)} 个数据包")
        except Exception as e:
            print(f"读取失败: {e}")
            sys.exit(1)
    
    def _extract_mysql_data(self) -> None:
        """从数据包中提取 MySQL 数据"""
        print("正在分析 MySQL 数据包...")
        
        for packet in self._packets:
            if not self._is_mysql_packet(packet):
                continue
            
            if IP not in packet or TCP not in packet:
                continue
            
            ip_layer = packet[IP]
            tcp_layer = packet[TCP]
            
            if not tcp_layer.payload:
                continue
            
            payload = bytes(tcp_layer.payload)
            if len(payload) == 0:
                continue
            
            # 确定数据方向
            is_client_to_server = tcp_layer.dport == self.mysql_port
            direction = "Client->Server" if is_client_to_server else "Server->Client"
            
            # 构建连接标识符
            if is_client_to_server:
                conn_key = f"{ip_layer.src}:{tcp_layer.sport} -> {ip_layer.dst}:{tcp_layer.dport}"
            else:
                conn_key = f"{ip_layer.dst}:{tcp_layer.dport} -> {ip_layer.src}:{tcp_layer.sport}"
            
            # 解析所有 MySQL 数据包
            timestamp = datetime.fromtimestamp(float(packet.time))
            for mysql_pkt in self._parse_mysql_packets(payload):
                self._connections[conn_key].append({
                    'timestamp': timestamp,
                    'direction': direction,
                    'packet': mysql_pkt,
                })
    
    def _is_mysql_packet(self, packet) -> bool:
        """判断是否为 MySQL 数据包"""
        if IP not in packet or TCP not in packet:
            return False
        tcp = packet[TCP]
        return tcp.sport == self.mysql_port or tcp.dport == self.mysql_port
    
    # ------------------------------------------------------------------------
    # 私有方法 - MySQL 协议解析
    # ------------------------------------------------------------------------
    
    def _parse_mysql_packets(self, data: bytes) -> list[dict]:
        """
        解析 TCP 负载中的所有 MySQL 数据包
        
        MySQL 数据包格式: [length:3][sequence:1][payload:length]
        """
        packets = []
        pos = 0
        
        while pos + 4 <= len(data):
            # 读取包长度 (3 字节小端序)
            length = struct.unpack('<I', data[pos:pos+3] + b'\x00')[0]
            sequence = data[pos + 3]
            
            if length == 0 or pos + 4 + length > len(data):
                break
            
            payload = data[pos + 4:pos + 4 + length]
            packets.append({
                'length': length,
                'sequence': sequence,
                'payload': payload,
            })
            pos += 4 + length
        
        return packets
    
    def _read_length_encoded_int(self, data: bytes, offset: int = 0) -> tuple[Optional[int], int]:
        """
        读取 MySQL 长度编码整数
        
        编码规则:
            < 0xFB: 1 字节
            0xFC: 后跟 2 字节
            0xFD: 后跟 3 字节
            0xFE: 后跟 8 字节
        """
        if offset >= len(data):
            return None, offset
        
        first = data[offset]
        
        if first < 0xFB:
            return first, offset + 1
        elif first == 0xFC and offset + 3 <= len(data):
            return struct.unpack('<H', data[offset+1:offset+3])[0], offset + 3
        elif first == 0xFD and offset + 4 <= len(data):
            return struct.unpack('<I', data[offset+1:offset+4] + b'\x00')[0], offset + 4
        elif first == 0xFE and offset + 9 <= len(data):
            return struct.unpack('<Q', data[offset+1:offset+9])[0], offset + 9
        
        return None, offset
    
    def _read_length_encoded_string(self, data: bytes, offset: int = 0) -> tuple[Optional[str], int]:
        """读取 MySQL 长度编码字符串"""
        if offset >= len(data):
            return None, offset
        
        # NULL 值
        if data[offset] == MySQLResponse.NULL_VALUE:
            return "NULL", offset + 1
        
        length, new_offset = self._read_length_encoded_int(data, offset)
        if length is None or new_offset + length > len(data):
            return None, offset
        
        try:
            value = data[new_offset:new_offset + length].decode('utf-8', errors='replace')
            return value, new_offset + length
        except Exception:
            return None, offset
    
    # ------------------------------------------------------------------------
    # 私有方法 - 命令解码
    # ------------------------------------------------------------------------
    
    def _decode_command(self, payload: bytes) -> Optional[str]:
        """解码客户端命令"""
        if len(payload) == 0:
            return None
        
        cmd = payload[0]
        
        if cmd == MySQLCommand.COM_QUERY:
            return self._decode_query(payload)
        elif cmd == MySQLCommand.COM_INIT_DB:
            return self._decode_init_db(payload)
        elif cmd == MySQLCommand.COM_STMT_PREPARE:
            return self._decode_stmt_prepare(payload)
        elif cmd == MySQLCommand.COM_STMT_EXECUTE:
            return self._decode_stmt_execute(payload)
        elif cmd == MySQLCommand.COM_STMT_CLOSE:
            return self._decode_stmt_close(payload)
        elif cmd == MySQLCommand.COM_QUIT:
            return "[QUIT]"
        elif cmd == MySQLCommand.COM_PING:
            return "[PING]"
        
        return None
    
    def _decode_query(self, payload: bytes) -> Optional[str]:
        """解码 COM_QUERY"""
        try:
            sql = payload[1:].decode('utf-8', errors='replace').strip()
            return sql if sql else None
        except Exception:
            return None
    
    def _decode_init_db(self, payload: bytes) -> Optional[str]:
        """解码 COM_INIT_DB"""
        try:
            db = payload[1:].decode('utf-8', errors='replace').strip()
            return f"USE {db}" if db else None
        except Exception:
            return None
    
    def _decode_stmt_prepare(self, payload: bytes) -> Optional[str]:
        """解码 COM_STMT_PREPARE"""
        try:
            sql = payload[1:].decode('utf-8', errors='replace').strip()
            return f"[PREPARE] {sql}" if sql else None
        except Exception:
            return None
    
    def _decode_stmt_execute(self, payload: bytes) -> str:
        """解码 COM_STMT_EXECUTE"""
        if len(payload) < 10:
            return "[EXECUTE] (incomplete)"
        
        stmt_id = struct.unpack('<I', payload[1:5])[0]
        result = f"[EXECUTE] stmt_id={stmt_id}"
        
        # 尝试提取参数
        if len(payload) > 10:
            params = self._extract_params(payload[10:])
            if params:
                result += f", params=[{', '.join(params)}]"
        
        return result
    
    def _decode_stmt_close(self, payload: bytes) -> Optional[str]:
        """解码 COM_STMT_CLOSE"""
        if len(payload) >= 5:
            stmt_id = struct.unpack('<I', payload[1:5])[0]
            return f"[STMT_CLOSE] stmt_id={stmt_id}"
        return None
    
    def _extract_params(self, data: bytes) -> Optional[list[str]]:
        """从 COM_STMT_EXECUTE 中提取参数"""
        params = []
        
        try:
            # 查找可读字符串起始位置
            text_start = 0
            for i, byte in enumerate(data):
                if 0x20 <= byte < 0x7F:
                    text_start = i
                    break
            
            # 提取所有可读字符串
            current = []
            for i in range(text_start, len(data)):
                byte = data[i]
                if 0x20 <= byte < 0x7F:
                    current.append(chr(byte))
                elif byte >= 0xC0:  # UTF-8 多字节
                    try:
                        if byte >= 0xE0 and i + 2 < len(data):
                            char = data[i:i+3].decode('utf-8', errors='ignore')
                            current.append(char)
                        elif i + 1 < len(data):
                            char = data[i:i+2].decode('utf-8', errors='ignore')
                            current.append(char)
                    except Exception:
                        pass
                else:
                    if len(current) >= 2:
                        params.append(''.join(current))
                    current = []
            
            if len(current) >= 2:
                params.append(''.join(current))
        except Exception:
            pass
        
        return params if params else None
    
    # ------------------------------------------------------------------------
    # 私有方法 - 响应解码
    # ------------------------------------------------------------------------
    
    def _decode_response(self, payload: bytes) -> Optional[str]:
        """解码服务器响应"""
        if len(payload) == 0:
            return None
        
        first = payload[0]
        
        # Error Packet
        if first == MySQLResponse.ERROR_PACKET:
            return self._decode_error(payload)
        
        # EOF Packet (忽略)
        if first == MySQLResponse.EOF_PACKET and len(payload) < 9:
            return None
        
        # 尝试解析为行数据
        row_data = self._try_parse_row(payload)
        if row_data:
            # 字段定义包 (以 "def" 开头)
            if row_data[0] == "def":
                if len(row_data) >= 5:
                    table = row_data[2] if len(row_data) > 2 else ""
                    column = row_data[4] if len(row_data) > 4 else ""
                    # 确保字段名是有效的标识符
                    if column and column.isprintable() and len(column) < 64 and not column.startswith("?"):
                        return f"FIELD: {table}.{column}" if table else f"FIELD: {column}"
                return None  # 忽略不完整或无效的字段定义
            
            # 普通行数据 - 过滤空数据和乱码
            def clean_value(v):
                """清理值中的乱码字符"""
                s = str(v).strip()
                # 移除 Unicode 替换字符
                s = s.replace('\ufffd', '').strip()
                return s
            
            non_empty = [clean_value(v) for v in row_data if v and clean_value(v) and len(clean_value(v)) > 1]
            # 检查是否有实际有意义的内容 (字母/数字/中文)
            meaningful = [v for v in non_empty if any(c.isalnum() or ord(c) > 127 for c in v)]
            if meaningful:
                return "ROW: " + " | ".join(non_empty)
            return None
        
        # OK Packet
        if first == MySQLResponse.OK_PACKET and len(payload) < 50:
            return self._decode_ok(payload)
        
        return None
    
    def _decode_error(self, payload: bytes) -> str:
        """解码 Error Packet"""
        if len(payload) < 9:
            return "ERROR"
        
        error_code = struct.unpack('<H', payload[1:3])[0]
        
        if payload[3:4] == b'#':
            sql_state = payload[4:9].decode('utf-8', errors='replace')
            message = payload[9:].decode('utf-8', errors='replace')
            return f"ERROR {error_code} ({sql_state}): {message}"
        else:
            message = payload[3:].decode('utf-8', errors='replace')
            return f"ERROR {error_code}: {message}"
    
    def _decode_ok(self, payload: bytes) -> Optional[str]:
        """解码 OK Packet"""
        if len(payload) < 7:
            return None
        
        affected_rows, offset = self._read_length_encoded_int(payload, 1)
        insert_id, _ = self._read_length_encoded_int(payload, offset)
        
        if affected_rows is not None and insert_id is not None:
            if affected_rows > 0 or insert_id > 0:
                return f"OK: affected_rows={affected_rows}, insert_id={insert_id}"
        
        return None
    
    def _try_parse_row(self, payload: bytes) -> Optional[list[str]]:
        """尝试解析为行数据"""
        try:
            row = []
            pos = 0
            parsed = 0
            
            while pos < len(payload):
                if payload[pos] == MySQLResponse.NULL_VALUE:
                    row.append("NULL")
                    pos += 1
                    parsed += 1
                    continue
                
                value, new_pos = self._read_length_encoded_string(payload, pos)
                if value is None or new_pos <= pos:
                    break
                
                if 0 < len(value) < 10000:
                    row.append(value)
                    parsed += 1
                pos = new_pos
            
            # 验证解析结果
            if parsed >= 1 and pos >= len(payload) * 0.8:
                return row
            
            return None
        except Exception:
            return None
    
    # ------------------------------------------------------------------------
    # 私有方法 - 格式化输出
    # ------------------------------------------------------------------------
    
    def _format_packet(self, pkt_info: dict) -> Optional[str]:
        """格式化单个数据包"""
        timestamp = pkt_info['timestamp'].strftime('%Y-%m-%d %H:%M:%S.%f')
        direction = pkt_info['direction']
        payload = pkt_info['packet']['payload']
        
        if direction == "Client->Server":
            decoded = self._decode_command(payload)
            if decoded:
                return f"\n[{timestamp}] {direction}\nSQL: {decoded}"
        else:
            decoded = self._decode_response(payload)
            if decoded:
                return f"\n[{timestamp}] {direction}\nResponse: {decoded}"
        
        return None


# ============================================================================
# 命令行入口
# ============================================================================

def main():
    """主函数"""
    parser = argparse.ArgumentParser(
        description='MySQL Packet Parser - 从 tcpdump 抓包中还原 MySQL 数据',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
示例:
  %(prog)s mysql.pcap              # 输出到控制台
  %(prog)s mysql.pcap -o report.txt  # 保存到文件
        """
    )
    parser.add_argument('pcap_file', help='pcap 文件路径')
    parser.add_argument('-o', '--output', help='输出文件路径')
    parser.add_argument('-p', '--port', type=int, default=3306, help='MySQL 端口 (默认: 3306)')
    
    args = parser.parse_args()
    
    # 解析并生成报告
    analyzer = MySQLPacketParser(args.pcap_file, args.port)
    analyzer.parse()
    
    if args.output:
        analyzer.save_report(args.output)
    else:
        analyzer.print_report()


if __name__ == '__main__':
    main()

