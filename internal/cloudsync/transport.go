package cloudsync

// Transport adapted from FreeType sync_store.go at 6cc7a6713c56e229b5bc0dbdb826282362d57195.
// It uses standard-library WebDAV and AWS SigV4 with TLS verification enabled.
import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type remoteEntry struct {
	ETag string
	Name string
	Dir  bool
	Size int64
}

type remoteStore interface {
	Put(key string, body io.ReadSeeker, size int64) error
	PutNew(key string, body io.ReadSeeker, size int64) error
	Get(key string, w io.Writer) error
	List(dir string) ([]remoteEntry, error)
	Delete(key string) error
	Probe() error
}

var errRemoteExists = errors.New("远端同名文件已存在")

const remoteHeaderTimeout = 30 * time.Second

func remoteClient(followRedirects bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = remoteHeaderTimeout
	client := &http.Client{Transport: transport}
	if !followRedirects {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	return client
}

func remoteError(action string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	detail := strings.TrimSpace(string(body))
	var s3 struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	if xml.Unmarshal(body, &s3) == nil && s3.Code != "" {
		detail = s3.Code + "：" + s3.Message
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%s失败：账号、密码或密钥错误，或没有权限；HTTP %d %s", action, resp.StatusCode, short(detail))
	case http.StatusInsufficientStorage:
		return fmt.Errorf("%s失败：远端空间不足；HTTP 507", action)
	}
	return fmt.Errorf("%s失败：HTTP %d %s", action, resp.StatusCode, short(detail))
}

func short(detail string) string {
	if detail == "" || strings.HasPrefix(detail, "<") {
		return ""
	}
	if r := []rune(detail); len(r) > 160 {
		detail = string(r[:160]) + "；后续内容已省略"
	}
	return "：" + detail
}

func cleanKey(key string) []string {
	var parts []string
	for _, part := range strings.Split(key, "/") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

type webdavStore struct {
	ctx      context.Context
	legacy   *webdavStore
	base     *url.URL
	user     string
	password string
	client   *http.Client
	made     map[string]bool
}

func (w *webdavStore) legacyStore() remoteStore {
	if w.legacy == nil {
		return nil
	}
	return w.legacy
}

func newWebDAVStore(address, user, password string) (*webdavStore, error) {
	base, err := url.Parse(strings.TrimSpace(address))
	if err != nil || base.Host == "" || (base.Scheme != "https" && base.Scheme != "http") {
		return nil, errors.New("WebDAV 地址要以 https:// 或 http:// 开头，例如 https://dav.jianguoyun.com/dav/FreeMD")
	}
	base.Path = strings.TrimRight(base.Path, "/")
	base.RawPath, base.RawQuery, base.Fragment = "", "", ""
	return &webdavStore{ctx: context.Background(), base: base, user: user, password: password, client: remoteClient(false), made: map[string]bool{}}, nil
}

func (w *webdavStore) address(parts []string, dir bool) string {
	u := *w.base
	u.Path = w.base.Path + "/" + strings.Join(parts, "/")
	if len(parts) == 0 {
		u.Path = w.base.Path
	}
	if dir {
		u.Path += "/"
	}
	return u.String()
}

func (w *webdavStore) do(method, address string, body io.Reader, size int64, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(w.ctx, method, address, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.ContentLength = size
	}
	if w.user != "" || w.password != "" {
		req.SetBasicAuth(w.user, w.password)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	return w.client.Do(req)
}

func (w *webdavStore) ensureDirs(parts []string) error {
	for i := 0; i <= len(parts); i++ {
		address := w.address(parts[:i], true)
		if w.made[address] {
			continue
		}
		resp, err := w.do("MKCOL", address, nil, 0, nil)
		if err != nil {
			return fmt.Errorf("创建远端目录失败：%w", err)
		}
		exists := resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusMethodNotAllowed ||
			resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent
		if !exists && (i > 0 || resp.StatusCode == http.StatusUnauthorized) {
			err = remoteError("创建远端目录", resp)
		}
		resp.Body.Close()
		if err != nil {
			return err
		}
		if exists {
			w.made[address] = true
		}
	}
	return nil
}

func (w *webdavStore) Put(key string, body io.ReadSeeker, size int64) error {
	return w.put(key, body, size, false)
}

func (w *webdavStore) PutNew(key string, body io.ReadSeeker, size int64) error {
	return w.put(key, body, size, true)
}

func (w *webdavStore) put(key string, body io.ReadSeeker, size int64, onlyNew bool) error {
	parts := cleanKey(key)
	if len(parts) == 0 {
		return errors.New("empty remote key")
	}
	if err := w.ensureDirs(parts[:len(parts)-1]); err != nil {
		return err
	}
	headers := map[string]string{"Content-Type": "application/octet-stream"}
	if onlyNew {
		headers["If-None-Match"] = "*"
	}
	resp, err := w.do(http.MethodPut, w.address(parts, false), body, size, headers)
	if err != nil {
		return fmt.Errorf("上传失败：%w", err)
	}
	defer resp.Body.Close()
	if onlyNew && resp.StatusCode == http.StatusPreconditionFailed {
		return errRemoteExists
	}
	if resp.StatusCode/100 != 2 {
		return remoteError("上传", resp)
	}
	return nil
}

func (w *webdavStore) Get(key string, out io.Writer) error {
	resp, err := w.do(http.MethodGet, w.address(cleanKey(key), false), nil, 0, nil)
	if err != nil {
		return fmt.Errorf("下载失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return remoteError("下载", resp)
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("下载中断：%w", err)
	}
	return nil
}

const propfindBody = `<?xml version="1.0" encoding="utf-8"?><D:propfind xmlns:D="DAV:"><D:prop><D:resourcetype/><D:getcontentlength/><D:getetag/></D:prop></D:propfind>`

type davMultistatus struct {
	Responses []struct {
		Href  string `xml:"href"`
		Props []struct {
			Collection *struct{} `xml:"prop>resourcetype>collection"`
			Length     string    `xml:"prop>getcontentlength"`
			ETag       string    `xml:"prop>getetag"`
		} `xml:"propstat"`
	} `xml:"response"`
}

func (w *webdavStore) propfind(address, depth string) (*http.Response, error) {
	return w.do("PROPFIND", address, strings.NewReader(propfindBody), int64(len(propfindBody)),
		map[string]string{"Depth": depth, "Content-Type": "application/xml; charset=utf-8"})
}

func hrefPath(href string) string {
	if u, err := url.Parse(strings.TrimSpace(href)); err == nil {
		return strings.TrimRight(u.Path, "/")
	}
	return strings.TrimRight(href, "/")
}

func (w *webdavStore) List(dir string) ([]remoteEntry, error) {
	parts := cleanKey(dir)
	address := w.address(parts, true)
	resp, err := w.propfind(address, "1")
	if err != nil {
		return nil, fmt.Errorf("读取远端目录失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusMultiStatus {
		return nil, remoteError("读取远端目录", resp)
	}
	var result davMultistatus
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("无法解析远端目录响应：%w", err)
	}
	self := hrefPath(address)
	var entries []remoteEntry
	for _, item := range result.Responses {
		path := hrefPath(item.Href)
		if path == self || !strings.HasPrefix(path, self+"/") {
			continue
		}
		name := path[len(self)+1:]
		if name == "" || strings.Contains(name, "/") {
			continue
		}
		entry := remoteEntry{Name: name}
		for _, prop := range item.Props {
			entry.Dir = entry.Dir || prop.Collection != nil
			entry.ETag = prop.ETag
			if prop.Length != "" {
				fmt.Sscan(strings.TrimSpace(prop.Length), &entry.Size)
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (w *webdavStore) Delete(key string) error {
	resp, err := w.do(http.MethodDelete, w.address(cleanKey(key), false), nil, 0, nil)
	if err != nil {
		return fmt.Errorf("删除失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 && resp.StatusCode != http.StatusNotFound {
		return remoteError("删除", resp)
	}
	return nil
}

func (w *webdavStore) Probe() error {
	if err := w.ensureDirs(nil); err != nil {
		return err
	}
	resp, err := w.propfind(w.address(nil, true), "0")
	if err != nil {
		return fmt.Errorf("连接失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMultiStatus {
		if resp.StatusCode == http.StatusNotFound {
			return errors.New("连接失败：远端目录不存在且无法创建；请先在网盘中创建该文件夹")
		}
		return remoteError("连接", resp)
	}
	return nil
}

type s3Store struct {
	ctx       context.Context
	legacy    *s3Store
	endpoint  *url.URL
	region    string
	bucket    string
	prefix    []string
	accessKey string
	secretKey string
	pathStyle bool
	client    *http.Client
	now       func() time.Time
}

func (s *s3Store) legacyStore() remoteStore {
	if s.legacy == nil {
		return nil
	}
	return s.legacy
}

func newS3Store(c Config) (*s3Store, error) {
	address := strings.TrimSpace(c.S3Endpoint)
	if address != "" && !strings.Contains(address, "://") {
		address = "https://" + address
	}
	endpoint, err := url.Parse(address)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "https" && endpoint.Scheme != "http") {
		return nil, errors.New("S3 服务地址不对，例如 https://s3.amazonaws.com 或 https://minio.example.com:9000")
	}
	bucket := strings.TrimSpace(c.S3Bucket)
	if bucket == "" || strings.ContainsAny(bucket, "/ ") {
		return nil, errors.New("请填写存储桶名称")
	}
	if strings.TrimSpace(c.S3AccessKey) == "" || c.S3SecretKey == "" {
		return nil, errors.New("请填写 Access Key 和 Secret Key")
	}
	region := strings.TrimSpace(c.S3Region)
	if region == "" {
		region = "us-east-1"
	}
	endpoint.Path, endpoint.RawPath, endpoint.RawQuery, endpoint.Fragment = "", "", "", ""
	return &s3Store{ctx: context.Background(),
		endpoint: endpoint, region: region, bucket: bucket, prefix: cleanKey(c.S3Prefix),
		accessKey: strings.TrimSpace(c.S3AccessKey), secretKey: c.S3SecretKey, pathStyle: c.S3PathStyle,
		client: remoteClient(false), now: time.Now,
	}, nil
}

func s3Escape(text string) string {
	var out strings.Builder
	for _, b := range []byte(text) {
		if b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '-' || b == '_' || b == '.' || b == '~' {
			out.WriteByte(b)
		} else {
			fmt.Fprintf(&out, "%%%02X", b)
		}
	}
	return out.String()
}

func s3Path(parts []string) string {
	escaped := make([]string, len(parts))
	for i, part := range parts {
		escaped[i] = s3Escape(part)
	}
	return "/" + strings.Join(escaped, "/")
}

func s3Query(query map[string]string) string {
	names := make([]string, 0, len(query))
	for name := range query {
		names = append(names, name)
	}
	sort.Strings(names)
	pairs := make([]string, len(names))
	for i, name := range names {
		pairs[i] = s3Escape(name) + "=" + s3Escape(query[name])
	}
	return strings.Join(pairs, "&")
}

func hmacSHA256(key []byte, text string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(text))
	return mac.Sum(nil)
}

func sigV4(method, path, query string, headers map[string]string, payloadHash, region, accessKey, secretKey string) string {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	var canonicalHeaders strings.Builder
	for _, name := range names {
		canonicalHeaders.WriteString(name + ":" + strings.TrimSpace(headers[name]) + "\n")
	}
	signed := strings.Join(names, ";")
	canonical := strings.Join([]string{method, path, query, canonicalHeaders.String(), signed, payloadHash}, "\n")
	stamp := headers["x-amz-date"]
	scope := stamp[:8] + "/" + region + "/s3/aws4_request"
	digest := sha256.Sum256([]byte(canonical))
	toSign := strings.Join([]string{"AWS4-HMAC-SHA256", stamp, scope, hex.EncodeToString(digest[:])}, "\n")
	key := hmacSHA256([]byte("AWS4"+secretKey), stamp[:8])
	key = hmacSHA256(key, region)
	key = hmacSHA256(key, "s3")
	key = hmacSHA256(key, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(key, toSign))
	return "AWS4-HMAC-SHA256 Credential=" + accessKey + "/" + scope + ", SignedHeaders=" + signed + ", Signature=" + signature
}

const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func (s *s3Store) do(method string, parts []string, query map[string]string, body io.ReadSeeker, size int64, conditions ...map[string]string) (*http.Response, error) {
	host := s.endpoint.Host
	path := s3Path(parts)
	if s.pathStyle {
		path = s3Path(append([]string{s.bucket}, parts...))
	} else {
		host = s.bucket + "." + host
	}
	payloadHash := emptySHA256
	if body != nil {
		hash := sha256.New()
		if _, err := io.Copy(hash, body); err != nil {
			return nil, err
		}
		if _, err := body.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		payloadHash = hex.EncodeToString(hash.Sum(nil))
	}
	rawQuery := s3Query(query)
	address := s.endpoint.Scheme + "://" + host + path
	if rawQuery != "" {
		address += "?" + rawQuery
	}
	var reader io.Reader
	if body != nil {
		reader = body
	}
	req, err := http.NewRequestWithContext(s.ctx, method, address, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.ContentLength = size
	}
	headers := map[string]string{
		"host":                 host,
		"x-amz-date":           s.now().UTC().Format("20060102T150405Z"),
		"x-amz-content-sha256": payloadHash,
	}
	for _, fields := range conditions {
		for name, value := range fields {
			headers[name] = value
			req.Header.Set(name, value)
		}
	}
	req.Header.Set("x-amz-date", headers["x-amz-date"])
	req.Header.Set("x-amz-content-sha256", payloadHash)
	req.Header.Set("Authorization", sigV4(method, path, rawQuery, headers, payloadHash, s.region, s.accessKey, s.secretKey))
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 == 3 {
		defer resp.Body.Close()
		hint := "区域或服务地址错误"
		if region := resp.Header.Get("x-amz-bucket-region"); region != "" {
			hint += "；存储桶区域为 " + region
		}
		return nil, fmt.Errorf("%s；HTTP %d", hint, resp.StatusCode)
	}
	return resp, nil
}

func (s *s3Store) object(key string) []string {
	return append(append([]string{}, s.prefix...), cleanKey(key)...)
}

func (s *s3Store) Put(key string, body io.ReadSeeker, size int64) error {
	return s.put(key, body, size, false)
}

func (s *s3Store) PutNew(key string, body io.ReadSeeker, size int64) error {
	return s.put(key, body, size, true)
}

func (s *s3Store) put(key string, body io.ReadSeeker, size int64, onlyNew bool) error {
	conditions := map[string]string{}
	if onlyNew {
		conditions["if-none-match"] = "*"
	}
	resp, err := s.do(http.MethodPut, s.object(key), nil, body, size, conditions)
	if err != nil {
		return fmt.Errorf("上传失败：%w", err)
	}
	defer resp.Body.Close()
	if onlyNew && resp.StatusCode == http.StatusPreconditionFailed {
		return errRemoteExists
	}
	if resp.StatusCode/100 != 2 {
		return remoteError("上传", resp)
	}
	return nil
}

func (s *s3Store) Get(key string, out io.Writer) error {
	resp, err := s.do(http.MethodGet, s.object(key), nil, nil, 0)
	if err != nil {
		return fmt.Errorf("下载失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return remoteError("下载", resp)
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("下载中断：%w", err)
	}
	return nil
}

type s3Listing struct {
	Contents []struct {
		Key  string `xml:"Key"`
		ETag string `xml:"ETag"`
		Size int64  `xml:"Size"`
	} `xml:"Contents"`
	CommonPrefixes []struct {
		Prefix string `xml:"Prefix"`
	} `xml:"CommonPrefixes"`
	IsTruncated bool   `xml:"IsTruncated"`
	Next        string `xml:"NextContinuationToken"`
}

func (s *s3Store) list(dir string, limit int) ([]remoteEntry, error) {
	prefix := strings.Join(s.object(dir), "/")
	if prefix != "" {
		prefix += "/"
	}
	var entries []remoteEntry
	token := ""
	for {
		query := map[string]string{"list-type": "2", "prefix": prefix, "delimiter": "/"}
		if limit > 0 {
			query["max-keys"] = fmt.Sprint(limit)
		}
		if token != "" {
			query["continuation-token"] = token
		}
		resp, err := s.do(http.MethodGet, nil, query, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("读取远端目录失败：%w", err)
		}
		if resp.StatusCode != http.StatusOK {
			err = remoteError("读取远端目录", resp)
			resp.Body.Close()
			return nil, err
		}
		var page s3Listing
		err = xml.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("无法解析远端目录响应：%w", err)
		}
		for _, item := range page.Contents {
			if name := strings.TrimPrefix(item.Key, prefix); name != "" && !strings.Contains(name, "/") {
				entries = append(entries, remoteEntry{Name: name, Size: item.Size, ETag: item.ETag})
			}
		}
		for _, item := range page.CommonPrefixes {
			if name := strings.Trim(strings.TrimPrefix(item.Prefix, prefix), "/"); name != "" {
				entries = append(entries, remoteEntry{Name: name, Dir: true})
			}
		}
		if !page.IsTruncated || page.Next == "" || limit > 0 {
			return entries, nil
		}
		token = page.Next
	}
}

func (s *s3Store) List(dir string) ([]remoteEntry, error) { return s.list(dir, 0) }

func (s *s3Store) Delete(key string) error {
	resp, err := s.do(http.MethodDelete, s.object(key), nil, nil, 0)
	if err != nil {
		return fmt.Errorf("删除失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 && resp.StatusCode != http.StatusNotFound {
		return remoteError("删除", resp)
	}
	return nil
}

func (s *s3Store) Probe() error {
	if _, err := s.list("", 1); err != nil {
		return fmt.Errorf("连接失败：%s", strings.TrimPrefix(err.Error(), "读取远端目录失败："))
	}
	probe := []byte("FreeMD")
	if err := s.Put(".freemd-probe", bytes.NewReader(probe), int64(len(probe))); err != nil {
		return fmt.Errorf("可读取存储桶，但无法写入：%w", err)
	}
	return s.Delete(".freemd-probe")
}
