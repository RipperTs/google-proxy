package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"golang.org/x/net/proxy"
)

// 常量配置
const (
	// 目标站点（上游）
	target = "https://translate.google.com"
	// 环境变量名
	socks5EnvKey = "SOCKS5_URL"
	// 日志目录和前缀
	logDir          = "logs"
	accessLogPrefix = "access"
	errorLogPrefix  = "error"
)

// dailyFileWriter 按天切分日志文件的 Writer
type dailyFileWriter struct {
	mu          sync.Mutex
	dir         string
	prefix      string
	currentDate string
	file        *os.File
}

// 访问日志 logger（单独文件）
var accessLogger *log.Logger
var accessLogCh chan string

func newDailyFileWriter(dir, prefix string) *dailyFileWriter {
	return &dailyFileWriter{
		dir:    dir,
		prefix: prefix,
	}
}

func (w *dailyFileWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	today := time.Now().Format("2006-01-02")
	if w.file == nil || w.currentDate != today {
		if err := w.rotate(today); err != nil {
			// 如果写入日志文件失败，就退回标准输出，避免丢日志
			return os.Stdout.Write(p)
		}
	}

	return w.file.Write(p)
}

func (w *dailyFileWriter) rotate(date string) error {
	if err := os.MkdirAll(w.dir, 0755); err != nil {
		return err
	}

	filename := w.prefix + "-" + date + ".log"
	path := w.dir + string(os.PathSeparator) + filename

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	if w.file != nil {
		_ = w.file.Close()
	}

	w.file = f
	w.currentDate = date
	return nil
}

// 初始化日志输出到 logs 目录，按天写文件
func setupLogging() {
	// 错误/系统日志 -> error-YYYY-MM-DD.log
	errorWriter := newDailyFileWriter(logDir, errorLogPrefix)
	log.SetOutput(errorWriter)
	// 保留时间前缀，便于排查
	log.SetFlags(log.LstdFlags)

	// 访问日志 -> access-YYYY-MM-DD.log
	accessWriter := newDailyFileWriter(logDir, accessLogPrefix)
	accessLogger = log.New(accessWriter, "", log.LstdFlags)

	// 异步写访问日志，避免请求被磁盘 IO 阻塞
	accessLogCh = make(chan string, 10000)
	go func() {
		for msg := range accessLogCh {
			accessLogger.Println(msg)
		}
	}()
}

// 程序启动时自动加载 .env
func init() {
	// 先初始化日志到文件，再输出后续日志
	setupLogging()

	// 尝试加载当前目录下的 .env 文件，不存在也没关系
	if err := godotenv.Load(); err != nil {
		log.Printf("[INFO] .env file not found or failed to load: %v (this is ok if you set env elsewhere)", err)
	} else {
		log.Printf("[INFO] .env loaded")
	}
}

// 构造 http.Transport，视环境变量决定是否走 SOCKS5
func newTransportWithOptionalSOCKS5() *http.Transport {
	raw := strings.TrimSpace(os.Getenv(socks5EnvKey))
	if raw == "" {
		// 不配置 SOCKS5，就用系统默认（可读 HTTP_PROXY 等）
		log.Printf("[INFO] %s not set, using direct/HTTP proxy from env", socks5EnvKey)
		tr := &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSHandshakeTimeout:   10 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConns:          1024,
			MaxIdleConnsPerHost:   256,
			ExpectContinueTimeout: 1 * time.Second,
			ForceAttemptHTTP2:     true,
		}
		return tr
	}

	u, err := url.Parse(raw)
	if err != nil {
		log.Fatalf("[FATAL] invalid %s=%q: %v", socks5EnvKey, raw, err)
	}

	if u.Scheme != "socks5" {
		log.Fatalf("[FATAL] %s must start with socks5://, got: %q", socks5EnvKey, raw)
	}

	if u.Host == "" {
		log.Fatalf("[FATAL] %s missing host:port, got: %q", socks5EnvKey, raw)
	}

	host := u.Host // 例如 154.17.227.135:8899

	var user, pass string
	if u.User != nil {
		user = u.User.Username()
		pass, _ = u.User.Password()
	}

	log.Printf("[INFO] using SOCKS5 proxy %s (user=%q)", host, user)

	baseDialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	var auth *proxy.Auth
	if user != "" {
		auth = &proxy.Auth{
			User:     user,
			Password: pass,
		}
	}

	socksDialer, err := proxy.SOCKS5("tcp", host, auth, baseDialer)
	if err != nil {
		log.Fatalf("[FATAL] failed to create SOCKS5 dialer: %v", err)
	}

	dialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
		return socksDialer.Dial(network, addr)
	}

	tr := &http.Transport{
		Proxy:                 nil, // 使用 SOCKS5 时不再叠 HTTP 代理
		DialContext:           dialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          1024,
		MaxIdleConnsPerHost:   256,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	return tr
}

// 构造反向代理
func newReverseProxy(target string) (*httputil.ReverseProxy, error) {
	targetURL, err := url.Parse(target)
	if err != nil {
		return nil, err
	}

	proxyRP := httputil.NewSingleHostReverseProxy(targetURL)

	// 使用 buffer pool 减少内存分配，提高高并发下的吞吐
	proxyRP.BufferPool = &byteBufferPool{
		pool: sync.Pool{
			New: func() interface{} {
				// 32KB 是 go http 默认缓冲大小，足够大部分响应片段
				return make([]byte, 32*1024)
			},
		},
	}

	originalDirector := proxyRP.Director
	proxyRP.Director = func(req *http.Request) {
		originalDirector(req)

		// ==== 伪装成正常浏览器 + 去掉代理痕迹 ====

		// 确保目标地址
		req.URL.Scheme = targetURL.Scheme
		req.URL.Host = targetURL.Host
		req.Host = targetURL.Host

		// 去掉常见代理相关头
		for _, h := range []string{
			"X-Real-IP",
			"X-Forwarded-For",
			"X-Forwarded-Host",
			"X-Forwarded-Proto",
			"Forwarded",
			"Via",
			"Proxy-Connection",
		} {
			req.Header.Del(h)
		}

		// 伪装 User-Agent
		req.Header.Set("User-Agent",
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64) "+
				"AppleWebKit/537.36 (KHTML, like Gecko) "+
				"Chrome/124.0.0.0 Safari/537.36")

		// 如果客户端没带 Accept-Language，就给一个常见的
		if req.Header.Get("Accept-Language") == "" {
			req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
		}

		// gzip/br 正常压缩
		if req.Header.Get("Accept-Encoding") == "" {
			req.Header.Set("Accept-Encoding", "gzip, deflate, br")
		}
	}

	// 使用我们自定义的 Transport（支持可选 SOCKS5）
	proxyRP.Transport = newTransportWithOptionalSOCKS5()

	// 统一错误处理
	proxyRP.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
		log.Printf("[ERROR] proxy error: %v", err)
		http.Error(rw, "Upstream error", http.StatusBadGateway)
	}

	return proxyRP, nil
}

// byteBufferPool 为反向代理提供可复用的缓冲区，降低 GC 压力
type byteBufferPool struct {
	pool sync.Pool
}

func (p *byteBufferPool) Get() []byte {
	if v := p.pool.Get(); v != nil {
		return v.([]byte)
	}
	return make([]byte, 32*1024)
}

func (p *byteBufferPool) Put(b []byte) {
	if b != nil {
		p.pool.Put(b[:0])
	}
}

// clientIP 从请求中提取访问 IP（优先使用 X-Forwarded-For / X-Real-IP）
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xr := r.Header.Get("X-Real-IP"); xr != "" {
		return strings.TrimSpace(xr)
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// loggingMiddleware 统一请求访问日志（info 级别）仅记录 IP 和 UA
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)

		ip := clientIP(r)
		ua := r.Header.Get("User-Agent")

		// 访问日志单独写到 access-YYYY-MM-DD.log（异步，避免阻塞请求）
		if accessLogCh != nil {
			msg := fmt.Sprintf("[INFO] access: ip=%s ua=%q", ip, ua)
			select {
			case accessLogCh <- msg:
			default:
				// 队列满了就丢弃，保证代理转发不被日志拖慢
			}
		}
	})
}

// -------- IP 级限流（内存实现）---------

// ipRateLimiter 针对单个 IP 的令牌桶限流，适合高并发下的近似精确控制
type ipRateLimiter struct {
	mu      sync.Mutex
	limit   float64            // 桶容量（最大令牌数），即窗口内允许的最大请求数
	rate    float64            // 每秒填充令牌数
	buckets map[string]*bucket // 每个 IP 一个桶
}

type bucket struct {
	tokens float64   // 当前令牌数
	last   time.Time // 上一次刷新的时间
}

func newIPRateLimiter(maxReq int, window time.Duration) *ipRateLimiter {
	if window <= 0 {
		window = 10 * time.Second
	}
	limit := float64(maxReq)
	rate := limit / window.Seconds()
	return &ipRateLimiter{
		limit:   limit,
		rate:    rate,
		buckets: make(map[string]*bucket),
	}
}

// Allow 返回是否允许当前 IP 通过，超过限额返回 false
func (rl *ipRateLimiter) Allow(ip string) bool {
	now := time.Now()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[ip]
	if !ok {
		// 首次出现的 IP，给满桶，直接通过
		rl.buckets[ip] = &bucket{
			tokens: rl.limit - 1, // 预扣 1 个
			last:   now,
		}
		return true
	}

	// 根据时间间隔补充令牌
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * rl.rate
		if b.tokens > rl.limit {
			b.tokens = rl.limit
		}
		b.last = now
	}

	if b.tokens < 1 {
		// 没有足够令牌，拒绝请求
		return false
	}

	b.tokens--
	return true
}

// 全局默认：每 IP 每 10 秒最多 300 次（约 30 QPS）
var defaultIPLimiter = newIPRateLimiter(300, 10*time.Second)

// rateLimitMiddleware 在内存中对 IP 做限流
func rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !defaultIPLimiter.Allow(ip) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("Too Many Requests\n"))
			return
		}

		next.ServeHTTP(w, r)
	})
}

func main() {
	proxyRP, err := newReverseProxy(target)
	if err != nil {
		log.Fatalf("[FATAL] failed to create reverse proxy: %v", err)
	}

	mux := http.NewServeMux()

	// 健康检查
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok " + time.Now().Format(time.RFC3339)))
	})

	// 其余所有请求都转发到 translate.google.com
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		proxyRP.ServeHTTP(w, r)
	})

	addr := ":8080"
	log.Printf("[INFO] reverse proxy for %s listening on %s", target, addr)
	log.Printf("[INFO] %s from env: %q", socks5EnvKey, os.Getenv(socks5EnvKey))

	server := &http.Server{
		Addr: addr,
		// 先限流，再记录访问日志：这样被限流的请求也会打访问日志
		Handler:           loggingMiddleware(rateLimitMiddleware(mux)),
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB，足够大多数请求头，避免过大头部攻击
	}

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("[FATAL] server error: %v", err)
	}
}
