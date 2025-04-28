package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/satyrius/gonx"
)

var (
	logFormat = `$remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent "$http_referer" "$http_user_agent" "$http_x_forwarded_for"`
	urlFilter = []string{"js", "css", "img", "svg", "webp", "png"}
)

func parseLogLine(line string) (ip, url, userAgent, timestamp, status string) {
	logReader := strings.NewReader(line)

	parser := gonx.NewParser(logFormat)
	reader := gonx.NewParserReader(logReader, parser)

	for {
		entry, err := reader.Read()
		if err == io.EOF {
			break
		} else if err != nil {
			fmt.Println("解析错误:", err)
			continue
		}

		remoteAddr, _ := entry.Field("remote_addr")
		timeLocal, _ := entry.Field("time_local")
		request, _ := entry.Field("request")
		status, _ = entry.Field("status")
		userAgent, _ = entry.Field("http_user_agent")

		httpForwardedIps, _ := entry.Field("http_x_forwarded_for")
		proxyIps := strings.Split(httpForwardedIps, ",")

		ip = proxyIps[0]
		if ip == "-" {
			ip = remoteAddr
		}
		url = strings.Replace(request, " HTTP/1.1", "", 1)
		timestamp = timeLocal
	}
	return
}

// 统计访问 IP 最多前十
func topTenIPs(ipCounts map[string]int) []string {
	type pair struct {
		IP    string
		Count int
	}
	var pairs []pair
	for ip, count := range ipCounts {
		pairs = append(pairs, pair{ip, count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].Count > pairs[j].Count
	})
	var topTen []string
	for i := 0; i < 10 && i < len(pairs); i++ {
		topTen = append(topTen, pairs[i].IP)
	}
	return topTen
}

// 统计访问 URL 最多前十
func topTenURLs(urlCounts map[string]int) []string {
	type pair struct {
		URL   string
		Count int
	}
	var pairs []pair
	for url, count := range urlCounts {
		pairs = append(pairs, pair{url, count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].Count > pairs[j].Count
	})
	var topTen []string
	for i := 0; i < 10 && i < len(pairs); i++ {
		topTen = append(topTen, pairs[i].URL)
	}
	return topTen
}

func topTenUserAgent(userAgentCounts map[string]int) []string {
	type pair struct {
		UA    string
		Count int
	}
	var pairs []pair
	for ua, count := range userAgentCounts {
		pairs = append(pairs, pair{ua, count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].Count > pairs[j].Count
	})
	var topTen []string
	for i := 0; i < 10 && i < len(pairs); i++ {
		topTen = append(topTen, pairs[i].UA)
	}
	return topTen
}

// 统计访问量集中哪些时间
func popularTimes(timestampCounts map[string]int) []string {
	type pair struct {
		Time  string
		Count int
	}
	var pairs []pair
	for time, count := range timestampCounts {
		pairs = append(pairs, pair{time, count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].Count > pairs[j].Count
	})
	var popular []string
	for i := 0; i < 10 && i < len(pairs); i++ {
		popular = append(popular, pairs[i].Time)
	}
	return popular
}

func topTenHttpCode(httpCodeCounts map[string]int) []string {
	type pair struct {
		Code  string
		Count int
	}
	var pairs []pair
	for code, count := range httpCodeCounts {
		pairs = append(pairs, pair{code, count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].Count > pairs[j].Count
	})
	var popular []string
	for i := 0; i < 10 && i < len(pairs); i++ {
		popular = append(popular, pairs[i].Code)
	}
	return popular
}

func IsStrContain(str string, slice []string) bool {
	for _, v := range slice {
		if strings.Contains(str, v) {
			return true
		}
	}
	return false
}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("用法: ./nginx-log-analyse <nginx_log_file>")
		return
	}
	logFile := os.Args[1]
	file, err := os.Open(logFile)
	if err != nil {
		fmt.Printf("无法打开文件: %s, %v\n", logFile, err)
		return
	}
	defer file.Close()

	ipCounts := make(map[string]int)
	urlCounts := make(map[string]int)
	userAgentCounts := make(map[string]int)
	timestampCounts := make(map[string]int)
	statusCounts := make(map[string]int)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		ip, url, userAgent, timestamp, status := parseLogLine(line)
		// 过滤
		if IsStrContain(url, urlFilter) {
			continue
		}

		ipCounts[ip]++
		userAgentCounts[userAgent]++
		urlCounts[url]++
		statusCounts[status]++

		t, err := time.Parse("02/Jan/2006:15:04:05 -0700", timestamp)
		if err == nil {
			hour := t.Format("15:00")
			timestampCounts[hour]++
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("读取文件时出错: %v\n", err)
		return
	}

	topIPs := topTenIPs(ipCounts)
	topURLs := topTenURLs(urlCounts)
	topTenUA := topTenUserAgent(userAgentCounts)
	popularTimesList := popularTimes(timestampCounts)
	topCodeList := topTenHttpCode(statusCounts)

	fmt.Println("[🖥 IP排名]")
	for _, ip := range topIPs {
		fmt.Printf("%s: %d\n", ip, ipCounts[ip])
	}

	fmt.Println("\n[🛸 UA排名]")
	for _, ua := range topTenUA {
		fmt.Printf("%s: %d\n", ua, userAgentCounts[ua])
	}

	fmt.Println("\n[🌐 URL排名]")
	for _, url := range topURLs {
		fmt.Printf("%s: %d\n", url, urlCounts[url])
	}

	fmt.Println("\n[⏰ 访问时间]")
	for _, t := range popularTimesList {
		fmt.Printf("%s: %d\n", t, timestampCounts[t])
	}

	fmt.Println("\n[🚦 HTTP状态码]")
	for _, code := range topCodeList {
		fmt.Printf("%s: %d\n", code, statusCounts[code])
	}
}

