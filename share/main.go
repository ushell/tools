package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type FileMeta struct {
	ID           string    `json:"id"`
	OriginalName string    `json:"original_name"`
	StoredName   string    `json:"stored_name"`
	Size         int64     `json:"size"`
	PasswordHash string    `json:"password_hash,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type Store struct {
	mu        sync.RWMutex
	dataDir   string
	filesDir  string
	indexPath string
	Files     map[string]*FileMeta
}

func NewStore(dataDir string) (*Store, error) {
	filesDir := filepath.Join(dataDir, "files")
	if err := os.MkdirAll(filesDir, 0755); err != nil {
		return nil, err
	}
	s := &Store{
		dataDir:   dataDir,
		filesDir:  filesDir,
		indexPath: filepath.Join(dataDir, "index.json"),
		Files:     make(map[string]*FileMeta),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	data, err := ioutil.ReadFile(s.indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, &s.Files)
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.Files, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.indexPath + ".tmp"
	if err := ioutil.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.indexPath)
}

func (s *Store) Add(m *FileMeta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Files[m.ID] = m
	return s.saveLocked()
}

func (s *Store) Get(id string) (*FileMeta, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.Files[id]
	return m, ok
}

func (s *Store) Path(m *FileMeta) string {
	return filepath.Join(s.filesDir, m.StoredName)
}

func randomID(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func hashPassword(pw string) string {
	if pw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(pw))
	return hex.EncodeToString(sum[:])
}

func checkPassword(meta *FileMeta, pw string) bool {
	if meta.PasswordHash == "" {
		return true
	}
	return hashPassword(pw) == meta.PasswordHash
}

const maxUploadBytes = 1 << 30

type Server struct {
	store     *Store
	tpl       *template.Template
	publicURL string
}

func NewServer(store *Store, publicURL string) *Server {
	funcs := template.FuncMap{"humanSize": humanSize}
	tpl := template.Must(template.New("").Funcs(funcs).Parse(tplSource))
	return &Server{store: store, tpl: tpl, publicURL: publicURL}
}

func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/upload", s.handleUpload)
	mux.HandleFunc("/s/", s.handleSharePage)
	mux.HandleFunc("/d/", s.handleDownload)
	return mux
}

func (s *Server) render(w http.ResponseWriter, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("template error: %v", err)
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s.render(w, "index", nil)
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "上传失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "请选择文件", http.StatusBadRequest)
		return
	}
	defer file.Close()

	password := strings.TrimSpace(r.FormValue("password"))

	id, err := randomID(8)
	if err != nil {
		http.Error(w, "生成ID失败", http.StatusInternalServerError)
		return
	}
	stored := id + filepath.Ext(header.Filename)
	dstPath := filepath.Join(s.store.filesDir, stored)
	dst, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		http.Error(w, "保存失败", http.StatusInternalServerError)
		return
	}
	n, err := io.Copy(dst, file)
	cerr := dst.Close()
	if err != nil || cerr != nil {
		os.Remove(dstPath)
		http.Error(w, "写入失败", http.StatusInternalServerError)
		return
	}

	meta := &FileMeta{
		ID:           id,
		OriginalName: filepath.Base(header.Filename),
		StoredName:   stored,
		Size:         n,
		PasswordHash: hashPassword(password),
		CreatedAt:    time.Now(),
	}
	if err := s.store.Add(meta); err != nil {
		os.Remove(dstPath)
		http.Error(w, "保存元数据失败", http.StatusInternalServerError)
		return
	}

	shareURL := s.buildURL(r, "/s/"+id)
	s.render(w, "result", map[string]interface{}{
		"Meta":        meta,
		"ShareURL":    shareURL,
		"HasPassword": meta.PasswordHash != "",
	})
}

func (s *Server) handleSharePage(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/s/")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	meta, ok := s.store.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	data := map[string]interface{}{
		"Meta":    meta,
		"NeedPwd": meta.PasswordHash != "",
		"Error":   r.URL.Query().Get("err"),
	}
	s.render(w, "share", data)
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/d/")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	meta, ok := s.store.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	pw := r.FormValue("password")
	if !checkPassword(meta, pw) {
		http.Redirect(w, r, "/s/"+id+"?err="+url.QueryEscape("密码错误"), http.StatusSeeOther)
		return
	}
	path := s.store.Path(meta)
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "文件丢失", http.StatusNotFound)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Description", "File Transfer")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+escapeFilename(meta.OriginalName)+"\"")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", meta.Size))
	http.ServeContent(w, r, meta.OriginalName, meta.CreatedAt, f)
}

func escapeFilename(name string) string {
	r := strings.NewReplacer("\"", "", "\r", "", "\n", "")
	return r.Replace(name)
}

func (s *Server) buildURL(r *http.Request, path string) string {
	if s.publicURL != "" {
		return strings.TrimRight(s.publicURL, "/") + path
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host + path
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

const baseCSS = `body{font-family:-apple-system,Segoe UI,Helvetica,Arial,sans-serif;background:#f6f8fa;margin:0;padding:40px 16px;color:#24292f}
.card{max-width:520px;margin:0 auto;background:#fff;padding:28px 32px;border-radius:10px;box-shadow:0 1px 4px rgba(0,0,0,.08)}
h1{margin:0 0 18px;font-size:22px}
label{display:block;margin:12px 0 6px;font-size:14px;color:#57606a}
input[type=text],input[type=password],input[type=file]{width:100%;padding:8px 10px;border:1px solid #d0d7de;border-radius:6px;box-sizing:border-box;font-size:14px}
button{margin-top:18px;width:100%;padding:10px;background:#1f883d;color:#fff;border:none;border-radius:6px;font-size:15px;cursor:pointer}
button:hover{background:#1a7333}
a{color:#0969da;text-decoration:none}a:hover{text-decoration:underline}
.muted{color:#57606a;font-size:13px}
.err{background:#ffebe9;border:1px solid #ffcecb;padding:8px 10px;border-radius:6px;color:#82071e;font-size:14px;margin-bottom:12px}
.share-link{word-break:break-all;background:#f6f8fa;padding:10px;border-radius:6px;border:1px solid #d0d7de;font-family:Menlo,Consolas,monospace;font-size:13px}
.row{display:flex;justify-content:space-between;font-size:13px;color:#57606a;margin:6px 0}
.footer{text-align:center;margin-top:24px;color:#8b949e;font-size:12px}`

var tplSource = `
{{define "index"}}<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>上传文件</title><style>` + baseCSS + `</style></head><body><div class="card">
<h1>上传文件</h1>
<form action="/upload" method="post" enctype="multipart/form-data">
  <label>选择文件</label>
  <input type="file" name="file" required>
  <label>访问密码（可选）</label>
  <input type="password" name="password" placeholder="留空则任何人可下载">
  <button type="submit">上传</button>
</form>
</div><div class="footer">简易文件分享 · Go</div></body></html>{{end}}

{{define "result"}}<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>上传成功</title><style>` + baseCSS + `</style></head><body><div class="card">
<h1>上传成功</h1>
<p class="muted">文件名：{{.Meta.OriginalName}}（{{humanSize .Meta.Size}}）</p>
{{if .HasPassword}}<p class="muted">此分享受密码保护。</p>{{end}}
<label>分享链接</label>
<div class="share-link">{{.ShareURL}}</div>
<p style="margin-top:18px"><a href="/">继续上传</a></p>
</div><div class="footer">简易文件分享 · Go</div></body></html>{{end}}

{{define "share"}}<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>下载文件</title><style>` + baseCSS + `</style></head><body><div class="card">
<h1>{{.Meta.OriginalName}}</h1>
<div class="row"><span>大小</span><span>{{humanSize .Meta.Size}}</span></div>
<div class="row"><span>上传时间</span><span>{{.Meta.CreatedAt.Format "2006-01-02 15:04"}}</span></div>
{{if .Error}}<div class="err" style="margin-top:12px">{{.Error}}</div>{{end}}
<form action="/d/{{.Meta.ID}}" method="get" style="margin-top:8px">
  {{if .NeedPwd}}
  <label>访问密码</label>
  <input type="password" name="password" required autofocus>
  {{end}}
  <button type="submit">下载</button>
</form>
</div><div class="footer">简易文件分享 · Go</div></body></html>{{end}}
`

func main() {
	addr := flag.String("addr", ":8080", "监听地址")
	dataDir := flag.String("data", "./data", "数据存储目录")
	publicURL := flag.String("public-url", "", "对外可访问的基础URL（可选，如 https://share.example.com）")
	flag.Parse()

	store, err := NewStore(*dataDir)
	if err != nil {
		log.Fatalf("初始化存储失败: %v", err)
	}

	srv := NewServer(store, *publicURL)
	mux := srv.routes()

	log.Printf("share server listening on %s, data dir: %s", *addr, *dataDir)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}
